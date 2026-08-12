package pi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/schema"
)

func TestGenerate_Basic(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "myproxy",
				Name:      "My Proxy",
				BaseURL:   "https://proxy.example.com",
				APIKeyEnv: "PROXY_KEY",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "gpt-5.4", Name: "GPT 5.4", ContextWindow: 128000, MaxOutput: 16384},
				},
			},
		},
	}

	path, content, err := pi.Generate(cfg, true)
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

	provs, ok := m["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers")
	}
	prov, ok := provs["myproxy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing myproxy")
	}
	if prov["baseUrl"] != "https://proxy.example.com" {
		t.Errorf("baseUrl = %v", prov["baseUrl"])
	}
	if prov["api"] != "openai-completions" {
		t.Errorf("api = %v, want openai-completions", prov["api"])
	}

	t.Logf("Generated:\n%s", content)
}

func TestGenerate_AnthropicProtocol(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "anthro",
				Name:      "Anthropic Proxy",
				BaseURL:   "https://anthro.example.com",
				Protocols: []string{"anthropic"},
				Models: []schema.Model{
					{ID: "claude-sonnet-4", Name: "Claude Sonnet 4"},
				},
			},
		},
	}

	_, content, err := pi.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "anthropic-messages") {
		t.Error("expected anthropic-messages api type")
	}
}

func TestGenerate_CostPreserved(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "costly",
				Name:      "Costly Provider",
				BaseURL:   "https://costly.example.com",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{
						ID:   "expensive-model",
						Name: "Expensive Model",
						Cost: &schema.ModelCost{Input: 15, Output: 75},
					},
				},
			},
		},
	}

	_, content, err := pi.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, `"input": 15`) {
		t.Error("cost input not preserved")
	}
	if !strings.Contains(content, `"output": 75`) {
		t.Error("cost output not preserved")
	}
}
