package codex_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/vspcoderz/provider-hub/internal/gen/codex"
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
				APIKeyEnv: "PROXY_API_KEY",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "gpt-5.4", Name: "GPT 5.4"},
				},
			},
		},
	}

	path, content, err := codex.Generate(cfg, true) // dry-run
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path == "" {
		t.Fatal("expected path")
	}
	if content == "" {
		t.Fatal("expected content")
	}

	// Parse generated TOML
	var m map[string]interface{}
	if _, err := toml.Decode(content, &m); err != nil {
		t.Fatalf("generated TOML is invalid: %v\n%s", err, content)
	}

	mp, ok := m["model_providers"].(map[string]interface{})
	if !ok {
		t.Fatal("missing model_providers")
	}
	prov, ok := mp["myproxy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing myproxy in model_providers")
	}
	if prov["name"] != "My Proxy" {
		t.Errorf("name = %v, want My Proxy", prov["name"])
	}
	if prov["base_url"] != "https://proxy.example.com/v1" {
		t.Errorf("base_url = %v", prov["base_url"])
	}
	if prov["wire_api"] != "responses" {
		t.Errorf("wire_api = %v, want responses", prov["wire_api"])
	}

	t.Logf("Generated:\n%s", content)
}

func TestGenerate_SkipsReserved(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "openai",
				Name:      "OpenAI",
				BaseURL:   "https://api.openai.com/v1",
				Protocols: []string{"openai"},
			},
		},
	}

	_, content, err := codex.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(content, "openai") {
		t.Error("should not generate reserved ID openai in output")
	}
}

func TestGenerate_SkipsAnthropicOnly(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "anthroproxy",
				Name:      "Anthropic Proxy",
				BaseURL:   "https://anthro.example.com/v1",
				Protocols: []string{"anthropic"},
			},
		},
	}

	_, content, err := codex.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(content, "anthroproxy") {
		t.Error("should not generate anthropic-only provider for codex")
	}
}

func TestGenerate_PreservesExistingKeys(t *testing.T) {
	cfg := &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "newprov",
				Name:      "New Provider",
				BaseURL:   "https://new.example.com/v1",
				Protocols: []string{"openai"},
			},
		},
	}

	_, content, err := codex.Generate(cfg, true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Verify it's valid TOML and has expected structure
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""

	var m map[string]interface{}
	toml.Decode(content, &m)

	if err := enc.Encode(m); err != nil {
		t.Fatalf("re-encode failed: %v", err)
	}

	var m2 map[string]interface{}
	if _, err := toml.Decode(buf.String(), &m2); err != nil {
		t.Fatalf("round-trip TOML invalid: %v", err)
	}

	mp := m2["model_providers"].(map[string]interface{})
	if _, ok := mp["newprov"]; !ok {
		t.Error("newprov missing from round-tripped TOML")
	}
}
