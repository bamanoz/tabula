# Tabula

Microkernel AI agent. Go kernel connects skills over WebSocket using a JSON pub/sub protocol.

```
┌──────────┐     ┌──────────────┐     ┌──────────────┐
│ CLI      │────▶│              │────▶│ LLM Driver   │
│ Gateway  │◀────│  Go Kernel   │◀────│ (Anthropic)  │
└──────────┘     │              │     └──────────────┘
                 │  WebSocket   │
                 │  Pub/Sub     │     ┌──────────────┐
                 │  Tool Exec   │────▶│ Subagent     │
                 │              │     └──────────────┘
                 │              │
                 │              │     ┌──────────────┐
                 │              │────▶│ Memory       │
                 └──────────────┘     └──────────────┘
```

## Install

```bash
git clone <repo> && cd tabula
./install.sh
```

Requires: Go 1.26+, Python 3.11+

Installs to `~/.tabula/` (override with `TABULA_HOME`):

```
~/.tabula/
├── bin/tabula          # Go binary
├── tabula.yaml         # config (boot command)
├── boot.py             # skill discovery & prompt assembly
├── skills/             # installed skills
├── memory/             # persistent memory
└── .venv/              # Python dependencies
```

## Usage

```bash
export ANTHROPIC_API_KEY=sk-...
tabula
```

Use OpenAI instead:

```bash
export TABULA_PROVIDER=openai
export OPENAI_API_KEY=sk-...
tabula
```

Verbose mode (logs to `~/.tabula/kernel.log`):

```bash
tabula -v
```

## Architecture

The kernel is a Go binary (WebSocket server) that:

1. Runs the boot script (`boot.py`) to discover skills and assemble a system prompt
2. Starts an HTTP/WebSocket server (default `localhost:8089`)
3. Spawns skill processes (LLM driver, CLI gateway)
4. Routes messages between them via session-scoped pub/sub
5. Intercepts and executes tool calls (EXEC, SPAWN, KILL, LIST)
6. Monitors child processes with a reaper goroutine

Skills connect to the kernel via WebSocket, declare what message types they send/receive, and join a session. The kernel handles routing — skills don't know about each other.

### Boot sequence

1. Kernel reads `tabula.yaml` to find the boot command
2. Boot script scans `skills/` for `SKILL.md` files, reads long-term memory, assembles system prompt
3. Boot outputs JSON config: `{url, system_prompt, spawn[]}`
4. Kernel starts WebSocket server, spawns listed processes
5. Processes connect, join sessions, begin message exchange

### Protocol

JSON messages over WebSocket. Each message has a `type` field:

| Message | Direction | Description |
|---------|-----------|-------------|
| `connect` | skill → kernel | Register with name, sends[], receives[] |
| `connected` | kernel → skill | Acknowledge with client ID |
| `join` | skill → kernel | Join a session |
| `joined` | kernel → skill | Acknowledge session join |
| `init` | kernel → skill | System prompt + tools (sent after join) |
| `message` | any → any | Text message (user input, subagent results) |
| `stream_start` | driver → gateway | LLM response begins |
| `stream_delta` | driver → gateway | Token chunk |
| `stream_end` | driver → gateway | LLM response complete |
| `tool_use` | driver → kernel | LLM wants to call a tool |
| `tool_result` | kernel → driver | Tool execution result |
| `done` | driver → gateway | Turn complete |
| `cancel` | gateway → kernel | Abort current operation |
| `error` | kernel → session | Process crash notification |

### Sessions

Sessions isolate message routing. The main conversation uses session `main`. Each subagent joins its own session (`subagent-<id>`). Cross-session messaging is supported by setting the `session` field explicitly in a message.

### Kernel tools

| Tool | Description |
|------|-------------|
| `EXEC` | Run a command synchronously, return stdout (truncated to 16KB) |
| `SPAWN` | Start a background process, return PID |
| `KILL` | Stop a process by PID |
| `LIST` | List all spawned processes with PID, command, alive status |

## Skills

| Skill | Description |
|-------|-------------|
| `lib` | Shared runtime library: `KernelConnection`, `DriverRuntime`, `SubagentRuntime`, provider adapters |
| `llm-anthropic` | Claude API driver with streaming, tool use, and subagent result collection |
| `llm-openai` | OpenAI Responses API driver with the same protocol and multi-agent behavior |
| `gateway-cli` | Interactive terminal UI (Rich markdown, shimmer spinner) |
| `subagent-anthropic` | Autonomous LLM sub-agent spawned for parallel tasks |
| `subagent-openai` | OpenAI-backed autonomous sub-agent for parallel tasks |
| `memory` | Persistent memory — save, search, list, get, delete |

### Subagents

The LLM driver can spawn sub-agents via the `SPAWN` tool. Each subagent is an independent process running its own LLM loop in a separate session.

```
Parent LLM ──SPAWN──▶ Kernel ──fork──▶ Subagent process
     │                                      │
     │◀─── message (result) ───────────────│
```

Key design:
- Subagents only get `EXEC` — no `SPAWN`/`KILL`/`LIST` (prevents recursive spawning)
- Max spawn depth (`TABULA_MAX_SPAWN_DEPTH`, default 3) and max children per session (`TABULA_MAX_CHILDREN_PER_SESSION`, default 5)
- Parent detects subagent IDs via `--id` argument in SPAWN commands
- Results collected with debounce batching (5s) and max wait (300s)
- Multiple subagents run in parallel, results aggregated into one LLM turn
- Spawn failures (exceeding limits) are detected and recorded as failed results
- Subagents can stay alive for follow-up messages with `--timeout`

Spawn command:
```bash
SPAWN python3 skills/subagent-anthropic/run.py \
  --id research_1 \
  --parent-session main \
  --task "Research topic X" \
  --timeout 30
```

### Memory

```bash
# Save (short-term, daily file)
EXEC python3 skills/memory/run.py save --category fact --title "Uses Go" "Kernel written in Go"

# Save (long-term, injected into system prompt)
EXEC python3 skills/memory/run.py save --category fact --title "Uses Go" --long-term "Kernel written in Go"

# Search (semantic + keyword)
EXEC python3 skills/memory/run.py search "what language"

# List, get, delete
EXEC python3 skills/memory/run.py list --category fact
EXEC python3 skills/memory/run.py get <entry-id>
EXEC python3 skills/memory/run.py delete <entry-id>
```

Supports semantic search via OpenAI embeddings when `OPENAI_API_KEY` is set. Falls back to keyword search otherwise.

## Provider Selection

`boot.py` selects the active driver and subagent pair from `TABULA_PROVIDER`.

Supported values:
- `anthropic` (default)
- `claude` → alias for `anthropic`
- `openai`
- `gpt` → alias for `openai`

Selection rules:
- if the requested provider skill exists, it is used;
- if it does not exist but another provider exists locally, `boot.py` falls back to it and prints a warning to stderr;
- if no provider skills exist, boot fails immediately.

The boot script also warns when the expected API key for the chosen provider is missing.

## Configuration

`~/.tabula/tabula.yaml`:

```yaml
boot: .venv/bin/python3 boot.py
```

The boot script handles everything else: skill discovery, system prompt assembly, and process list.

### Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TABULA_HOME` | Workspace directory | `~/.tabula` |
| `TABULA_URL` | Kernel WebSocket URL | `ws://localhost:8089/ws` |
| `TABULA_PROVIDER` | Active LLM provider (`anthropic`, `claude`, `openai`, `gpt`) | `anthropic` |
| `TABULA_VERBOSE` | Enable verbose logging in skill processes (`1` to enable) | unset |
| `TABULA_MAX_SPAWN_DEPTH` | Max nesting depth for SPAWN (prevents recursive spawning) | `3` |
| `TABULA_MAX_CHILDREN_PER_SESSION` | Max active subagent processes per session | `5` |
| `ANTHROPIC_API_KEY` | Claude API key | required for `anthropic` provider |
| `ANTHROPIC_MODEL` | Model name | `claude-sonnet-4-6` |
| `ANTHROPIC_BASE_URL` | API endpoint override | `https://api.anthropic.com` |
| `OPENAI_API_KEY` | OpenAI API key | required for `openai` provider |
| `OPENAI_MODEL` | OpenAI model name | `gpt-5` |
| `OPENAI_BASE_URL` | OpenAI API endpoint override | `https://api.openai.com` |

Set `TABULA_PROVIDER=openai` to spawn `skills/llm-openai/run.py` instead of `skills/llm-anthropic/run.py`.

## Adding skills

Create a directory in `skills/` with:
- `SKILL.md` — documentation (injected into system prompt)
- `run.py` — entry point (or any executable)

The skill connects to the kernel via WebSocket (`TABULA_URL`), sends a `connect` message declaring its message types, joins a session, and starts communicating.

## License

MIT
