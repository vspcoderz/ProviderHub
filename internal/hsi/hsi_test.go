package hsi

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vspcoderz/provider-hub/internal/gen/codex"
	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/schema"
)

func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		in       []string
		prov, m  string
		rest     []string
	}{
		{[]string{"run", "hi"}, "", "", []string{"run", "hi"}},
		{[]string{"--provider", "agentrouter", "run", "hi"}, "agentrouter", "", []string{"run", "hi"}},
		{[]string{"--provider", "agentrouter", "--model", "claude-opus-5", "--", "run", "hi"}, "agentrouter", "claude-opus-5", []string{"run", "hi"}},
		{[]string{"--", "-p", "x"}, "", "", []string{"-p", "x"}},
	}
	for _, c := range cases {
		p, m, rest := parseRunArgs(c.in)
		if p != c.prov || m != c.m || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("parseRunArgs(%v) = (%q,%q,%v), want (%q,%q,%v)", c.in, p, m, rest, c.prov, c.m, c.rest)
		}
	}
}

func TestSetEnv(t *testing.T) {
	env := []string{"A=1", "B=2"}
	env = setEnv(env, "B", "3")
	env = setEnv(env, "C", "4")
	want := []string{"A=1", "B=3", "C=4"}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("setEnv = %v, want %v", env, want)
	}
}

func testCfg() *schema.Config {
	return &schema.Config{
		Version: 1,
		Providers: []schema.Provider{
			{
				ID:        "hsi-provider-a",
				Name:      "Provider A",
				BaseURL:   "https://a.example.com/v1",
				APIKeyEnv: "HISITEST_A_KEY",
				Protocols: []string{"openai"},
				Models: []schema.Model{
					{ID: "model-1", Name: "Model One"},
					{ID: "model-2", Name: "Model Two"},
				},
			},
			{
				ID:        "hsi-provider-b",
				Name:      "Provider B",
				BaseURL:   "https://b.example.com",
				APIKeyEnv: "HISITEST_B_KEY",
				Protocols: []string{"anthropic"},
				Models: []schema.Model{
					{ID: "claude-x", Name: "Claude X"},
				},
			},
		},
	}
}

func TestFreshConfigs(t *testing.T) {
	cfg := testCfg()

	oc, err := opencode.FreshConfig(cfg, "hsi-provider-a", "model-1")
	if err != nil {
		t.Fatalf("opencode FreshConfig: %v", err)
	}
	if !strings.Contains(string(oc), `"model": "hsi-provider-a/model-1"`) {
		t.Errorf("opencode missing default model: %s", oc)
	}
	if !strings.Contains(string(oc), "https://a.example.com/v1") {
		t.Errorf("opencode missing provider URL: %s", oc)
	}

	cx, err := codex.FreshConfig(cfg, "hsi-provider-a", "model-1")
	if err != nil {
		t.Fatalf("codex FreshConfig: %v", err)
	}
	if !strings.Contains(string(cx), `model_provider = "hsi-provider-a"`) {
		t.Errorf("codex missing model_provider: %s", cx)
	}

	pc, err := pi.FreshConfig(cfg)
	if err != nil {
		t.Fatalf("pi FreshConfig: %v", err)
	}
	if !strings.Contains(string(pc), "hsi-provider-b") {
		t.Errorf("pi missing provider b: %s", pc)
	}
}

func TestUpstreamBase(t *testing.T) {
	cases := map[string]string{
		"https://agentrouter.org":      "https://agentrouter.org/v1",
		"https://agentrouter.org/":     "https://agentrouter.org/v1",
		"https://gorouter.app/v1":      "https://gorouter.app/v1",
		"https://gorouter.app/v1/":     "https://gorouter.app/v1",
		"https://a.example.com":        "https://a.example.com/v1",
		"https://a.example.com/v1/api": "https://a.example.com/v1/api/v1",
	}
	for in, want := range cases {
		if got := upstreamBase(in); got != want {
			t.Errorf("upstreamBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelCatalog(t *testing.T) {
	p := &schema.Provider{
		ID:      "agentrouter",
		Name:    "AgentRouter",
		BaseURL: "https://agentrouter.org",
		Models: []schema.Model{
			{ID: "claude-opus-5", Name: "Opus 5", ContextWindow: 200000},
			{ID: "gpt-5.6-sol", Name: "GPT 5.6 SOL"},
		},
	}
	data, err := modelCatalog(p)
	if err != nil {
		t.Fatalf("modelCatalog: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"slug":"claude-opus-5"`, `"slug":"gpt-5.6-sol"`, `"base_instructions"`, `"context_window":200000`} {
		if !strings.Contains(s, want) {
			t.Errorf("modelCatalog missing %s: %s", want, s)
		}
	}
}
