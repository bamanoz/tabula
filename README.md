# Tabula

Tabula is a compact agent kernel: a small Go core that orchestrates Python skills over WebSocket.
Drivers talk to LLM APIs, gateways talk to people, tool skills do work, and subagents are real child
processes with their own session scope.

It is closer to a local agent platform than a single chatbot app.

- The kernel owns routing, sessions, process lifecycle, and built-in kernel tools.
- Skills own integrations and domain logic.
- Boot scripts assemble a distro-specific runtime from `SKILL.md`, templates, and config.
- Distros let the same kernel power different products.

Tabula is designed to stay inspectable and hackable: small core, explicit process boundaries, simple wire
protocol, and plain files under `~/.tabula/`.

## Status

Tabula is already useful as a local agent runtime, but it is still in the "strong core, maturing ops shell"
phase.

- Strong today: compact kernel, process-based skills, distro model, official Anthropic/OpenAI SDK drivers,
  OpenAI-compatible HTTP gateway, real subagent process isolation, local memory via MemPalace.
- Still maturing: operator tooling, subagent control/registry, long-running gateway lifecycle hardening,
  and the general "product shell" around the kernel.

If you want a local-first, inspectable agent system with clear seams, this is what Tabula is for.

## Install

### Release install

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Release install:

- downloads the Go binary and runtime assets from GitHub Releases
- creates `~/.tabula/.venv`
- installs Python runtime dependencies from `scripts/requirements-runtime.txt`
- installs the default `assistant` distro
- attempts to install a user service on macOS/Linux

Requires Python 3.11+.

Install a specific version:

```bash
VERSION=v1.0.0 curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Switch distro later:

```bash
tabula-install-distro <local-path-or-github-tree-url>
```

<details>
<summary>Windows</summary>

```powershell
irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1 | iex
```

Windows support exists, but the main development/test flow today is macOS/Linux-first.
</details>

### Install from source

```bash
git clone https://github.com/bamanoz/tabula.git
cd tabula
bash scripts/install-dev.sh
```

Install a different distro from source:

```bash
bash scripts/install-dev.sh --distro guardian
```

Requires Go 1.26+ and Python 3.11+.

Source install:

- builds `bin/tabula`
- creates `TABULA_HOME/.venv`
- installs Python dev dependencies from `scripts/requirements-dev.txt`
- copies shared skill runtime code into `TABULA_HOME/skills/lib`
- installs the selected distro under `TABULA_HOME/distrib/<name>`
- refreshes the flat runtime surface (`boot.py`, `templates/`, `skills/`)
- installs launchers like `tabula-server`, `tabula-cli`, `tabula-api`, `tabula-install-distro`

## Quick start

The simplest path is `.env`-based config:

```bash
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> ~/.tabula/.env
tabula-server
tabula-cli
```

Use OpenAI instead:

```bash
cat >> ~/.tabula/.env <<'EOF'
TABULA_PROVIDER=openai
OPENAI_API_KEY=sk-...
EOF

tabula-server
tabula-cli
```

Notes:

- Default provider is `anthropic` unless overridden by `TABULA_PROVIDER` or `config/global.toml`.
- If you installed from a release and the user service is already running, `tabula-cli` may be enough.
- `tabula-cli` and `tabula-api` connect to a running kernel; `tabula-server` starts one.

## Mental model

Typical flow:

```text
user -> gateway -> kernel -> driver -> tools / hooks / subagents
```

Pieces:

- Kernel: small Go server that owns sessions, routing, spawned processes, hooks, and kernel tools.
- Boot: Python script that emits JSON config describing the runtime to start.
- Drivers: provider-specific LLM loops.
- Gateways: CLI, HTTP API, Telegram.
- Skills: process-based capabilities discovered from the active distro.
- Subagents: separate child processes running their own driver loop in their own session.

Typical installed layout:

```text
~/.tabula/
├── bin/
├── distrib/
│   ├── assistant/
│   ├── guardian/
│   └── active -> assistant
├── boot.py -> distrib/active/boot.py
├── boot-cicd.py
├── templates/ -> distrib/active/templates
├── skills/
│   ├── lib/
│   └── ... -> distrib/active/skills/*
├── bundles/
├── config/global.toml
├── secrets.json
├── .env
├── data/
├── logs/
└── .venv/
```

The important part is the flat runtime surface: the active distro fans out into `boot.py`, `templates/`, and
`skills/`, while shared runtime code stays in `skills/lib/`.

## Distros

Tabula ships with more than one runtime surface.

### `assistant`

The default general-purpose distro.

It includes:

- provider drivers: Anthropic and OpenAI
- gateways: CLI, OpenAI-compatible HTTP API, Telegram
- tool and support skills: files, sessions, pair, MCP, timer, cron, observer
- hooks: logger and permissions
- provider-matched subagents
- bundle-backed memory tools: `memory-save`, `memory-search`, `memory-admin`

The assistant boot script scans `skills/` recursively, reads `SKILL.md`, follows symlinks into bundles,
assembles the system prompt, discovers tools, and selects exactly one active driver and one matching
subagent runtime based on `TABULA_PROVIDER`.

### `guardian`

A much narrower distro focused on sandboxed Python execution.

- exposes a single `execute_code` tool
- ships a minimal CLI gateway
- builds a Docker sandbox image during install when Docker is available

Guardian is not a drop-in replacement for the assistant distro. It is a focused runtime for controlled code
execution inside a dedicated sandbox.

## What the assistant distro can do

### Providers

- Anthropic via the official `anthropic` SDK
- OpenAI via the official `openai` SDK
- provider selection via `.env`, `config/global.toml`, or explicit overrides in some gateways

### Gateways

- `gateway-cli`: local terminal chat UI
- `gateway-api`: OpenAI-compatible `/v1/chat/completions` and `/v1/responses`
- `gateway-telegram`: Telegram bot gateway backed by `python-telegram-bot`

### Tools and support skills

- `files`: `read_file`, `write_file`, `str_replace`
- `mcp`: bridge to external Model Context Protocol servers
- `sessions`: cross-session send/list/history
- `pair`: approve and revoke gateway access
- `timer` and `cron`: delayed and scheduled work
- `observer`: HTTP metrics over hook events
- `hook-logger` and `hook-permissions`: audit and policy

### Memory and bundles

- `memory-save`, `memory-search`, `memory-admin` are provided by the `bundles/memory` bundle
- memory is backed by MemPalace and stored locally under `data/memory/palace/`
- the repo also includes the optional `caveman` bundle as an example/reference bundle

### Subagents

Subagents are not fake threads inside one runtime. They are separate spawned processes with their own
provider loop and their own session scope.

This is one of Tabula's strongest architectural properties.

It also means subagent control and accounting matter a lot, and that part of the system is still simpler than
the core architecture deserves. Today subagents work and run in parallel, but their operational control plane
is still fairly minimal.

## Common commands

| Command | What it does |
| --- | --- |
| `tabula-server` | Start the kernel with sane defaults (`TABULA_BOOT`, `TABULA_PATH`) |
| `tabula-cli` | Connect the local CLI gateway to a running kernel |
| `TABULA_API_PORT=8090 tabula-api` | Start the OpenAI-compatible HTTP gateway |
| `tabula-install-distro <path-or-github-tree-url>` | Install or switch the active distro |
| `tabula serve` | Low-level kernel server entrypoint |
| `tabula run --prompt "..."` | One-shot prompt -> response mode |

Direct `tabula serve` and `tabula run` need `TABULA_BOOT` if you are not using the wrapper scripts:

```bash
TABULA_BOOT='"$HOME/.tabula/.venv/bin/python3" "$HOME/.tabula/boot.py"' tabula serve
```

For CI-style minimal runs, use the installed `boot-cicd.py`:

```bash
TABULA_BOOT='"$HOME/.tabula/.venv/bin/python3" "$HOME/.tabula/boot-cicd.py"' \
  tabula run --prompt "summarize this file"
```

`boot-cicd.py` is intentionally small: driver only, minimal runtime surface, no full assistant shell.

## API gateway

Start a kernel, then start the API gateway:

```bash
tabula-server
TABULA_API_PORT=8090 tabula-api
```

Example request:

```bash
curl http://localhost:8090/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"tabula","messages":[{"role":"user","content":"hello"}]}'
```

The gateway is OpenAI-compatible on purpose: existing OpenAI SDK clients can talk to Tabula without needing a
custom client.

## Architecture

Boot sequence in the assistant distro:

1. Kernel runs the command from `TABULA_BOOT`.
2. `boot.py` loads `.env`, scans `skills/`, parses `SKILL.md`, and assembles prompt/tools/commands/spawn list.
3. Boot prints a single JSON config object.
4. Kernel starts the WebSocket server and spawns configured skill processes.
5. Skills connect, declare message types, join sessions, and start exchanging messages.

Important properties:

- Skills do not call each other directly. The kernel routes messages between them.
- Session scope is the basic isolation boundary for conversations.
- Built-in kernel tools are separate from skill tools.
- The assistant distro is metadata-driven; skill frontmatter affects prompt injection, tool exposure,
  slash commands, and compatibility with kernel tools.

Built-in kernel tools currently include:

- `shell_exec`
- `process_spawn`
- `process_kill`
- `process_list`

See `distrib/assistant/boot.py`, `skills/lib/protocol.py`, and `skills/skill-contract/SKILL.md` for the
actual runtime contract.

## Configuration model

Tabula uses three layers:

- `.env`: easiest place for local overrides and API keys
- `config/global.toml`: structured defaults for providers, gateways, sessions, MCP, and other runtime settings
- `secrets.json`: secret store entries referenced from config

In practice, many installs start with just `.env`:

```bash
# ~/.tabula/.env
TABULA_PROVIDER=anthropic
ANTHROPIC_API_KEY=sk-ant-...
```

Structured config example:

```toml
provider = "openai"

[openai]
model = "gpt-5.4"
base_url = "https://api.openai.com/v1"
api_key = { source = "store", id = "driver-openai.api_key" }
```

Some useful runtime variables:

| Variable | Description |
| --- | --- |
| `TABULA_HOME` | Tabula home directory, default `~/.tabula` |
| `TABULA_PROVIDER` | Active provider, usually `anthropic` or `openai` |
| `TABULA_BOOT` | Boot command used by `tabula serve` / `tabula run` |
| `TABULA_URL` | Kernel WebSocket URL used by skills |
| `TABULA_API_PORT` | HTTP port for `tabula-api` |
| `TABULA_MAX_SPAWN_DEPTH` | Max nested subagent depth |
| `TABULA_MAX_CHILDREN_PER_SESSION` | Max child subagents per session |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | Provider API keys |

## Adding skills

The main extension seam is still simple:

1. create a directory in `skills/`
2. add a `SKILL.md` with frontmatter and human-readable docs
3. add a `run.py` entrypoint
4. connect to the kernel via WebSocket and declare what the skill sends/receives

If a skill exposes tools, declare them in `SKILL.md` frontmatter. The boot layer discovers them and hands them
to the active driver.

See `skills/skill-contract/SKILL.md` for the current contract.

## Testing

The verification matrix is intentionally split so you can run the right layer for the change:

```bash
make test-unit
make test-smoke
make test-e2e
make test-contract
```

Current layers:

- `unit`: fast logic-only tests
- `smoke`: minimal runtime boot/connect/init path
- `e2e`: heavier runtime flows like hooks, MCP, observer, mock driver, and subagents
- `contract`: protocol and extension contract checks under `skills/`
- `manual`: real-env and diagnostic helpers run directly

See [`tests/README.md`](tests/README.md) for the current matrix.

## Development

Useful commands:

```bash
make build
make install
make test-unit
make test-smoke
make test-e2e
make test-contract
```

Notes:

- `make install` runs `scripts/install-dev.sh`
- runtime Python dependencies live in `scripts/requirements-runtime.txt`
- source installs use `scripts/requirements-dev.txt`

## License

MIT
