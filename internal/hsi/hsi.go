// Package hsi implements harness-specific integration (hsi): thin wrappers
// (ph-claude, ph-codex, ph-pi, ph-opencode) that inject the provider-hub
// routers into a real harness via isolated per-run configs + environment
// variables, leaving the harness's own config untouched.
package hsi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/vspcoderz/provider-hub/internal/config"
	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/keystore"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"gopkg.in/yaml.v3"
)

// Tool describes a supported harness and how it is driven.
type Tool struct {
	Name   string // canonical name (claude, codex, pi, opencode)
	Binary string // binary looked up on PATH
}

var Tools = []Tool{
	{Name: "claude", Binary: "claude"},
	{Name: "codex", Binary: "codex"},
	{Name: "pi", Binary: "pi"},
	{Name: "opencode", Binary: "opencode"},
}

func findTool(name string) (Tool, error) {
	for _, t := range Tools {
		if t.Name == name {
			return t, nil
		}
	}
	return Tool{}, fmt.Errorf("unknown harness %q (supported: claude, codex, pi, opencode)", name)
}

// ToolSel is the stored per-harness default provider/model.
type ToolSel struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model,omitempty"`
}

// Config is the hsi.yaml format.
type Config struct {
	Version int                `yaml:"version"`
	Tools   map[string]ToolSel `yaml:"tools"`
}

// ConfigPath returns ~/.config/provider-hub/hsi.yaml.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "provider-hub", "hsi.yaml"), nil
}

// Load reads the hsi config (returns defaults if absent).
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	c := &Config{Version: 1, Tools: map[string]ToolSel{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse hsi config: %w", err)
	}
	if c.Tools == nil {
		c.Tools = map[string]ToolSel{}
	}
	return c, nil
}

func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Set stores the default provider/model for a harness.
func Set(toolName, providerID, modelID string) error {
	if _, err := findTool(toolName); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if providerID != "" && config.FindProvider(cfg, providerID) == nil {
		return fmt.Errorf("provider %q not found in providers.yaml", providerID)
	}
	c, err := Load()
	if err != nil {
		return err
	}
	sel := c.Tools[toolName]
	if providerID != "" {
		sel.Provider = providerID
	}
	if modelID != "" {
		sel.Model = modelID
	} else {
		if p := config.FindProvider(cfg, sel.Provider); p != nil && len(p.Models) > 0 {
			sel.Model = p.Models[0].ID
		}
	}
	c.Tools[toolName] = sel
	return c.Save()
}

// selection resolves the effective provider/model for a harness:
// explicit flags > PH_PROVIDER/PH_MODEL env > stored hsi.yaml > first provider/model.
func selection(toolName, flagProvider, flagModel string) (string, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", "", err
	}
	c, err := Load()
	if err != nil {
		return "", "", err
	}

	providerID := c.Tools[toolName].Provider
	modelID := c.Tools[toolName].Model

	if v := os.Getenv("PH_PROVIDER"); v != "" {
		providerID = v
	}
	if v := os.Getenv("PH_MODEL"); v != "" {
		modelID = v
	}
	if flagProvider != "" {
		providerID = flagProvider
	}
	if flagModel != "" {
		modelID = flagModel
	}

	if providerID == "" {
		for _, p := range cfg.Providers {
			providerID = p.ID
			break
		}
	}
	if providerID == "" {
		return "", "", fmt.Errorf("no providers configured; run 'ph add' first")
	}
	if config.FindProvider(cfg, providerID) == nil {
		return "", "", fmt.Errorf("provider %q not found in providers.yaml", providerID)
	}
	if modelID == "" {
		if p := config.FindProvider(cfg, providerID); p != nil && len(p.Models) > 0 {
			modelID = p.Models[0].ID
		}
	}
	return providerID, modelID, nil
}

// resolveKey returns the API key from keystore, else the env var.
func resolveKey(p schema.Provider) string {
	if k, err := keystore.Get(p.ID); err == nil && k != "" {
		return k
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

// runDir returns the isolated per-run config dir for a harness and recreates it fresh.
func runDir(toolName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "provider-hub", "hsi", toolName)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// parseRunArgs splits leading --provider/--model flags from the tool args.
func parseRunArgs(args []string) (provider, model string, rest []string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--":
			return provider, model, args[i+1:]
		default:
			return provider, model, args[i:]
		}
	}
	return provider, model, nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func delEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// Run injects the provider into the harness and execs the real binary.
func Run(toolName string, args []string) error {
	tool, err := findTool(toolName)
	if err != nil {
		return err
	}

	flagProvider, flagModel, rest := parseRunArgs(args)
	providerID, modelID, err := selection(toolName, flagProvider, flagModel)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider := config.FindProvider(cfg, providerID)
	if provider == nil {
		return fmt.Errorf("provider %q not found", providerID)
	}

	bin, err := exec.LookPath(tool.Binary)
	if err != nil {
		return fmt.Errorf("%s not found on PATH: %w", tool.Binary, err)
	}

	env := os.Environ()

	switch toolName {
	case "claude":
		// claude code appends "/v1/messages" to ANTHROPIC_BASE_URL, so the base
		// must not already carry "/v1".
		env = setEnv(env, "ANTHROPIC_BASE_URL", schema.AnthropicBase(provider.BaseURL))
		if key := resolveKey(*provider); key != "" {
			env = setEnv(env, "ANTHROPIC_AUTH_TOKEN", key)
		}
		env = setEnv(env, "ANTHROPIC_MODEL", modelID)
		// Keep background/small-model calls on the router too.
		env = setEnv(env, "ANTHROPIC_SMALL_FAST_MODEL", modelID)
		// Opt in to gateway model discovery so the /model picker lists the
		// router's models from /v1/models (off by default since 2.1.129).
		env = setEnv(env, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY", "1")

	case "codex":
		// codex 0.122+ hard-requires the Responses API, which the routers do
		// not implement. Run a local Responses→ChatCompletions translation
		// proxy and point codex's provider block at it via -c overrides.
		// This keeps trust/permissions intact on the real ~/.codex config.
		env = delEnv(env, "CODEX_HOME")
		key := resolveKey(*provider)
		if key == "" {
			hint := fmt.Sprintf("ph key set %s", providerID)
			if provider.APIKeyEnv != "" {
				hint = fmt.Sprintf("set %s (or run ph key set %s)", provider.APIKeyEnv, providerID)
			}
			return fmt.Errorf("provider %q has no API key configured; %s", providerID, hint)
		}
		port, stop, err := startProxy(provider, key)
		if err != nil {
			return err
		}
		defer stop()
		if provider.APIKeyEnv != "" {
			env = setEnv(env, provider.APIKeyEnv, key)
		}
		catalogPath, err := writeModelCatalog(provider)
		if err != nil {
			return err
		}
		rest = append([]string{
			"-c", "model_providers." + providerID + ".base_url=http://127.0.0.1:" + fmt.Sprintf("%d", port) + "/v1",
			"-c", "model_providers." + providerID + ".wire_api=responses",
			"-c", "model_providers." + providerID + ".name=" + provider.Name,
			"-c", "model_provider=" + providerID,
			"-c", "model=" + modelID,
			"-c", "model_catalog_json=" + catalogPath,
		}, rest...)

		// Codex must run as a child so the proxy can be torn down afterwards.
		cmd := exec.Command(bin, append([]string{tool.Binary}, rest...)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return &ExitError{Code: ee.ExitCode()}
			}
			return err
		}
		return nil

	case "pi":
		dir, err := runDir(toolName)
		if err != nil {
			return err
		}
		content, err := pi.FreshConfig(cfg)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "models.json"), content, 0o600); err != nil {
			return err
		}
		env = setEnv(env, "PI_CODING_AGENT_DIR", dir)
		// pi picks its own provider/model from models.json, so forward the
		// resolved selection explicitly (ph-pi flags were already consumed).
		modelSel := modelID
		if !strings.Contains(modelID, "/") {
			modelSel = providerID + "/" + modelID
		}
		rest = append([]string{"--model", modelSel}, rest...)

	case "opencode":
		dir, err := runDir(toolName)
		if err != nil {
			return err
		}
		content, err := opencode.FreshConfig(cfg, providerID, modelID)
		if err != nil {
			return err
		}
		cfgPath := filepath.Join(dir, "opencode.json")
		if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
			return err
		}
		env = setEnv(env, "OPENCODE_CONFIG", cfgPath)
	}

	return syscall.Exec(bin, append([]string{tool.Binary}, rest...), env)
}

// setupScript is the generated wrapper body. %s is the harness name.
const setupScript = `#!/bin/sh
# Managed by provider-hub (ph hsi setup). Delete with: ph hsi rm %s
exec ph hsi run %s "$@"
`

// Setup writes ph-<tool> wrapper scripts into dir (default ~/.local/bin).
func Setup(dir string) error {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, t := range Tools {
		script := fmt.Sprintf(setupScript, t.Name, t.Name)
		path := filepath.Join(dir, "ph-"+t.Name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return err
		}
		fmt.Printf("  wrote %s\n", path)
	}
	return nil
}

// Remove deletes the wrapper script for a harness (or all if name empty).
func Remove(name string, dir string) error {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".local", "bin")
	}
	names := []string{name}
	if name == "" {
		names = nil
		for _, t := range Tools {
			names = append(names, t.Name)
		}
	}
	for _, n := range names {
		path := filepath.Join(dir, "ph-"+n)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("  removed %s\n", path)
	}
	return nil
}

// List prints the per-harness defaults and real binary locations.
func List() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c, err := Load()
	if err != nil {
		return err
	}

	for _, t := range Tools {
		sel := c.Tools[t.Name]
		display := sel.Provider
		if sel.Model != "" {
			display += "/" + sel.Model
		}
		if display == "" {
			display = "(unset - defaults to first provider)"
		}
		bin, _ := exec.LookPath(t.Binary)
		if bin == "" {
			bin = "(not found)"
		}
		fmt.Printf("  %-9s %-30s -> %s\n", t.Name, display, bin)
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("\nNo providers configured. Run 'ph add' first.")
		return nil
	}
	fmt.Println("\nProviders:")
	for _, p := range cfg.Providers {
		fmt.Printf("  - %s (%s)\n", p.ID, p.BaseURL)
	}
	return nil
}
