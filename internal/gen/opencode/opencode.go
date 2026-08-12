package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vspcoderz/provider-hub/internal/keystore"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/sync"
)

// ConfigPath returns ~/.config/opencode/opencode.json.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

// ocProvider is the opencode provider format.
type ocProvider struct {
	NPM     string              `json:"npm"`
	Name    string              `json:"name"`
	Options ocOptions           `json:"options"`
	Models  map[string]ocModel  `json:"models,omitempty"`
}

type ocOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
}

type ocModel struct {
	Name  string       `json:"name,omitempty"`
	Limit *ocLimit     `json:"limit,omitempty"`
}

type ocLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// ocConfig is the top-level opencode.json structure.
type ocConfig struct {
	Schema   string                  `json:"$schema,omitempty"`
	Provider map[string]ocProvider   `json:"provider,omitempty"`
	Model    string                  `json:"model,omitempty"`
}

// npmPackageMap maps protocol to npm package.
var npmPackageMap = map[string]string{
	"anthropic": "@ai-sdk/anthropic",
	"openai":    "@ai-sdk/openai-compatible",
}

func npmPackage(p schema.Provider) string {
	if pkg, ok := p.Tools["opencode"]; ok && pkg.NPM != "" {
		return pkg.NPM
	}
	if hasProtocol(p, "anthropic") && !hasProtocol(p, "openai") {
		return "@ai-sdk/anthropic"
	}
	return "@ai-sdk/openai-compatible"
}

// Generate writes providers from cfg into opencode.json.
func Generate(cfg *schema.Config, dryRun bool) (string, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", "", err
	}

	// Load existing
	existing := ocConfig{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	if existing.Provider == nil {
		existing.Provider = map[string]ocProvider{}
	}
	existing.Schema = "https://opencode.ai/config.json"

	// Build providers
	for _, p := range cfg.Providers {
		if !isEnabled(p, "opencode") {
			continue
		}

		prov := ocProvider{
			NPM:  npmPackage(p),
			Name: p.Name,
			Options: ocOptions{
				BaseURL: p.BaseURL,
			},
		}

		if p.APIKeyEnv != "" {
			// Check keystore first, fall back to env var
			if storedKey, _ := keystore.Get(p.ID); storedKey != "" {
				prov.Options.APIKey = storedKey
			} else {
				prov.Options.APIKey = "{env:" + p.APIKeyEnv + "}"
			}
		}

		prov.Models = map[string]ocModel{}
		for _, m := range p.Models {
			om := ocModel{
				Name: m.Name,
			}
			if m.ContextWindow > 0 || m.MaxOutput > 0 {
				om.Limit = &ocLimit{
					Context: m.ContextWindow,
					Output:  m.MaxOutput,
				}
			}
			prov.Models[m.ID] = om
		}

		existing.Provider[p.ID] = prov
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return path, "", fmt.Errorf("marshal opencode config: %w", err)
	}

	if dryRun {
		return path, string(data), nil
	}

	if _, err := sync.Backup(path); err != nil {
		return path, "", err
	}
	if err := sync.EnsureDir(path); err != nil {
		return path, "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return path, "", fmt.Errorf("write opencode config: %w", err)
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
	return json.Unmarshal(data, &m)
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

// FindExistingProviders reads the live opencode.json and returns provider IDs.
func FindExistingProviders() []string {
	path, err := ConfigPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m ocConfig
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	var ids []string
	for k := range m.Provider {
		ids = append(ids, k)
	}
	return ids
}

// SetModel sets the active model in opencode.json (e.g. "provider/model").
func SetModel(providerID, modelID string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m ocConfig
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	m.Model = providerID + "/" + modelID
	data, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
