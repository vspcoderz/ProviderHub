package schema

import (
	"strings"
	"time"
)

// AnthropicBase strips a trailing "/v1" from a base URL. Anthropic-protocol
// tools (claude code, pi anthropic-messages) append their own "/v1/messages",
// so a base that already ends in "/v1" would produce "/v1/v1/messages".
func AnthropicBase(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return strings.TrimSuffix(trimmed, "/v1")
	}
	return trimmed
}

// Config is the top-level canonical config file.
type Config struct {
	Version   int        `yaml:"version"`
	Providers []Provider `yaml:"providers"`
}

// Provider describes one custom provider endpoint.
type Provider struct {
	ID                string            `yaml:"id"`
	Name              string            `yaml:"name"`
	BaseURL           string            `yaml:"baseUrl"`
	APIKeyEnv         string            `yaml:"apiKeyEnv,omitempty"`
	Protocols         []string          `yaml:"protocols,omitempty"`  // "anthropic", "openai"
	Headers           map[string]string `yaml:"headers,omitempty"`
	Tools             map[string]Tool   `yaml:"tools,omitempty"`
	Models            []Model           `yaml:"models"`
	SystemPrompt      string            `yaml:"systemPrompt,omitempty"` // system prompt / agent memory
}

// Tool configures how a provider is deployed to a specific tool.
type Tool struct {
	Enabled bool   `yaml:"enabled"`
	APIMode string `yaml:"apiMode,omitempty"`   // hermes: anthropic_messages, chat_completions, codex_responses
	NPM     string `yaml:"npm,omitempty"`       // opencode: SDK package
}

// Model describes a single model served by a provider.
type Model struct {
	ID            string     `yaml:"id"`
	Name          string     `yaml:"name,omitempty"`
	ContextWindow int        `yaml:"contextWindow,omitempty"`
	MaxOutput     int        `yaml:"maxOutput,omitempty"`
	Reasoning     bool       `yaml:"reasoning,omitempty"`
	Cost          *ModelCost `yaml:"cost,omitempty"`
}

// ModelCost stores per-million-token pricing.
type ModelCost struct {
	Input     float64 `yaml:"input"`
	Output    float64 `yaml:"output"`
	CacheRead float64 `yaml:"cacheRead,omitempty"`
	CacheWrite float64 `yaml:"cacheWrite,omitempty"`
}

// SyncState records when each tool was last synced.
type SyncState struct {
	LastSync map[string]time.Time `yaml:"lastSync"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Version:   1,
		Providers: []Provider{},
	}
}
