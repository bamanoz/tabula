# Tabula

Microkernel AI agent. Zig kernel connects skills over Unix sockets using a JSON Lines protocol.

```
┌──────────┐     ┌──────────────┐     ┌─────────────┐
│ CLI      │────▶│              │────▶│ LLM Driver  │
│ Gateway  │◀────│  Zig Kernel  │◀────│ (Anthropic)  │
└──────────┘     │              │     └─────────────┘
                 │  Unix Socket │
                 │  Pub/Sub     │     ┌─────────────┐
                 │  Tool Exec   │────▶│ Memory      │
                 └──────────────┘     └─────────────┘
```

## Install

```bash
git clone <repo> && cd tabula
./install.sh
```

Requires: Zig 0.14+, Python 3.11+

Installs to `~/.tabula/` (override with `TABULA_HOME`):

```
~/.tabula/
├── bin/tabula          # binary
├── tabula.yaml         # config
├── skills/             # installed skills
├── memory/             # persistent memory
└── .venv/              # Python dependencies
```

## Usage

```bash
export ANTHROPIC_API_KEY=sk-...
tabula
```

Verbose mode:

```bash
tabula -v
```

## Architecture

The kernel is a single-threaded Zig binary that:

1. Opens a Unix domain socket
2. Spawns skill processes (LLM driver, CLI gateway, etc.)
3. Routes messages between them via pub/sub
4. Intercepts and executes tool calls (EXEC, SPAWN, KILL, LIST)

Skills connect to the kernel socket, declare what message types they send/receive, and join a session. The kernel handles routing — skills don't know about each other.

### Protocol

JSON Lines over Unix socket. Each message has a `type` field:

| Message | Direction | Description |
|---------|-----------|-------------|
| `connect` | skill → kernel | Register with name, sends[], receives[] |
| `join` | skill → kernel | Join a session |
| `message` | gateway → driver | User input |
| `stream_start` | driver → gateway | LLM response begins |
| `stream_delta` | driver → gateway | Token chunk |
| `stream_end` | driver → gateway | LLM response complete |
| `tool_use` | driver → kernel | LLM wants to call a tool |
| `tool_result` | kernel → driver | Tool execution result |
| `done` | driver → gateway | Turn complete |

### Kernel Tools

| Tool | Description |
|------|-------------|
| `EXEC` | Run a command synchronously, return stdout |
| `SPAWN` | Start a background process, return PID |
| `KILL` | Stop a process by PID |
| `LIST` | List spawned processes |

## Skills

| Skill | Description |
|-------|-------------|
| `llm-anthropic` | Claude API driver with streaming and tool use |
| `gateway-cli` | Interactive terminal UI (Rich markdown, shimmer spinner) |
| `memory` | Persistent memory — save, search, list, get, delete |

### Memory

```bash
# Save
EXEC python3 skills/memory/run.py save --category fact --title "Uses Zig" "Kernel written in Zig"

# Search
EXEC python3 skills/memory/run.py search "what language"

# List
EXEC python3 skills/memory/run.py list --category fact
```

Long-term memories (`--long-term`) are injected into the system prompt on startup. Supports semantic search via OpenAI embeddings when `OPENAI_API_KEY` is set.

## Configuration

`~/.tabula/tabula.yaml`:

```yaml
socket: /tmp/tabula.sock
system_prompt: .venv/bin/python3 system_prompt.py skills/
spawn:
  - .venv/bin/python3 skills/llm-anthropic/run.py
  - .venv/bin/python3 skills/gateway-cli/run.py
```

### Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TABULA_HOME` | Workspace directory | `~/.tabula` |
| `TABULA_SOCKET` | Kernel socket path | from config |
| `ANTHROPIC_API_KEY` | Claude API key | required |
| `ANTHROPIC_MODEL` | Model name | `claude-sonnet-4-6` |
| `OPENAI_API_KEY` | For memory embeddings | optional |

## License

MIT
