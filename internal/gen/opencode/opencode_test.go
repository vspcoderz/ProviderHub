package opencode_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/schema"
)

func TestGenerate_Basic(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "myproxy",
				Name:      "My Proxy",
				BaseURL:   "https://proxy.example.com/v1",
				APIKeyEnv: "PROXY_KEY",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "gpt-5.4", Name: "GPT 5.4", ContextWindow: 128000, MaxOutput: 16384},
				},
			},
		},
	}

	path, content, err := opencode.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path == "" {
		t.Fatal("expected path")
	}

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, content)
	}

	provs, ok := m["provider"].(map[string]interface{})
	if !ok {
		t.Fatal("missing provider")
	}
	prov, ok := provs["myproxy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing myproxy")
	}
	if prov["npm"] != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %v, want @ai-sdk/openai-compatible", prov["npm"])
	}
	if prov["name"] != "My Proxy" {
		t.Errorf("name = %v", prov["name"])
	}

	opts, ok := prov["options"].(map[string]interface{})
	if !ok {
		t.Fatal("missing options")
	}
	if opts["baseURL"] != "https://proxy.example.com/v1" {
		t.Errorf("baseURL = %v", opts["baseURL"])
	}

	t.Logf("Generated:\n%s", content)
}

func TestGenerate_AnthropicProvider(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "anthro",
				Name:      "Anthropic",
				BaseURL:   "https://api.anthropic.com/v1",
				APIKeyEnv: "ANTHROPIC_API_KEY",
				Protocols: []string{"anthropic"},
				Models: []schema.Model{
					{ID: "claude-sonnet-4", Name: "Claude Sonnet 4"},
				},
			},
		},
	}

	_, content, err := opencode.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "@ai-sdk/anthropic") {
		t.Error("expected @ai-sdk/anthropic npm package")
	}
	if !strings.Contains(content, "{env:ANTHROPIC_API_KEY}") {
		t.Error("expected env substitution syntax")
	}
}

func TestGenerate_ModelLimits(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "limited",
				Name:      "Limited Provider",
				BaseURL:   "https://limited.example.com/v1",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{
						ID:            "big-model",
						Name:          "Big Model",
						ContextWindow: 200000,
						MaxOutput:     64000,
					},
				},
			},
		},
	}

	_, content, err := opencode.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal([]byte(content), &m)

	provs := m["provider"].(map[string]interface{})
	prov := provs["limited"].(map[string]interface{})
	models := prov["models"].(map[string]interface{})
	big := models["big-model"].(map[string]interface{})
	limit := big["limit"].(map[string]interface{})

	if limit["context"] != float64(200000) {
		t.Errorf("context = %v, want 200000", limit["context"])
	}
	if limit["output"] != float64(64000) {
		t.Errorf("output = %v, want 64000", limit["output"])
	}
}
