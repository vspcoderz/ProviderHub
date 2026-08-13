# Provider Hub (`ph`)

**One source of truth for your LLM providers — synced across every AI coding tool.**

Provider Hub is a single CLI that manages your LLM provider configurations
(router gateways, proxies, custom endpoints) and deploys them to **codex, pi,
opencode, hermes**, and even **Claude Code** — so you add a model once and use
it everywhere.

```bash
ph add                          # add a provider once
ph sync                         # deploy it to every tool
ph hsi setup                    # enable ph-claude / ph-codex / ph-pi / ph-opencode
ph-claude                        # run Claude Code against your routers
```

---

## Highlights

- **One config, many tools** — canonical `~/.config/provider-hub/providers.yaml`
  is the single source of truth; `ph sync` writes provider blocks into every
  harness's native config format.
- **Harness wrappers** — `ph-claude`, `ph-codex`, `ph-pi`, `ph-opencode` inject
  your routers into a real harness through isolated per-run configs + env vars,
  leaving the harness's own config, trust, and permissions untouched.
- **Codex works on chat-only routers** — a bundled local
  Responses→ChatCompletions translation proxy makes codex usable with new-api
  gateways (agentrouter, gorouter) that only speak Chat Completions.
- **Claude Code model picker** — gateway model discovery is enabled, so `/model`
  lists your router's models (not just the default).
- **Automatic protocol handling** — trailing `/v1` is stripped/added per protocol
  so `anthropic-messages` and OpenAI-style APIs hit the right path every time.
- **Health checking** — `ph check <id>` probes a provider and auto-discovers
  models via `/models`.
- **Web GUI** — `ph serve` launches a management dashboard at
  `http://localhost:7357`.
- **Secrets managed safely** — API keys live in a 0600 keystore, never in the
  canonical config, and are written per-tool in the way each harness expects.
- **Backups everywhere** — every write is preceded by a timestamped `.bak`.

## Supported tools

| Tool | Config written to | Format | Wire APIs |
|------|-------------------|--------|-----------|
| codex | `~/.codex/config.toml` | TOML | OpenAI Responses |
| pi | `~/.pi/agent/models.json` | JSON | OpenAI Chat / Responses, Anthropic Messages |
| opencode | `~/.config/opencode/opencode.json` | JSON | Anthropic, OpenAI, OpenAI-compatible (npm SDK) |
| hermes | `~/.hermes/config.yaml` | YAML | anthropic_messages, chat_completions, codex_responses |
| claude | env-driven (wrapper) | env vars | Anthropic Messages |

## Requirements

- Go 1.24+
- The harness binaries you want to use (`claude`, `codex`, `pi`, `opencode`,
  `hermes`) on your `PATH`
- `uv` for the codex translation proxy (one-time venv setup with
  fastapi/uvicorn/httpx)

## Install

```bash
git clone https://github.com/vspcoderz/ProviderHub.git
cd provider-hub
./install.sh                         # Linux/macOS — builds, installs, writes wrappers
# Windows: install.bat
```

Or build manually:

```bash
go build -o ph ./cmd/ph
sudo install -m 755 ph /usr/local/bin/ph   # or copy to a dir on your PATH
```

## Quick start

```bash
# 1. Add a provider (interactive)
ph add
#    ID: agentrouter, Base URL: https://agentrouter.org,
#    API key env: AGENTROUTER_API_KEY, models: claude-opus-5, ...

# 2. Export your key(s)
export AGENTROUTER_API_KEY="sk-..."

# 3. Deploy to every tool
ph sync

# 4. Enable harness wrappers (optional but recommended)
ph hsi setup

# 5. Use it
ph-claude                      # Claude Code against your routers
ph-codex exec "fix the bug"    # codex against your routers
ph-pi -p "summarize README"
ph-opencode run "explain this repo"
```

Override a wrapper's provider/model on the fly:

```bash
ph-codex --provider fxqidian --model glm-5.2 exec "hello"
ph-opencode --model fxqidian/glm-5.2 run "hello"
PH_PROVIDER=agentrouter PH_MODEL=claude-opus-5 ph-claude
```

## Commands

| Command | Description |
|---------|-------------|
| `ph list` | List all providers in canonical config |
| `ph add` | Add a provider interactively (stdin) |
| `ph import` | Import providers from existing tool configs |
| `ph sync [--tool T] [--dry-run]` | Sync to tool configs (default: all) |
| `ph doctor` | Validate all tool configs + env vars |
| `ph check <id>` | Check provider health and detect models via `/models` |
| `ph serve` | Launch web GUI at http://localhost:7357 |
| `ph key set/show/list/rm <id>` | Manage stored API keys |
| `ph system set/show/list/rm <id>` | Per-provider system prompts |
| `ph agent set/show/list/rm <id>` | Per-provider agent memory |
| `ph skill add/show/list/rm <name>` | Manage skills |
| `ph agents-md` | Generate `AGENTS.md` |
| `ph hsi list` | Show harness wrapper defaults |
| `ph hsi set <tool> --provider <id> [--model <id>]` | Set a harness default |
| `ph hsi setup` | Write `ph-claude`, `ph-codex`, `ph-pi`, `ph-opencode` wrappers |
| `ph hsi run <tool> [flags] [--] <args>` | Run a harness with routers injected |
| `ph hsi rm [<tool>]` | Remove wrapper scripts |

## Canonical config

`~/.config/provider-hub/providers.yaml` is the single source of truth:

```yaml
version: 1
providers:
  - id: agentrouter
    name: AgentRouter
    baseUrl: https://agentrouter.org
    apiKeyEnv: AGENTROUTER_API_KEY
    protocols: [anthropic, openai]
    headers:
      User-Agent: claude-cli/2.1.0 (external, cli)   # passes CF-style client gates
    tools:
      codex:    {enabled: true}
      pi:       {enabled: true}
      opencode: {enabled: true, npm: "@ai-sdk/anthropic"}
      hermes:   {enabled: true, apiMode: anthropic_messages}
    models:
      - id: claude-opus-5
        name: Opus 5
        contextWindow: 200000
        maxOutput: 16000
        reasoning: true
        cost: {input: 3, output: 15}
```

## How the wrappers work

Each wrapper isolates the harness from your real config:

- **claude** — `ANTHROPIC_BASE_URL` (trailing `/v1` stripped; Claude appends
  `/v1/messages`), `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL`,
  `ANTHROPIC_SMALL_FAST_MODEL`, and
  `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` so `/model` lists router models.
- **codex** — starts a local Responses→ChatCompletions translation proxy
  (`~/.cache/provider-hub/hsi/proxy/`) and overlays `base_url`, `wire_api`,
  `model_provider`, `model`, and a generated `model_catalog.json` via `-c` on the
  real `~/.codex/config.toml`. Codex runs as a child process so the proxy is
  cleaned up after each run. Keeps trust/permissions intact.
- **pi** — writes a fresh `models.json` into an isolated `PI_CODING_AGENT_DIR`
  and forwards the selected model via `--model <provider>/<model>`.
- **opencode** — points `OPENCODE_CONFIG` at a fresh generated JSON config.

Per-run configs live in `~/.cache/provider-hub/hsi/<tool>/` (0700). Defaults are
stored in `~/.config/provider-hub/hsi.yaml`.

## Provider compatibility notes

- **codex** supports only the OpenAI protocol and, since 0.122+, hard-requires
  `wire_api = "responses"`. Chat-Completions-only routers are bridged by the
  vendored translation proxy (`internal/hsi/api2codex.py`, MIT), which forwards
  your provider `headers` (e.g. the claude UA) upstream.
- **hermes** supports `anthropic_messages`, `chat_completions`, `codex_responses`.
- **opencode** uses npm packages: `@ai-sdk/anthropic`, `@ai-sdk/openai-compatible`,
  `@ai-sdk/openai`.
- **pi** uses `openai-completions`, `openai-responses`, or `anthropic-messages`.
  For `anthropic-messages`, pi appends `/v1/messages` itself, so a canonical
  `baseUrl` ending in `/v1` is stripped automatically.
- **headers** propagate to codex (`http_headers`), pi (`headers`), opencode
  (`options.headers`), and hermes (`extra_headers`). Cloudflare-gated routers
  (gorouter, agentrouter) require
  `headers: {User-Agent: "claude-cli/2.1.0 (external, cli)"}`.

## API keys

Keys are stored in `~/.config/provider-hub/keys.yaml` (0600). Generators write
them where each harness expects:

- **opencode** — inline `apiKey` in `opencode.json`
- **hermes** — `~/.hermes/.env`
- **pi** — inline `apiKey` in `models.json`
- **codex** — references the env var (TOML cannot embed secrets directly)

## Safety

- Timestamped backups before every write (`.bak.<timestamp>`)
- Non-destructive merge — only provider-related keys are modified
- `ph doctor` validates all configs after sync
- `keys.yaml` and per-run cache dirs use 0600/0700 permissions
- Never commit `keys.yaml` — add it to your `.gitignore`

## Installing from source

`install.sh` (Linux/macOS) and `install.bat` (Windows) build the `ph` binary,
install it to a bin directory, add it to `PATH`, and write the harness wrappers.
Override the destination with `PREFIX` (e.g. `PREFIX="$HOME/tools" ./install.sh`).

## Project layout

```
cmd/ph/            CLI entrypoint
internal/agent/    per-provider agent memory
internal/config/   canonical providers.yaml load/save
internal/gen/      per-tool generators (codex, pi, opencode, hermes, openclaude)
internal/gui/      web management dashboard
internal/hsi/      harness wrappers + codex translation proxy
internal/keystore/ encrypted-key storage
internal/schema/   canonical config types
internal/skill/    skills storage
internal/system/   per-provider system prompts
```

## License

MIT — see the `api2codex.py` header for the vendored proxy's original license.