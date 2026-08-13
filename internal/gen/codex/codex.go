package codex

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/sync"
)

var reservedIDs = map[string]bool{
	"openai": true, "ollama": true, "lmstudio": true,
}

// ConfigPath returns ~/.codex/config.toml.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

// Generate writes providers from cfg into codex config.toml, non-destructively.
// It preserves all existing keys not managed by provider-hub.
// Returns the path written and any diff info.
func Generate(cfg *schema.Config, dryRun bool) (string, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", "", err
	}

	// Load existing config as generic map
	existing := map[string]interface{}{}
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &existing); err != nil {
			return path, "", fmt.Errorf("decode existing codex config: %w", err)
		}
	}

	// Build model_providers table
	providers := map[string]interface{}{}
	providersOrder := []string{} // track order

	for _, p := range cfg.Providers {
		if reservedIDs[p.ID] {
			continue
		}
		if !isEnabled(p, "codex") {
			continue
		}

		// Check protocol compatibility
		if !hasProtocol(p, "openai") {
			continue // codex only supports openai responses api
		}

		providers[p.ID] = BuildProviderEntry(p)
		providersOrder = append(providersOrder, p.ID)
	}

	// Set model_providers
	existing["model_providers"] = providers

	// Set defaults if only one provider
	if len(providersOrder) == 1 {
		existing["model_provider"] = providersOrder[0]
	}

	if dryRun {
		var buf bytes.Buffer
		enc := toml.NewEncoder(&buf)
		enc.Indent = ""
		enc.Encode(existing)
		return path, buf.String(), nil
	}

	// Backup
	if _, err := sync.Backup(path); err != nil {
		return path, "", err
	}

	// Write
	if err := sync.EnsureDir(path); err != nil {
		return path, "", err
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(existing); err != nil {
		return path, "", fmt.Errorf("encode codex config: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return path, "", fmt.Errorf("write codex config: %w", err)
	}

	return path, buf.String(), nil
}

// BuildProviderEntry returns the codex model_providers entry for a provider.
func BuildProviderEntry(p schema.Provider) map[string]interface{} {
	entry := map[string]interface{}{
		"name":     p.Name,
		"base_url": p.BaseURL,
	}
	if p.APIKeyEnv != "" {
		entry["env_key"] = p.APIKeyEnv
	}
	entry["wire_api"] = "responses"

	if len(p.Headers) > 0 {
		entry["http_headers"] = p.Headers
	}
	return entry
}

// FreshConfig returns a standalone config.toml containing every codex-compatible
// provider, with the given provider/model selected as default. Used by the hsi
// harness wrappers so the real ~/.codex config stays untouched.
func FreshConfig(cfg *schema.Config, defaultProviderID, defaultModelID string) ([]byte, error) {
	providers := map[string]interface{}{}
	for _, p := range cfg.Providers {
		if reservedIDs[p.ID] {
			continue
		}
		if !isEnabled(p, "codex") || !hasProtocol(p, "openai") {
			continue
		}
		providers[p.ID] = BuildProviderEntry(p)
	}

	doc := map[string]interface{}{
		"model":           defaultModelID,
		"model_provider":  defaultProviderID,
		"model_providers": providers,
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode codex hsi config: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks the generated config by running `codex --help` (just parsing check).
func Validate() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // no config is fine
	}
	// Try to decode
	var m map[string]interface{}
	_, err = toml.DecodeFile(path, &m)
	return err
}

func isEnabled(p schema.Provider, tool string) bool {
	t, ok := p.Tools[tool]
	if !ok {
		return true // default: enabled
	}
	return t.Enabled
}

func hasProtocol(p schema.Provider, proto string) bool {
	for _, pp := range p.Protocols {
		if pp == proto {
			return true
		}
	}
	return len(p.Protocols) == 0 // no protocols listed = assume compatible
}

// CodexHome returns the codex home dir, or ~/.codex.
func CodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// FindExistingProviders reads the live config.toml and returns provider IDs found.
func FindExistingProviders() []string {
	path, err := ConfigPath()
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil
	}
	mp, ok := m["model_providers"].(map[string]interface{})
	if !ok {
		return nil
	}
	var ids []string
	for k := range mp {
		ids = append(ids, k)
	}
	return ids
}

// Diff returns a human-readable diff between existing and proposed.
func Diff(existing, proposed string) string {
	if existing == proposed {
		return "(no changes)"
	}
	eLines := strings.Split(existing, "\n")
	pLines := strings.Split(proposed, "\n")

	var out []string
	max := len(eLines)
	if len(pLines) > max {
		max = len(pLines)
	}

	for i := 0; i < max; i++ {
		var e, p string
		if i < len(eLines) {
			e = eLines[i]
		}
		if i < len(pLines) {
			p = pLines[i]
		}
		if e != p {
			if e != "" {
				out = append(out, "- "+e)
			}
			if p != "" {
				out = append(out, "+ "+p)
			}
		}
	}
	return strings.Join(out, "\n")
}

// IsCompatible checks if a provider can work with codex.
func IsCompatible(p schema.Provider) (bool, string) {
	if reservedIDs[p.ID] {
		return false, fmt.Sprintf("ID %q is reserved by codex", p.ID)
	}
	if !hasProtocol(p, "openai") {
		return false, "codex only supports openai responses wire api"
	}
	return true, ""
}

// Check runs codex briefly to verify config parses.
func Check() error {
	home := CodexHome()
	cmd := exec.Command("codex", "--help")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codex check failed: %s\n%s", err, out)
	}
	return nil
}
