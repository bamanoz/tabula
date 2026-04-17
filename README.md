# Tabula

A modular AI agent. Small Go kernel, pluggable everything — LLM providers, gateways, tools, personality.


## Install

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Requires Python 3.11+. Installs to `~/.tabula/` (override with `TABULA_HOME`).

To install a specific version: `VERSION=v1.0.0 bash install.sh`

To install with optional bundles: `BUNDLES=caveman bash install.sh`

<details>
<summary>Windows</summary>

```powershell
irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1 | iex
# With bundles: $env:BUNDLES='caveman'; irm ... | iex
```
</details>

<details>
<summary>Install from source</summary>

```bash
git clone https://github.com/bamanoz/tabula.git && cd tabula
./scripts/install-dev.sh    # macOS/Linux
# or
./scripts/install-dev.ps1   # Windows
# With bundles: BUNDLES=caveman ./scripts/install-dev.sh
```

Requires Go 1.26+ and Python 3.11+.

Source install copies `boot.py`, `boot-cicd.py`, skills, templates, service units, and launchers into `TABULA_HOME`, then builds `bin/tabula`.
</details>

<details>
<summary>What gets installed</summary>

```
~/.tabula/
├── bin/tabula              # Go kernel binary
├── bin/tabula-cli          # Launch: CLI session
├── bin/tabula-api          # Launch: API gateway
├── bin/tabula-server       # Launch: persistent server
├── service/                # launchd/systemd templates
├── boot.py                 # Skill discovery & prompt assembly
├── boot-cicd.py            # Minimal CI/CD boot script
├── templates/              # System prompt templates
├── skills/                 # Installed skills (+ symlinks to bundles)
├── bundles/                # Optional skill bundles (if installed)
├── memory/                 # Persistent memory
├── logs/                   # Kernel logs
└── .venv/                  # Python dependencies
```
</details>

## Quick start

Add your API key to `~/.tabula/.env` and connect:

```bash
echo "ANTHROPIC_API_KEY=sk-..." >> ~/.tabula/.env
tabula-cli
```

On first launch, Tabula will introduce itself and ask you to set up its identity together.

## Running

The kernel has two modes:

```bash
# Persistent server — connects skills, stays running
tabula serve

# One-shot — runs a single prompt and exits
tabula run --prompt "what is 2+2?"
```

`tabula-server` sets a sane default automatically:

```bash
tabula-server
```

Direct `tabula serve` / `tabula run` invocations still require `TABULA_BOOT` to be set, pointing to a boot script:

```bash
# Interactive mode (full skill set)
TABULA_BOOT="python3 boot.py" tabula serve

# CI/CD mode (minimal, driver only)
TABULA_BOOT="python3 boot-cicd.py" tabula run --prompt "summarize this file"
```

`boot-cicd.py` is a minimal boot script that spawns only the LLM driver — no MCP, no hooks, no session registry. It is installed at `$TABULA_HOME/boot-cicd.py`.

## Features

- **Multi-provider** — Anthropic (Claude) and OpenAI out of the box, switchable via env var
- **Parallel subagents** — LLM spawns independent sub-agents for concurrent tasks
- **OpenAI-compatible API** — Drop-in `/v1/chat/completions` and `/v1/responses` endpoints with SSE streaming
- **Skill system** — Modular Python skills that connect via WebSocket, auto-discovered at boot
- **MCP bridge** — Connect any Model Context Protocol server as a tool source
- **Persistent memory** — Save, search, and recall facts across sessions
- **Hooks** — Before/after events for messages, tool calls, spawns, and sessions
- **Cron & timers** — Scheduled tasks and reminders
- **Project files** — Editable identity, personality, and workspace rules (`IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md`)
- **Session isolation** — Each conversation and subagent runs in its own session scope

## Architecture

The kernel is a Go binary — a WebSocket server that routes messages between skills via session-scoped pub/sub.

**Boot sequence:**
1. Kernel runs the boot script specified by `TABULA_BOOT`
2. Boot scans `skills/` for `SKILL.md` files (follows symlinks for bundles), assembles system prompt, discovers tools
3. Boot outputs JSON config → kernel starts WebSocket server, spawns skill processes
4. Skills connect, join sessions, begin message exchange

**Skills are processes.** Each skill connects via WebSocket, declares its message types, and joins a session. The kernel routes messages — skills don't know about each other. Drivers talk to LLMs, gateways talk to users, tool skills execute commands.

**Subagents are autonomous.** The LLM driver spawns sub-agent processes that run their own LLM loop in separate sessions. Multiple subagents run in parallel; results are collected and aggregated back into the parent conversation.

### Skills

| Skill | Description |
|-------|-------------|
| `driver-anthropic` | Claude API — streaming, tool use, subagent orchestration |
| `driver-openai` | OpenAI Responses API — same protocol, same capabilities |
| `gateway-cli` | Interactive terminal UI with markdown rendering |
| `gateway-api` | OpenAI-compatible HTTP API with SSE streaming |
| `gateway-telegram` | Telegram bot gateway |
| `subagent-anthropic` | Autonomous Claude sub-agent for parallel tasks |
| `subagent-openai` | Autonomous OpenAI sub-agent for parallel tasks |
| `memory` | Persistent memory — save, search, list, delete |
| `files` | Read, write, and edit files |
| `pair` | Universal pairing for gateways (Telegram, etc.) |
| `sessions` | Cross-session messaging |
| `mcp` | Model Context Protocol bridge |
| `cron` | Scheduled task execution |
| `hook-logger` | Audit logger (JSONL) |
| `hook-permissions` | Permission enforcement — blocks denied tool calls |

### Bundles

Bundles are optional thematic skill packages. They live in `bundles/` and are symlinked into `skills/` at install time, so the agent sees a flat layout.

| Bundle | Skills | Description |
|--------|--------|-------------|
| `caveman` | 6 | Ultra-compressed communication — commit messages, code reviews, memory compression |

Install with: `BUNDLES=caveman bash install.sh` or `BUNDLES=all` for everything.

### Kernel tools

| Tool | Description |
|------|-------------|
| `EXEC` | Run a command, return stdout (capped at 16KB) |
| `SPAWN` | Start a background process, return PID |
| `KILL` | Stop a process by PID |
| `LIST` | List spawned processes |

### API gateway

Start the kernel, then connect the API gateway:

```bash
tabula-server
TABULA_API_PORT=8090 tabula-api
tabula-api                              # connect to running kernel
```

```bash
curl http://localhost:8090/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"tabula","messages":[{"role":"user","content":"hello"}]}'
```

<details>
<summary>Protocol reference</summary>

JSON messages over WebSocket. Each message has a `type` field:

| Message | Direction | Description |
|---------|-----------|-------------|
| `connect` | skill → kernel | Register with name, sends[], receives[] |
| `connected` | kernel → skill | Acknowledge with client ID |
| `join` | skill → kernel | Join a session |
| `joined` | kernel → skill | Acknowledge session join |
| `member_joined` | kernel → session | Broadcast: new client in session |
| `init` | kernel → skill | System prompt + tools |
| `message` | any → any | Text message |
| `stream_start` | driver → gateway | LLM response begins |
| `stream_delta` | driver → gateway | Token chunk |
| `stream_end` | driver → gateway | LLM response complete |
| `tool_use` | driver → kernel | LLM requests tool call |
| `tool_result` | kernel → driver | Tool execution result |
| `done` | driver → gateway | Turn complete |
| `cancel` | gateway → kernel | Abort current operation |
| `error` | kernel → session | Error notification |

</details>

## Adding skills

Create a directory in `skills/` with a `SKILL.md` (frontmatter + docs) and a `run.py` entry point. The skill connects to the kernel via WebSocket, declares its message types, and joins a session.

See `skills/skill-contract/SKILL.md` for the full specification.

## Testing

The verification matrix is split into explicit layers so we can run fast checks locally and keep heavier runtime flows targeted:

```bash
make test-unit
make test-smoke
make test-e2e
make test-contract
```

Python layers are classified with `pytest` markers via `conftest.py`; the current matrix is documented in [`tests/README.md`](tests/README.md).

## Configuration

### Permissions

Create `~/.tabula/permissions.json` to control which tools the agent can use:

```json
{
  "rules": [
    {"tool": "EXEC", "command": "rm -rf *", "effect": "deny"},
    {"tool": "EXEC", "command": "git push *--force*", "effect": "deny"},
    {"tool": "write_file", "effect": "deny"},
    {"tool": "*", "effect": "allow"}
  ]
}
```

Rules use glob patterns. Specificity wins: command-level rules override tool-level.
Within same specificity, deny overrides allow. No file = allow all.

<details>
<summary>Environment variables</summary>

| Variable | Description | Default |
|----------|-------------|---------|
| `TABULA_HOME` | Workspace directory | `~/.tabula` |
| `TABULA_BOOT` | Boot command to run (required) | none |
| `TABULA_URL` | Kernel WebSocket URL | `ws://localhost:8089/ws` |
| `TABULA_PROVIDER` | LLM provider (`anthropic`, `openai`) | `anthropic` |
| `BUNDLES` | Bundles to install (`caveman`, `all`, comma-separated) | none |
| `TABULA_VERBOSE` | Verbose logging (`1` to enable) | unset |
| `TABULA_API_PORT` | API gateway port | unset |
| `TABULA_API_AUTH` | API Bearer token | unset |
| `TABULA_ALLOWED_ORIGINS` | Comma-separated allowed WebSocket origins; default allows only localhost / same-host origins | unset |
| `TABULA_MAX_SPAWN_DEPTH` | Max subagent nesting depth | `3` |
| `TABULA_MAX_CHILDREN_PER_SESSION` | Max subagents per session | `5` |
| `ANTHROPIC_API_KEY` | Claude API key | required for `anthropic` |
| `ANTHROPIC_MODEL` | Claude model | `claude-sonnet-4-6` |
| `ANTHROPIC_BASE_URL` | Claude API endpoint | `https://api.anthropic.com` |
| `OPENAI_API_KEY` | OpenAI API key | required for `openai` |
| `OPENAI_MODEL` | OpenAI model | `gpt-5` |
| `OPENAI_BASE_URL` | OpenAI API endpoint | `https://api.openai.com` |

</details>

## Development

Install from source into `TABULA_HOME`:

```bash
make install
```

That flow installs Python packages from `scripts/requirements-dev.txt`, and the release installer uses `scripts/requirements-runtime.txt` as its runtime dependency source of truth.

## License

MIT
