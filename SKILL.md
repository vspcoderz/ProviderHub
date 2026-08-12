# provider-hub (ph)

A unified manager for LLM provider configurations across codex, pi, opencode, and hermes.

## What it does

Manages a single source of truth (`~/.config/provider-hub/providers.yaml`) and syncs provider configs to 4 tools:
- **codex** → `~/.codex/config.toml` (TOML, `[model_providers.*]`)
- **pi** → `~/.pi/agent/models.json` (JSON, providers map)
- **opencode** → `~/.config/opencode/opencode.json` (JSON, provider map + npm SDK)
- **hermes** → `~/.hermes/config.yaml` (YAML, providers dict)

## Commands

```bash
ph list                  # List all providers
ph add                   # Add provider interactively
ph import                # Import from existing tool configs
ph sync                  # Sync to all tools
ph sync --dry-run        # Preview without writing
ph sync --tool opencode  # Sync to one tool only
ph doctor                # Validate configs + check env vars
ph serve                 # Web GUI at http://localhost:7357

ph key set <id>          # Store API key for a provider
ph key show <id>         # Show masked key
ph key list              # List all stored keys
ph key rm <id>           # Remove stored key
```

## Canonical config format

```yaml
# ~/.config/provider-hub/providers.yaml
version: 1
providers:
  - id: myprovider
    name: My Provider
    baseUrl: https://api.example.com/v1
    apiKeyEnv: MY_API_KEY        # env var name
    protocols: [anthropic, openai]  # which wire APIs
    headers:
      User-Agent: custom/1.0
    tools:
      codex:   {enabled: true}
      pi:      {enabled: true}
      opencode:{enabled: true, npm: "@ai-sdk/anthropic"}
      hermes:  {enabled: true, apiMode: anthropic_messages}
    models:
      - id: claude-sonnet-4
        name: Claude Sonnet 4
        contextWindow: 200000
        maxOutput: 16000
        reasoning: true
        cost: {input: 3, output: 15}
```

## Provider compatibility

- **codex** only supports `openai` protocol (Responses API). Anthropic-only providers are flagged incompatible.
- **hermes** supports `anthropic_messages`, `chat_completions`, `codex_responses` api modes.
- **opencode** uses npm packages: `@ai-sdk/anthropic`, `@ai-sdk/openai-compatible`, `@ai-sdk/openai`.
- **pi** uses `openai-completions`, `openai-responses`, or `anthropic-messages`.

## API keys

Keys are stored separately in `~/.config/provider-hub/keys.yaml` (0600 perms). Generators write keys to:
- **opencode**: inline in `opencode.json` `apiKey` field
- **hermes**: to `~/.hermes/.env`
- **pi**: inline in `models.json` `apiKey` field
- **codex**: references env var (cannot embed directly)

## Safety

- Backups created before every write (`.bak.<timestamp>`)
- Non-destructive merge: only provider-related keys are modified
- `ph doctor` validates all configs after sync
- Never commit `keys.yaml` (add to `.gitignore`)
