package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/vspcoderz/provider-hub/internal/config"
	"github.com/vspcoderz/provider-hub/internal/gen/codex"
	"github.com/vspcoderz/provider-hub/internal/gen/hermes"
	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/gui"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "list", "ls":
		cmdList()
	case "add":
		cmdAdd(args)
	case "sync":
		cmdSync(args)
	case "doctor":
		cmdDoctor()
	case "check":
		cmdCheck(args)
	case "import":
		cmdImport()
	case "serve":
		cmdServe()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`provider-hub (ph) - manage LLM providers across codex, pi, opencode, hermes

Usage:
  ph list              List all providers in canonical config
  ph add               Add a provider interactively (stdin)
  ph sync [--tool T]   Sync to tool configs (default: all)
  ph doctor            Validate all tool configs
  ph check <id>        Check provider health via /models
  ph import            Import from existing tool configs
  ph serve             Launch web GUI at http://localhost:7357

Flags:
  --dry-run            Show what would be written without writing
  --tool codex|pi|opencode|hermes   Target specific tool

Examples:
  ph list
  ph sync --tool opencode
  ph sync --dry-run
  ph doctor
  ph serve
`)
}

func cmdList() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured. Run 'ph add' to add one.")
		return
	}

	for _, p := range cfg.Providers {
		tools := []string{}
		for name, t := range p.Tools {
			if t.Enabled {
				tools = append(tools, name)
			} else {
				tools = append(tools, name+"(off)")
			}
		}
		protocols := strings.Join(p.Protocols, ",")
		if protocols == "" {
			protocols = "openai"
		}

		fmt.Printf("  %-20s  %s\n", p.ID, p.Name)
		fmt.Printf("  %-20s  url=%s\n", "", p.BaseURL)
		fmt.Printf("  %-20s  key=%s  protocols=%s\n", "", p.APIKeyEnv, protocols)
		if len(tools) > 0 {
			fmt.Printf("  %-20s  tools=%s\n", "", strings.Join(tools, ","))
		}
		fmt.Printf("  %-20s  models=%d\n", "", len(p.Models))
		fmt.Println()
	}
}

func cmdAdd(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var id, name, baseURL, apiKeyEnv string
	var protocols []string
	var models []schema.Model

	fmt.Print("Provider ID (e.g. agentrouter): ")
	fmt.Scanln(&id)
	if id == "" {
		fmt.Fprintln(os.Stderr, "ID is required")
		os.Exit(1)
	}

	if config.FindProvider(cfg, id) != nil {
		fmt.Fprintf(os.Stderr, "Provider %q already exists. Use edit instead.\n", id)
		os.Exit(1)
	}

	fmt.Print("Display name: ")
	fmt.Scanln(&name)

	fmt.Print("Base URL (e.g. https://api.example.com/v1): ")
	fmt.Scanln(&baseURL)

	fmt.Print("API key env var (e.g. MY_API_KEY): ")
	fmt.Scanln(&apiKeyEnv)

	fmt.Print("Protocols (comma-separated, e.g. anthropic,openai): ")
	var protoStr string
	fmt.Scanln(&protoStr)
	for _, p := range strings.Split(protoStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			protocols = append(protocols, p)
		}
	}

	// Add at least one model
	for {
		var modelID, modelName string
		fmt.Print("Model ID (empty to finish): ")
		fmt.Scanln(&modelID)
		if modelID == "" {
			break
		}
		fmt.Print("Model display name: ")
		fmt.Scanln(&modelName)
		models = append(models, schema.Model{
			ID:   modelID,
			Name: modelName,
		})
	}

	provider := schema.Provider{
		ID:        id,
		Name:      name,
		BaseURL:   baseURL,
		APIKeyEnv: apiKeyEnv,
		Protocols: protocols,
		Models:    models,
	}

	config.UpsertProvider(cfg, provider)
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Provider %q added.\n", id)
}

func cmdSync(args []string) {
	dryRun := false
	toolFilter := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--tool":
			if i+1 < len(args) {
				toolFilter = args[i+1]
				i++
			}
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	generators := []struct {
		name string
		fn   func(*schema.Config, bool) (string, string, error)
	}{
		{"codex", codex.Generate},
		{"pi", pi.Generate},
		{"opencode", opencode.Generate},
		{"hermes", hermes.Generate},
	}

	for _, g := range generators {
		if toolFilter != "" && g.name != toolFilter {
			continue
		}

		path, content, err := g.fn(cfg, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", g.name, err)
			continue
		}

		if dryRun {
			fmt.Printf("=== %s (dry-run) ===\n", g.name)
			fmt.Printf("Would write to: %s\n", path)
			fmt.Println(content)
		} else {
			fmt.Printf("[%s] Synced to %s\n", g.name, path)
		}
	}

	if dryRun {
		fmt.Println("\nDry run complete. No files were modified.")
	} else {
		fmt.Println("\nSync complete.")
	}
}

func cmdDoctor() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	type checkResult struct {
		name     string
		path     string
		validate func() error
		existing []string
	}

	checks := []checkResult{
		{"codex", "", codex.Validate, codex.FindExistingProviders()},
		{"pi", "", pi.Validate, pi.FindExistingProviders()},
		{"opencode", "", opencode.Validate, opencode.FindExistingProviders()},
		{"hermes", "", hermes.Validate, hermes.FindExistingProviders()},
	}

	// Get paths
	p, _ := codex.ConfigPath()
	checks[0].path = p
	p, _ = pi.ConfigPath()
	checks[1].path = p
	p, _ = opencode.ConfigPath()
	checks[2].path = p
	p, _ = hermes.ConfigPath()
	checks[3].path = p

	allOK := true
	for _, c := range checks {
		fmt.Printf("  %-12s ", c.name)

		if err := c.validate(); err != nil {
			fmt.Printf("FAIL: %v\n", err)
			allOK = false
			continue
		}

		if len(c.existing) > 0 {
			fmt.Printf("OK (%d providers: %s)\n", len(c.existing), strings.Join(c.existing, ", "))
		} else {
			fmt.Println("OK (no providers)")
		}
	}

	// Check env vars
	fmt.Println("\n  Environment variables:")
	for _, p := range cfg.Providers {
		if p.APIKeyEnv != "" {
			val := os.Getenv(p.APIKeyEnv)
			if val == "" {
				fmt.Printf("    %-20s  MISSING\n", p.APIKeyEnv)
				allOK = false
			} else {
				fmt.Printf("    %-20s  set\n", p.APIKeyEnv)
			}
		}
	}

	if allOK {
		fmt.Println("\nAll checks passed.")
	} else {
		fmt.Println("\nSome checks failed. See above for details.")
	}
}

func cmdCheck(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph check <provider-id>")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	provider := config.FindProvider(cfg, args[0])
	if provider == nil {
		fmt.Fprintf(os.Stderr, "Provider %q not found\n", args[0])
		os.Exit(1)
	}

	fmt.Printf("Checking %s at %s...\n", provider.Name, provider.BaseURL)

	// Try to hit /models endpoint
	url := strings.TrimRight(provider.BaseURL, "/") + "/v1/models"
	fmt.Printf("  GET %s\n", url)
	// TODO: actual HTTP check with api key
	fmt.Println("  (HTTP check not yet implemented)")
}

func cmdImport() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Track imported providers by ID for deduplication.
	type imported struct {
		provider schema.Provider
		sources  []string
	}
	providers := map[string]*imported{}

	addProvider := func(id string, p schema.Provider, source string) {
		if id == "" {
			return
		}
		if existing, ok := providers[id]; ok {
			// Merge: union protocols, prefer more detailed models, combine sources.
			existing.sources = append(existing.sources, source)
			protoSet := map[string]bool{}
			for _, pr := range existing.provider.Protocols {
				protoSet[pr] = true
			}
			for _, pr := range p.Protocols {
				if !protoSet[pr] {
					existing.provider.Protocols = append(existing.provider.Protocols, pr)
				}
			}
			if len(p.Models) > len(existing.provider.Models) {
				existing.provider.Models = p.Models
			}
			if existing.provider.BaseURL == "" && p.BaseURL != "" {
				existing.provider.BaseURL = p.BaseURL
			}
			if existing.provider.APIKeyEnv == "" && p.APIKeyEnv != "" {
				existing.provider.APIKeyEnv = p.APIKeyEnv
			}
			if existing.provider.Name == "" && p.Name != "" {
				existing.provider.Name = p.Name
			}
			if existing.provider.Headers == nil && p.Headers != nil {
				existing.provider.Headers = p.Headers
			}
			// Merge tools: add tool entries that don't exist yet.
			if p.Tools != nil {
				if existing.provider.Tools == nil {
					existing.provider.Tools = map[string]schema.Tool{}
				}
				for k, v := range p.Tools {
					if _, ok := existing.provider.Tools[k]; !ok {
						existing.provider.Tools[k] = v
					}
				}
			}
		} else {
			providers[id] = &imported{provider: p, sources: []string{source}}
		}
	}

	importCount := map[string]int{}

	// --- Import from opencode ---
	{
		home, err := os.UserHomeDir()
		if err == nil {
			ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
			data, err := os.ReadFile(ocPath)
			if err == nil {
				var oc struct {
					Provider map[string]struct {
						NPM     string `json:"npm"`
						Name    string `json:"name"`
						Options struct {
							BaseURL string `json:"baseURL"`
							APIKey  string `json:"apiKey,omitempty"`
						} `json:"options"`
						Models map[string]struct {
							Name  string `json:"name,omitempty"`
							Limit *struct {
								Context int `json:"context"`
								Output  int `json:"output"`
							} `json:"limit,omitempty"`
						} `json:"models,omitempty"`
					} `json:"provider,omitempty"`
				}
				if json.Unmarshal(data, &oc) == nil {
					for id, prov := range oc.Provider {
						p := schema.Provider{
							ID:      id,
							Name:    prov.Name,
							BaseURL: prov.Options.BaseURL,
							Tools: map[string]schema.Tool{
								"opencode": {Enabled: true, NPM: prov.NPM},
							},
						}
						// Extract env var from "{env:MY_KEY}" format.
						if k := prov.Options.APIKey; strings.HasPrefix(k, "{env:") && strings.HasSuffix(k, "}") {
							p.APIKeyEnv = k[5 : len(k)-1]
						}
						// Infer protocol from npm package.
						if strings.Contains(prov.NPM, "anthropic") {
							p.Protocols = []string{"anthropic"}
						} else {
							p.Protocols = []string{"openai"}
						}
						for mid, m := range prov.Models {
							sm := schema.Model{ID: mid, Name: m.Name}
							if m.Limit != nil {
								sm.ContextWindow = m.Limit.Context
								sm.MaxOutput = m.Limit.Output
							}
							p.Models = append(p.Models, sm)
						}
						addProvider(id, p, "opencode")
					}
					importCount["opencode"] = len(oc.Provider)
				}
			}
		}
	}

	// --- Import from hermes ---
	{
		home, err := os.UserHomeDir()
		if err == nil {
			hPath := filepath.Join(home, ".hermes", "config.yaml")
			data, err := os.ReadFile(hPath)
			if err == nil {
				var raw map[string]interface{}
				if yaml.Unmarshal(data, &raw) == nil {
					// Modern format: providers dict.
					if provs, ok := raw["providers"].(map[string]interface{}); ok {
						for id, v := range provs {
							entry, ok := v.(map[string]interface{})
							if !ok {
								continue
							}
							p := schema.Provider{ID: id}
							if name, ok := entry["name"].(string); ok {
								p.Name = name
							}
							if api, ok := entry["api"].(string); ok {
								p.BaseURL = strings.TrimSuffix(api, "/v1")
							}
							if keyEnv, ok := entry["key_env"].(string); ok {
								p.APIKeyEnv = keyEnv
							}
							if apiMode, ok := entry["api_mode"].(string); ok {
								switch apiMode {
								case "anthropic_messages":
									p.Protocols = []string{"anthropic"}
								case "chat_completions":
									p.Protocols = []string{"openai"}
								}
							}
							if headers, ok := entry["extra_headers"].(map[string]interface{}); ok {
								p.Headers = map[string]string{}
								for hk, hv := range headers {
									if s, ok := hv.(string); ok {
										p.Headers[hk] = s
									}
								}
							}
							if models, ok := entry["models"].([]interface{}); ok {
								for _, m := range models {
									if mid, ok := m.(string); ok {
										p.Models = append(p.Models, schema.Model{ID: mid})
									}
								}
							}
							p.Tools = map[string]schema.Tool{
								"hermes": {Enabled: true},
							}
							addProvider(id, p, "hermes")
						}
						importCount["hermes"] = len(provs)
					}
					// Legacy format: custom_providers list.
					if cps, ok := raw["custom_providers"].([]interface{}); ok {
						for _, cp := range cps {
							entry, ok := cp.(map[string]interface{})
							if !ok {
								continue
							}
							id, _ := entry["name"].(string)
							if id == "" {
								continue
							}
							p := schema.Provider{ID: id}
							if name, ok := entry["name"].(string); ok {
								p.Name = name
							}
							if bu, ok := entry["base_url"].(string); ok {
								p.BaseURL = bu
							}
							if keyEnv, ok := entry["key_env"].(string); ok {
								p.APIKeyEnv = keyEnv
							}
							if models, ok := entry["models"].([]interface{}); ok {
								for _, m := range models {
									if mid, ok := m.(string); ok {
										p.Models = append(p.Models, schema.Model{ID: mid})
									}
								}
							}
							p.Protocols = []string{"openai"}
							p.Tools = map[string]schema.Tool{
								"hermes": {Enabled: true},
							}
							addProvider(id, p, "hermes")
						}
						importCount["hermes"] += len(cps)
					}
				}
			}
		}
	}

	// --- Import from codex ---
	{
		home, err := os.UserHomeDir()
		if err == nil {
			cPath := filepath.Join(home, ".codex", "config.toml")
			data, err := os.ReadFile(cPath)
			if err == nil {
				var raw map[string]interface{}
				if toml.Unmarshal(data, &raw) == nil {
					if mp, ok := raw["model_providers"].(map[string]interface{}); ok {
						for id, v := range mp {
							entry, ok := v.(map[string]interface{})
							if !ok {
								continue
							}
							p := schema.Provider{ID: id}
							if name, ok := entry["name"].(string); ok {
								p.Name = name
							}
							if bu, ok := entry["base_url"].(string); ok {
								p.BaseURL = bu
							}
							if envKey, ok := entry["env_key"].(string); ok {
								p.APIKeyEnv = envKey
							}
							if headers, ok := entry["http_headers"].(map[string]interface{}); ok {
								p.Headers = map[string]string{}
								for hk, hv := range headers {
									if s, ok := hv.(string); ok {
										p.Headers[hk] = s
									}
								}
							}
							// Codex only supports openai responses wire api.
							p.Protocols = []string{"openai"}
							p.Tools = map[string]schema.Tool{
								"codex": {Enabled: true},
							}
							addProvider(id, p, "codex")
						}
						importCount["codex"] = len(mp)
					}
				}
			}
		}
	}

	// --- Import from pi ---
	{
		home, err := os.UserHomeDir()
		if err == nil {
			piPath := filepath.Join(home, ".pi", "agent", "models.json")
			data, err := os.ReadFile(piPath)
			if err == nil {
				var raw struct {
					Providers map[string]struct {
						Name    string `json:"name"`
						BaseURL string `json:"baseUrl"`
						API     string `json:"api"`
						APIKey  string `json:"apiKey,omitempty"`
						Headers map[string]string `json:"headers,omitempty"`
						Models  []struct {
							ID            string `json:"id"`
							Name          string `json:"name"`
							Reasoning     bool   `json:"reasoning"`
							ContextWindow int    `json:"contextWindow"`
							MaxTokens     int    `json:"maxTokens"`
							Cost          *struct {
								Input      float64 `json:"input"`
								Output     float64 `json:"output"`
								CacheRead  float64 `json:"cacheRead"`
								CacheWrite float64 `json:"cacheWrite"`
							} `json:"cost,omitempty"`
						} `json:"models"`
					} `json:"providers"`
				}
				if json.Unmarshal(data, &raw) == nil {
					for id, prov := range raw.Providers {
						p := schema.Provider{
							ID:      id,
							Name:    prov.Name,
							BaseURL: prov.BaseURL,
							Headers: prov.Headers,
						}
						// Strip "$" prefix from apiKey.
						if k := prov.APIKey; strings.HasPrefix(k, "$") {
							p.APIKeyEnv = k[1:]
						}
						// Map pi api field to protocols.
						switch prov.API {
						case "anthropic-messages":
							p.Protocols = []string{"anthropic"}
						case "openai-completions":
							p.Protocols = []string{"openai"}
						default:
							p.Protocols = []string{"openai"}
						}
						for _, m := range prov.Models {
							sm := schema.Model{
								ID:            m.ID,
								Name:          m.Name,
								Reasoning:     m.Reasoning,
								ContextWindow: m.ContextWindow,
								MaxOutput:     m.MaxTokens,
							}
							if m.Cost != nil {
								sm.Cost = &schema.ModelCost{
									Input:      m.Cost.Input,
									Output:     m.Cost.Output,
									CacheRead:  m.Cost.CacheRead,
									CacheWrite: m.Cost.CacheWrite,
								}
							}
							p.Models = append(p.Models, sm)
						}
						p.Tools = map[string]schema.Tool{
							"pi": {Enabled: true},
						}
						addProvider(id, p, "pi")
					}
					importCount["pi"] = len(raw.Providers)
				}
			}
		}
	}

	// Print import summary.
	fmt.Println("Importing from existing tool configs...")
	for _, tool := range []string{"opencode", "hermes", "codex", "pi"} {
		n := importCount[tool]
		if n == 0 {
			fmt.Printf("  %-10s (none found)\n", tool)
		} else {
			fmt.Printf("  %-10s %d providers\n", tool, n)
		}
	}

	// Upsert all imported providers into canonical config.
	for _, imp := range providers {
		config.UpsertProvider(cfg, imp.provider)
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	// Print per-provider details.
	fmt.Printf("\nImported %d provider(s):\n", len(providers))
	for id, imp := range providers {
		fmt.Printf("  %-20s  %s\n", id, imp.provider.Name)
		fmt.Printf("  %-20s  from: %s\n", "", strings.Join(imp.sources, ", "))
		fmt.Printf("  %-20s  models: %d\n", "", len(imp.provider.Models))
	}

	fmt.Printf("\nCanonical config now has %d provider(s).\n", len(cfg.Providers))
}

func cmdServe() {
	if err := gui.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
