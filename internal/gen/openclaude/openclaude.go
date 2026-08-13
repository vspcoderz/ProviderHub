package openclaude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vspcoderz/provider-hub/internal/keystore"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/sync"
)

func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".openclaude.json"), nil
}

// ocProfile is an OpenClaude provider profile.
type ocProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
	ProviderProfiles []ocProfile `json:"providerProfiles,omitempty"`
}

// ocConfig is the top-level openclaude.json structure.
type ocConfig struct {
	ProviderProfiles []ocProfile `json:"providerProfiles,omitempty"`
	Model            string      `json:"model,omitempty"`
}

// mapProviderType maps protocol names to OpenClaude provider types.
func mapProviderType(p schema.Provider) string {
	if t, ok := p.Tools["openclaude"]; ok && t.NPM != "" {
		return t.NPM // reuse npm field for provider type
	}
	if hasProtocol(p, "anthropic") && !hasProtocol(p, "openai") {
		return "custom-anthropic"
	}
	return "custom-openai"
}

// Generate writes providers from cfg into openclaude.json.
func Generate(cfg *schema.Config, dryRun bool) (string, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", "", err
	}

	// Load existing config
	existing := ocConfig{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}

	// Build profiles from our config
	for _, p := range cfg.Providers {
		if !isEnabled(p, "openclaude") {
			continue
		}

		profileID := "ph-" + p.ID

		// Determine default model
		defaultModel := ""
		if len(p.Models) > 0 {
			defaultModel = p.Models[0].ID
		}

		// Get API key from keystore or env var
		apiKey := ""
		if storedKey, _ := keystore.Get(p.ID); storedKey != "" {
			apiKey = storedKey
		} else if p.APIKeyEnv != "" {
			apiKey = os.Getenv(p.APIKeyEnv)
		}

		profile := ocProfile{
			ID:       profileID,
			Name:     p.Name,
			Provider: mapProviderType(p),
			BaseURL:  p.BaseURL,
			Model:    defaultModel,
			APIKey:   apiKey,
		}

		// Update or append
		found := false
		for i, existingProfile := range existing.ProviderProfiles {
			if existingProfile.ID == profileID {
				existing.ProviderProfiles[i] = profile
				found = true
				break
			}
		}
		if !found {
			existing.ProviderProfiles = append(existing.ProviderProfiles, profile)
		}
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return path, "", fmt.Errorf("marshal openclaude config: %w", err)
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
		return path, "", fmt.Errorf("write openclaude config: %w", err)
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

// FindExistingProviders reads the live openclaude.json and returns provider names.
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
	var names []string
	for _, p := range m.ProviderProfiles {
		names = append(names, p.Name)
	}
	return names
}

// SetModel sets the active model in openclaude.json.
func SetModel(profileID, model string) error {
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
	m.Model = model
	data, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
