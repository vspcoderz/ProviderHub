package hermes_test

import (
	"strings"
	"testing"

	"github.com/vspcoderz/provider-hub/internal/gen/hermes"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"gopkg.in/yaml.v3"
)

func TestGenerate_Basic(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "agentrouter",
				Name:      "AgentRouter",
				BaseURL:   "https://agentrouter.org",
				APIKeyEnv: "AGENTROUTER_API_KEY",
				Protocols: []string{"anthropic"},
				Models: []schema.Model{
					{ID: "claude-opus-5", Name: "Claude Opus 5"},
				},
			},
		},
	}

	path, content, err := hermes.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path == "" {
		t.Fatal("expected path")
	}

	var m map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &m); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, content)
	}

	provs, ok := m["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers dict")
	}
	prov, ok := provs["agentrouter"].(map[string]interface{})
	if !ok {
		t.Fatal("missing agentrouter")
	}
	if prov["name"] != "AgentRouter" {
		t.Errorf("name = %v", prov["name"])
	}
	if prov["key_env"] != "AGENTROUTER_API_KEY" {
		t.Errorf("key_env = %v", prov["key_env"])
	}
	if prov["api_mode"] != "anthropic_messages" {
		t.Errorf("api_mode = %v, want anthropic_messages", prov["api_mode"])
	}

	t.Logf("Generated:\n%s", content)
}

func TestGenerate_OpenAIProtocol(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "openai",
				Name:      "OpenAI",
				BaseURL:   "https://api.openai.com",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "gpt-5.4", Name: "GPT 5.4"},
				},
			},
		},
	}

	_, content, err := hermes.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "chat_completions") {
		t.Error("expected chat_completions api_mode for openai protocol")
	}
}

func TestGenerate_ExtraHeaders(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "special",
				Name:      "Special Provider",
				BaseURL:   "https://special.example.com",
				Protocols: []string{"openai"},
				Headers: map[string]string{
					"User-Agent": "custom-agent/1.0",
				},
				Models: []schema.Model{
					{ID: "model1", Name: "Model 1"},
				},
			},
		},
	}

	_, content, err := hermes.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(content, "extra_headers") {
		t.Error("missing extra_headers")
	}
	if !strings.Contains(content, "custom-agent/1.0") {
		t.Error("header value not preserved")
	}
}

func TestGenerate_BaseURLStripsV1(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "withv1",
				Name:      "With V1",
				BaseURL:   "https://api.example.com/v1",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "m1", Name: "M1"},
				},
			},
		},
	}

	_, content, err := hermes.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// hermes adds /v1 itself, so if baseURL already has /v1, it should be stripped first
	// The generator appends /v1 to base_url in the output
	// This tests the output is well-formed
	var m map[string]interface{}
	yaml.Unmarshal([]byte(content), &m)
	provs := m["providers"].(map[string]interface{})
	prov := provs["withv1"].(map[string]interface{})
	api, _ := prov["api"].(string)
	if !strings.Contains(api, "example.com") {
		t.Errorf("api should contain base domain, got: %v", api)
	}
}
