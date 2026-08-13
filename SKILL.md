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

## Harness wrappers (hsi)

`ph hsi` generates thin wrapper binaries (`ph-claude`, `ph-codex`, `ph-pi`, `ph-opencode`)
that run a harness with the provider-hub routers injected, while leaving the
harness's own config untouched. Run the wrapper for router-backed sessions, or
the plain binary for normal usage.

```bash
ph hsi setup              # write wrappers to ~/.local/bin
ph hsi list               # show per-harness defaults + binaries
ph hsi set claude --provider agentrouter --model claude-opus-5
ph hsi set codex --provider gorouter --model claude-opus-4-8
ph hsi run opencode --provider agentrouter --model claude-opus-5 -- <args>   # bypass wrapper
ph hsi rm [<tool>]        # remove wrapper(s)
```

- Per-harness defaults live in `~/.config/provider-hub/hsi.yaml`.
- Override any run with `PH_PROVIDER`/`PH_MODEL` env vars, or `--provider/--model` before `--`.
- Isolation mechanism per harness:
  - **claude** → `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL` env
  - **codex** → runs a local Responses→ChatCompletions translation proxy
  (`~/.cache/provider-hub/hsi/proxy/`) and overlays `base_url`, `wire_api`,
  `model_provider`, `model`, and `model_catalog_json` via `-c` on the real
  `~/.codex/config.toml` (keeps trust/permissions intact). The proxy is
  auto-started per run and torn down afterwards; deps install once via `uv`.
  - **pi** → fresh `PI_CODING_AGENT_DIR` dir with a generated `models.json`
  - **opencode** → fresh `OPENCODE_CONFIG` json file
- Isolated configs are written fresh per run to `~/.cache/provider-hub/hsi/<tool>/` (0700).

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

- **codex** only supports `openai` protocol (Responses API). Codex 0.122+ hard-requires `wire_api =
  "responses"`. Routers that only speak Chat Completions (new-api gateways like agentrouter) are made
  usable by `ph-codex`'s local translation proxy (`internal/hsi/api2codex.py`, vendored MIT), which
  converts Responses→ChatCompletions and forwards the provider's `headers` (e.g. the claude UA) upstream.
  Requires `uv` for the one-time venv setup (fastapi/uvicorn/httpx).
- **hermes** supports `anthropic_messages`, `chat_completions`, `codex_responses` api modes.
- **opencode** uses npm packages: `@ai-sdk/anthropic`, `@ai-sdk/openai-compatible`, `@ai-sdk/openai`.
- **pi** uses `openai-completions`, `openai-responses`, or `anthropic-messages`. For `anthropic-messages`,
  pi appends `/v1/messages` itself, so a canonical `baseUrl` ending in `/v1` is stripped automatically.
- **claude** (Claude Code) is env-driven: `ANTHROPIC_BASE_URL` (trailing `/v1` stripped, Claude appends
  `/v1/messages`), `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`.
  `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` is set so the `/model` picker lists gateway models
  from the provider's `/v1/models` (opt-in since Claude Code 2.1.129).
- **headers** from `providers.yaml` propagate to codex (`http_headers`), pi (`headers`), opencode
  (`options.headers`), and hermes (`extra_headers`). Cloudflare-gated routers (gorouter, agentrouter) require
  `headers: {User-Agent: "claude-cli/2.1.0 (external, cli)"}` to pass their client gate.

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
