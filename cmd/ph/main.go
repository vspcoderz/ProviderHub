package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vspcoderz/provider-hub/internal/agent"
	"github.com/vspcoderz/provider-hub/internal/config"
	"github.com/vspcoderz/provider-hub/internal/gen/codex"
	"github.com/vspcoderz/provider-hub/internal/gen/hermes"
	"github.com/vspcoderz/provider-hub/internal/gen/openclaude"
	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/gui"
	"github.com/vspcoderz/provider-hub/internal/hsi"
	"github.com/vspcoderz/provider-hub/internal/keystore"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/skill"
	"github.com/vspcoderz/provider-hub/internal/system"
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
	case "key":
		cmdKey(args)
	case "system":
		cmdSystem(args)
	case "agent":
		cmdAgent(args)
	case "skill":
		cmdSkill(args)
	case "hsi":
		cmdHsi(args)
	case "agents-md":
		cmdAgentsMd(args)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		fmt.Fprintln(os.Stderr, "Available: list, add, sync, doctor, check, import, serve, key, system, agent, skill, hsi, agents-md, help")
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`provider-hub (ph) - manage LLM providers across codex, pi, opencode, hermes

Usage:
  ph list              List all providers in canonical config
  ph add               Add a provider interactively (stdin)
  ph import            Import providers from existing tool configs
  ph sync [--tool T]   Sync to tool configs (default: all)
  ph doctor            Validate all tool configs + env vars
  ph check <id>        Check provider health and detect models via /models
  ph serve             Launch web GUI at http://localhost:7357
  ph key set <id>      Set API key for a provider
  ph key show <id>     Show masked key for a provider
  ph key list          List all stored keys
  ph key rm <id>       Remove stored key for a provider
  ph system set <id>   Manage per-provider system prompts (set|show|list|rm)
  ph agent set <id>    Manage per-provider agent memory (set|show|list|rm)
  ph skill add <name>  Manage skills (add|show|list|rm)
  ph agents-md         Generate AGENTS.md
  ph hsi list          Show harness wrapper defaults
  ph hsi set <tool> --provider <id> [--model <id>]
                       Set default provider/model for a harness wrapper
  ph hsi setup         Write ph-claude, ph-codex, ph-pi, ph-opencode wrappers
  ph hsi run <tool> [--provider <id>] [--model <id>] [--] <args>
                       Run a harness with router providers injected (used by wrappers)
  ph hsi rm [<tool>]   Remove wrapper scripts

Flags:
  --dry-run            Show what would be written without writing
  --tool codex|pi|opencode|hermes|openclaude   Target specific tool

Examples:
  ph list
  ph sync --tool opencode
  ph sync --dry-run
  ph doctor
  ph check agentrouter
  ph serve
  ph key set agentrouter
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

	var id, name, baseURL, apiKeyEnv, modelName string
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

	fmt.Print("Protocols (comma-separated, e.g. anthropic,openai; empty to auto-detect): ")
	var protoStr string
	fmt.Scanln(&protoStr)
	for _, p := range strings.Split(protoStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			protocols = append(protocols, p)
		}
	}

	if len(protocols) == 0 && baseURL != "" {
		key := resolveKey(apiKeyEnv, id)
		if detected := detectProtocols(baseURL, key); len(detected) > 0 {
			protocols = detected
			fmt.Printf("Auto-detected protocols: %s\n", strings.Join(protocols, ","))
		}
	}

	fmt.Print("Model ID (empty to finish): ")
	var modelID string
	for {
		fmt.Scanln(&modelID)
		if modelID == "" {
			break
		}
		fmt.Print("Model display name: ")
		fmt.Scanln(&modelName)
		models = append(models, schema.Model{ID: modelID, Name: modelName})
		fmt.Print("Model ID (empty to finish): ")
		modelID = ""
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
				toolFilter = strings.ToLower(args[i+1])
				i++
			}
		}
	}

	valid := map[string]bool{"codex": true, "pi": true, "opencode": true, "hermes": true, "openclaude": true}
	if toolFilter != "" && !valid[toolFilter] {
		fmt.Fprintf(os.Stderr, "Invalid tool: %s\n", toolFilter)
		os.Exit(1)
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
		{"openclaude", openclaude.Generate},
	}

	for _, gen := range generators {
		if toolFilter != "" && gen.name != toolFilter {
			continue
		}

		path, content, err := gen.fn(cfg, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", gen.name, err)
			continue
		}

		if dryRun {
			fmt.Printf("=== %s (dry-run) ===\n", gen.name)
			fmt.Printf("Would write to: %s\n", path)
			fmt.Println(content)
		} else {
			fmt.Printf("[%s] Synced to %s\n", gen.name, path)
		}
	}
}

func cmdDoctor() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	checks := []struct {
		name     string
		validate func() error
		existing []string
	}{
		{"codex", codex.Validate, codex.FindExistingProviders()},
		{"pi", pi.Validate, pi.FindExistingProviders()},
		{"opencode", opencode.Validate, opencode.FindExistingProviders()},
		{"hermes", hermes.Validate, hermes.FindExistingProviders()},
		{"openclaude", openclaude.Validate, openclaude.FindExistingProviders()},
	}

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

	fmt.Println("\n  Environment variables:")
	for _, p := range cfg.Providers {
		if p.APIKeyEnv != "" {
			if os.Getenv(p.APIKeyEnv) == "" {
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
	key := resolveKey(provider.APIKeyEnv, provider.ID)
	if key == "" {
		fmt.Println("  Warning: no API key found (env or keystore); request may be unauthorized.")
	}

	ids, err := fetchModels(provider.BaseURL, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Failed: %v\n", err)
		os.Exit(1)
	}
	if len(ids) == 0 {
		fmt.Println("  Reachable, but no models returned.")
		return
	}

	fmt.Printf("  Detected %d models:\n", len(ids))
	for _, id := range ids {
		fmt.Printf("    - %s\n", id)
	}

	fmt.Print("\nSave these models to the provider config? [y/N]: ")
	var answer string
	fmt.Scanln(&answer)
	if strings.EqualFold(strings.TrimSpace(answer), "y") {
		existing := map[string]bool{}
		for _, m := range provider.Models {
			existing[m.ID] = true
		}
		for _, id := range ids {
			if !existing[id] {
				provider.Models = append(provider.Models, schema.Model{ID: id})
			}
		}
		config.UpsertProvider(cfg, *provider)
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Models saved.")
	}
}

func cmdImport() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Importing providers from existing tool configs...")

	sources := []struct {
		name string
		fn   func() []string
	}{
		{"codex", codex.FindExistingProviders},
		{"pi", pi.FindExistingProviders},
		{"opencode", opencode.FindExistingProviders},
		{"hermes", hermes.FindExistingProviders},
		{"openclaude", openclaude.FindExistingProviders},
	}

	imported := 0
	for _, src := range sources {
		ids := src.fn()
		for _, id := range ids {
			if config.FindProvider(cfg, id) != nil {
				continue
			}
			config.UpsertProvider(cfg, schema.Provider{
				ID:   id,
				Name: id,
			})
			fmt.Printf("  [%s] imported provider %q\n", src.name, id)
			imported++
		}
	}

	if imported == 0 {
		fmt.Println("No new providers found to import.")
		return
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Import complete: %d provider(s) added. Run 'ph list' to review.\n", imported)
}

func cmdServe() {
	if err := gui.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdKey(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph key <set|show|list|rm> [provider-id]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "set":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph key set <provider-id>")
			os.Exit(1)
		}
		id := subArgs[0]

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if config.FindProvider(cfg, id) == nil {
			fmt.Fprintf(os.Stderr, "Provider %q not found\n", id)
			os.Exit(1)
		}

		fmt.Print("Enter API key for " + id + ": ")
		var key string
		fmt.Scanln(&key)
		if key == "" {
			fmt.Fprintln(os.Stderr, "Key cannot be empty")
			os.Exit(1)
		}
		if err := keystore.Set(id, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Key set for %s\n", id)

	case "show":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph key show <provider-id>")
			os.Exit(1)
		}
		id := subArgs[0]
		key, err := keystore.Get(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if key == "" {
			fmt.Printf("No key stored for %s\n", id)
		} else {
			fmt.Printf("%s: %s\n", id, keystore.Mask(key))
		}

	case "list":
		keys, err := keystore.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(keys) == 0 {
			fmt.Println("No keys stored.")
			return
		}
		for id, key := range keys {
			fmt.Printf("  %-20s  %s\n", id, keystore.Mask(key))
		}

	case "rm":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph key rm <provider-id>")
			os.Exit(1)
		}
		id := subArgs[0]
		if err := keystore.Remove(id); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Key removed for %s\n", id)

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: key %s\n", sub)
		fmt.Fprintln(os.Stderr, "Available: set, show, list, rm")
		os.Exit(1)
	}
}

func cmdSystem(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph system <set|show|list|rm> [provider-id]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "set":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph system set <provider-id>")
			os.Exit(1)
		}
		id := subArgs[0]
		fmt.Print("Enter system prompt for " + id + " (single line): ")
		prompt := readLine()
		if prompt == "" {
			fmt.Fprintln(os.Stderr, "Prompt cannot be empty")
			os.Exit(1)
		}
		if err := system.SetSystemPrompt(id, prompt); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("System prompt set for %s\n", id)

	case "show":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph system show <provider-id>")
			os.Exit(1)
		}
		prompt, err := system.GetSystemPrompt(subArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if prompt == "" {
			fmt.Printf("No system prompt stored for %s\n", subArgs[0])
		} else {
			fmt.Printf("%s: %s\n", subArgs[0], prompt)
		}

	case "list":
		prompts, err := system.ListSystemPrompts()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(prompts) == 0 {
			fmt.Println("No system prompts stored.")
			return
		}
		for id, prompt := range prompts {
			fmt.Printf("  %-20s  %s\n", id, system.MaskSystemPrompt(prompt))
		}

	case "rm":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph system rm <provider-id>")
			os.Exit(1)
		}
		if err := system.RemoveSystemPrompt(subArgs[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("System prompt removed for %s\n", subArgs[0])

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: system %s\n", sub)
		fmt.Fprintln(os.Stderr, "Available: set, show, list, rm")
		os.Exit(1)
	}
}

func cmdAgent(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph agent <set|show|list|rm> [provider-id]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "set":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph agent set <provider-id>")
			os.Exit(1)
		}
		id := subArgs[0]
		fmt.Print("Enter agent memory for " + id + " (single line): ")
		memory := readLine()
		if memory == "" {
			fmt.Fprintln(os.Stderr, "Memory cannot be empty")
			os.Exit(1)
		}
		if err := agent.Set(id, memory); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Agent memory set for %s\n", id)

	case "show":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph agent show <provider-id>")
			os.Exit(1)
		}
		memory, err := agent.Get(subArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if memory == "" {
			fmt.Printf("No agent memory stored for %s\n", subArgs[0])
		} else {
			fmt.Printf("%s: %s\n", subArgs[0], memory)
		}

	case "list":
		memories, err := agent.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(memories) == 0 {
			fmt.Println("No agent memories stored.")
			return
		}
		for id, memory := range memories {
			fmt.Printf("  %-20s  %s\n", id, agent.Mask(memory))
		}

	case "rm":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph agent rm <provider-id>")
			os.Exit(1)
		}
		if err := agent.Remove(subArgs[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Agent memory removed for %s\n", subArgs[0])

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: agent %s\n", sub)
		fmt.Fprintln(os.Stderr, "Available: set, show, list, rm")
		os.Exit(1)
	}
}

func cmdSkill(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph skill <add|show|list|rm> [name]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "add", "set":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph skill add <name>")
			os.Exit(1)
		}
		name := subArgs[0]
		fmt.Print("Enter skill body for " + name + " (single line): ")
		body := readLine()
		if body == "" {
			fmt.Fprintln(os.Stderr, "Skill body cannot be empty")
			os.Exit(1)
		}
		if err := skill.Set(name, body); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Skill %q saved.\n", name)

	case "show":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph skill show <name>")
			os.Exit(1)
		}
		body, err := skill.Get(subArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if body == "" {
			fmt.Printf("No skill stored named %s\n", subArgs[0])
		} else {
			fmt.Printf("%s:\n%s\n", subArgs[0], body)
		}

	case "list":
		skills, err := skill.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(skills) == 0 {
			fmt.Println("No skills stored.")
			return
		}
		for name := range skills {
			fmt.Printf("  %s\n", name)
		}

	case "rm":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph skill rm <name>")
			os.Exit(1)
		}
		if err := skill.Remove(subArgs[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Skill %q removed.\n", subArgs[0])

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: skill %s\n", sub)
		fmt.Fprintln(os.Stderr, "Available: add, show, list, rm")
		os.Exit(1)
	}
}

// cmdHsi manages the harness-specific integration wrappers.
func cmdHsi(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: ph hsi <list|set|setup|run|rm> [tool]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list":
		if err := hsi.List(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "set":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph hsi set <tool> --provider <id> [--model <id>]")
			os.Exit(1)
		}
		tool := subArgs[0]
		provider, model := "", ""
		for i := 1; i < len(subArgs); i++ {
			switch subArgs[i] {
			case "--provider":
				if i+1 < len(subArgs) {
					provider = subArgs[i+1]
					i++
				}
			case "--model":
				if i+1 < len(subArgs) {
					model = subArgs[i+1]
					i++
				}
			}
		}
		if err := hsi.Set(tool, provider, model); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ph-%s default: %s/%s\n", tool, provider, model)

	case "setup":
		dir := ""
		for i := 0; i < len(subArgs); i++ {
			if subArgs[i] == "--dir" && i+1 < len(subArgs) {
				dir = subArgs[i+1]
				i++
			}
		}
		fmt.Println("Writing harness wrappers:")
		if err := hsi.Setup(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Done. Run e.g. 'ph-claude' to use your router providers, or 'claude' normally.")

	case "run":
		if len(subArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: ph hsi run <tool> [--provider <id>] [--model <id>] [--] <args>")
			os.Exit(1)
		}
		tool := subArgs[0]
		if err := hsi.Run(tool, subArgs[1:]); err != nil {
			if ee, ok := err.(*hsi.ExitError); ok {
				os.Exit(ee.Code)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "rm":
		name := ""
		if len(subArgs) > 0 {
			name = subArgs[0]
		}
		if err := hsi.Remove(name, ""); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown hsi subcommand: %s\n", sub)
		fmt.Fprintln(os.Stderr, "Available: list, set, setup, run, rm")
		os.Exit(1)
	}
}

// --- helpers ---

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// resolveKey returns the API key from the env var if set, else the keystore.
func resolveKey(envVar, providerID string) string {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}
	if k, err := keystore.Get(providerID); err == nil {
		return k
	}
	return ""
}

// modelsResponse matches the OpenAI-style /models payload.
type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// fetchModels calls GET {baseURL}/models and returns the model IDs.
func fetchModels(baseURL, key string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var mr modelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}
	ids := make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// detectProtocols probes the base URL to guess the wire protocol(s).
// An OpenAI-style /models endpoint implies "openai"; an Anthropic host
// (or a working /v1/messages surface) implies "anthropic".
func detectProtocols(baseURL, key string) []string {
	var protocols []string

	if _, err := fetchModels(baseURL, key); err == nil {
		protocols = append(protocols, "openai")
	}

	if strings.Contains(strings.ToLower(baseURL), "anthropic") || anthropicReachable(baseURL, key) {
		protocols = append(protocols, "anthropic")
	}

	return protocols
}

// anthropicReachable does a best-effort probe of the Anthropic messages
// surface. A 400/401/405 still indicates the endpoint exists.
func anthropicReachable(baseURL, key string) bool {
	url := strings.TrimRight(baseURL, "/") + "/messages"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Any structured HTTP response (even an error) means the surface exists.
	return resp.StatusCode < 500
}
