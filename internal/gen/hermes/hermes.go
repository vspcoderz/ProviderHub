package hermes

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vspcoderz/provider-hub/internal/keystore"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/sync"
	"gopkg.in/yaml.v3"
)

// ConfigPath returns ~/.hermes/config.yaml.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hermes", "config.yaml"), nil
}

// hermesProvider is the hermes providers dict entry.
type hermesProvider struct {
	Name         string            `yaml:"name"`
	API          string            `yaml:"api"`
	KeyEnv       string            `yaml:"key_env,omitempty"`
	DefaultModel string            `yaml:"default_model,omitempty"`
	APIMode      string            `yaml:"api_mode,omitempty"`
	Models       []string          `yaml:"models,omitempty"`
	ExtraHeaders map[string]string `yaml:"extra_headers,omitempty"`
	Discover     bool              `yaml:"discover_models,omitempty"`
}

// apiModeMap maps protocol names to hermes api_mode values.
var apiModeMap = map[string]string{
	"anthropic": "anthropic_messages",
	"openai":    "chat_completions",
}

func resolveAPIMode(p schema.Provider) string {
	// Check explicit tool config
	if t, ok := p.Tools["hermes"]; ok && t.APIMode != "" {
		return t.APIMode
	}
	// Infer from protocols
	if hasProtocol(p, "anthropic") && !hasProtocol(p, "openai") {
		return "anthropic_messages"
	}
	return "chat_completions"
}

// Generate writes providers from cfg into hermes config.yaml.
// Non-destructive: only updates providers/model sections, preserving everything else.
func Generate(cfg *schema.Config, dryRun bool) (string, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", "", err
	}

	// Read existing config as raw YAML tree to preserve structure
	existingData, err := os.ReadFile(path)
	if err != nil {
		return path, "", fmt.Errorf("read hermes config: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(existingData, &raw); err != nil {
		return path, "", fmt.Errorf("parse hermes config: %w", err)
	}

	// Build providers dict (modern format)
	providers := map[string]interface{}{}

	for _, p := range cfg.Providers {
		if !isEnabled(p, "hermes") {
			continue
		}

		prov := hermesProvider{
			Name:    p.Name,
			API:     p.BaseURL,
			KeyEnv:  p.APIKeyEnv,
			APIMode: resolveAPIMode(p),
		}

		// Check keystore for stored key
		if storedKey, _ := keystore.Get(p.ID); storedKey != "" {
			prov.KeyEnv = "" // don't reference env var if we have the key
			// Write key to hermes .env file
			writeHermesEnv(p.ID, storedKey)
		}

		if len(p.Models) > 0 {
			// Set default model to first model
			prov.DefaultModel = p.Models[0].ID
			for _, m := range p.Models {
				prov.Models = append(prov.Models, m.ID)
			}
		}

		if len(p.Headers) > 0 {
			prov.ExtraHeaders = p.Headers
		}

		providers[p.ID] = prov
	}

	raw["providers"] = providers

	// Update model.provider to first enabled provider if only one and a model exists
	if len(cfg.Providers) == 1 {
		p := cfg.Providers[0]
		if isEnabled(p, "hermes") && len(p.Models) > 0 {
			raw["model"] = map[string]interface{}{
				"default":  p.Models[0].ID,
				"provider": "custom:" + p.ID,
			}
		}
	}

	// Marshal
	data, err := yaml.Marshal(raw)
	if err != nil {
		return path, "", fmt.Errorf("marshal hermes config: %w", err)
	}

	if dryRun {
		return path, string(data), nil
	}

	// Backup
	if _, err := sync.Backup(path); err != nil {
		return path, "", err
	}

	// Ensure dir
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return path, "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return path, "", fmt.Errorf("write hermes config: %w", err)
	}

	return path, string(data), nil
}

// Validate checks the generated config.
func Validate() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]interface{}
	return yaml.Unmarshal(data, &m)
}

func isEnabled(p schema.Provider, tool string) bool {
	t, ok := p.Tools[tool]
	if !ok {
		return true
	}
	return t.Enabled
}

func hasProtocol(p schema.Provider, proto string) bool {
	for _, pp := range p.Protocols {
		if pp == proto {
			return true
		}
	}
	return len(p.Protocols) == 0
}

// FindExistingProviders reads the live config.yaml and returns provider IDs.
func FindExistingProviders() []string {
	path, err := ConfigPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil
	}

	var ids []string

	// Modern format: providers dict
	if provs, ok := m["providers"].(map[string]interface{}); ok {
		for k := range provs {
			ids = append(ids, k)
		}
	}

	// Legacy format: custom_providers list
	if cps, ok := m["custom_providers"].([]interface{}); ok {
		for _, cp := range cps {
			if entry, ok := cp.(map[string]interface{}); ok {
				if name, ok := entry["name"].(string); ok {
					ids = append(ids, name)
				}
			}
		}
	}

	return ids
}

// CurrentModel returns the current model.provider value.
func CurrentModel() (provider, model string) {
	path, err := ConfigPath()
	if err != nil {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return "", ""
	}
	mm, ok := m["model"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	prov, _ := mm["provider"].(string)
	def, _ := mm["default"].(string)

	// Strip custom: prefix
	prov = strings.TrimPrefix(prov, "custom:")

	return prov, def
}

// SetModel sets the active model in hermes config.yaml.
func SetModel(providerID, modelID string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}

	raw["model"] = map[string]interface{}{
		"default":  modelID,
		"provider": "custom:" + providerID,
	}

	data, err = yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// IsCustomProvider checks if a provider is in custom_providers format.
func IsCustomProvider(providerID string) bool {
	path, err := ConfigPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return false
	}

	// Check legacy format
	if cps, ok := m["custom_providers"].([]interface{}); ok {
		for _, cp := range cps {
			if entry, ok := cp.(map[string]interface{}); ok {
				if name, ok := entry["name"].(string); ok && name == providerID {
					return true
				}
			}
		}
	}

	return false
}

// EnsureCustomProvider converts a provider from custom_providers list to providers dict format.
// This is a one-time migration for legacy configs.
func EnsureCustomProvider(providerID string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Simple string replacement for the legacy entry
	re := regexp.MustCompile(`(?m)^  - name: ` + regexp.QuoteMeta(providerID) + `\n(?:    .*\n)*`)
	replaced := re.ReplaceAll(data, nil)

	return os.WriteFile(path, replaced, 0o644)
}

// writeHermesEnv writes or updates an API key in ~/.hermes/.env
func writeHermesEnv(providerID, key string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	envPath := filepath.Join(home, ".hermes", ".env")

	// Read existing
	data, _ := os.ReadFile(envPath)
	content := string(data)

	// Build env var name (uppercase, underscore-separated)
	envVar := strings.ToUpper(providerID) + "_API_KEY"
	line := envVar + "=" + key

	// Check if already exists
	lines := strings.Split(content, "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, envVar+"=") {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, line)
	}

	os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0o600)
}
