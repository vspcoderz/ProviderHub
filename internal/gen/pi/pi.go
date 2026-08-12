package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/sync"
)

// ConfigPath returns ~/.pi/agent/models.json.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent", "models.json"), nil
}

// piProvider is the pi models.json provider format.
type piProvider struct {
	Name         string            `json:"name"`
	BaseURL      string            `json:"baseUrl"`
	API          string            `json:"api"`
	APIKey       string            `json:"apiKey,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Models       []piModel         `json:"models"`
}

// piModel is the pi models.json model format.
type piModel struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Reasoning     bool         `json:"reasoning"`
	Input         []string     `json:"input"`
	ContextWindow int          `json:"contextWindow"`
	MaxTokens     int          `json:"maxTokens"`
	Cost          piModelCost  `json:"cost"`
}

type piModelCost struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	CacheRead float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// piConfig is the top-level models.json structure.
type piConfig struct {
	Providers map[string]piProvider `json:"providers"`
}

// mapAPIMode maps protocol names to pi API types.
var apiModeMap = map[string]string{
	"anthropic": "anthropic-messages",
	"openai":    "openai-completions",
}

// Generate writes providers from cfg into pi models.json.
func Generate(cfg *schema.Config, dryRun bool) (string, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", "", err
	}

	// Load existing
	existing := piConfig{Providers: map[string]piProvider{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing) // ignore errors on corrupt files
	}

	// Build providers
	for _, p := range cfg.Providers {
		if !isEnabled(p, "pi") {
			continue
		}

		// Determine API type from protocols
		api := "openai-completions"
		if hasProtocol(p, "anthropic") && !hasProtocol(p, "openai") {
			api = "anthropic-messages"
		} else if hasProtocol(p, "openai") {
			// Check if responses API is available
			api = "openai-completions"
		}

		piProv := piProvider{
			Name:    p.Name,
			BaseURL: p.BaseURL,
			API:     api,
		}

		if p.APIKeyEnv != "" {
			piProv.APIKey = "$" + p.APIKeyEnv
		}
		if len(p.Headers) > 0 {
			piProv.Headers = p.Headers
		}

		for _, m := range p.Models {
			pm := piModel{
				ID:            m.ID,
				Name:          m.Name,
				Reasoning:     m.Reasoning,
				Input:         []string{"text"},
				ContextWindow: m.ContextWindow,
				MaxTokens:     m.MaxOutput,
			}
			if m.Cost != nil {
				pm.Cost = piModelCost{
					Input:      m.Cost.Input,
					Output:     m.Cost.Output,
					CacheRead:  m.Cost.CacheRead,
					CacheWrite: m.Cost.CacheWrite,
				}
			}
			// Add image input for openai models
			if hasProtocol(p, "openai") {
				pm.Input = append(pm.Input, "image")
			}
			piProv.Models = append(piProv.Models, pm)
		}

		existing.Providers[p.ID] = piProv
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return path, "", fmt.Errorf("marshal pi config: %w", err)
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
		return path, "", fmt.Errorf("write pi config: %w", err)
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

// FindExistingProviders reads the live models.json and returns provider IDs.
func FindExistingProviders() []string {
	path, err := ConfigPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m piConfig
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	var ids []string
	for k := range m.Providers {
		ids = append(ids, k)
	}
	return ids
}
