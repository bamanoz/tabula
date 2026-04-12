---
name: tabula-guide
description: "Tabula architecture reference. Use `EXEC cat skills/tabula-guide/SKILL.md` to read."
---

# Tabula Architecture Guide

## Overview

Tabula is a multi-agent LLM kernel. Components:

- **Kernel** (Go) — WebSocket hub routing messages between clients
- **Skills** — modular components: drivers, gateways, hooks, tools, subagents. Language-agnostic (any language that speaks WebSocket + JSON).
- **boot.py** — assembles config at startup: system prompt, tools, spawn list

All communication is JSON over WebSocket (`TABULA_URL`, default `ws://localhost:8089/ws`).

## Boot System

`boot.py` outputs JSON config to stdout:

```json
{
  "url": "ws://localhost:8089/ws",
  "system_prompt": "...",
  "spawn": ["python3 skills/cron/run.py daemon", ...],
  "tools": [{"name": "...", "description": "...", "params": {...}, "required": [...], "exec": "..."}],
  "commands": [{"name": "...", "description": "...", "body": "..."}]
}
```

### Provider resolution

`TABULA_PROVIDER` env (default: `anthropic`). Aliases: claude→anthropic, gpt/openclaw→openai.
Falls back to first available driver if requested unavailable.

### System prompt assembly

The system prompt is split into **static** and **dynamic** sections separated by
`<!-- CACHE_BOUNDARY -->` for future prompt caching.

**Static sections** (cacheable):
1. Identity — from `templates/SYSTEM.md`
2. `## Tools` — from `templates/TOOLS.md`
3. `## Guidelines` — from `templates/GUIDELINES.md`
4. `## Safety` — from `templates/SAFETY.md`
5. Project files — IDENTITY.md, SOUL.md, USER.md, AGENTS.md (if present in `~/.tabula/`)

**Dynamic sections** (per-session):
6. `## Available skills` — one-liner per skill with description
7. `## Long-term memory` — from `~/.tabula/memory/MEMORY.md`
8. `## MCP Tools` — discovered MCP server tools
9. `## Environment` — provider, date, working directory

### Templates

Static prompt sections are stored in `templates/` directory:
`SYSTEM.md`, `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md`.

### Project files

User-editable files in `~/.tabula/` injected into the system prompt.
Created automatically on first boot from `templates/` defaults via
`ensure_project_files()` (write-if-missing).

| File | Purpose | Subagent |
|------|---------|----------|
| `IDENTITY.md` | Name, personality, language | No |
| `SOUL.md` | Personality, tone, style | No |
| `USER.md` | User context (name, timezone, preferences) | No |
| `AGENTS.md` | Workspace instructions, behavioral rules | Yes |

Subagents receive a minimal prompt: identity, tools, guidelines, safety,
AGENTS.md only, and environment. No skills, memory, IDENTITY, SOUL, or USER.

### Subagent prompt

`build_subagent_prompt()` generates a minimal prompt for subagents and writes it
to `~/.tabula/.subagent_prompt` at boot time. The subagent runtime reads this file
instead of using the init prompt from the kernel. This keeps the kernel agnostic —
it always sends the same system prompt to all clients.

### Auto-spawn

`build_spawn()` starts: cron daemon (if no OS crontab), MCP pool (if configured),
sessions daemon, hook-logger.

## Kernel

Go WebSocket hub (`internal/kernel/`). Manages clients, sessions, spawned processes.

### Startup sequence

1. Read `tabula.yaml` → boot command
2. Execute boot → parse JSON config
3. Load kernel tools + merge skill tools
4. Start WebSocket server
5. Spawn boot processes

### Connect & Join

```
Client → {"type": "connect", "name": "cli", "sends": [...], "receives": [...], "hooks": [...]}
Kernel → {"type": "connected", "id": "c1"}
Client → {"type": "join", "session": "main"}
Kernel → {"type": "joined", "session": "main"}
Kernel → {"type": "init", "prompt": "...", "tools": [...]}  (if client receives "init")
```

### Message types

| Type | Direction | Purpose |
|------|-----------|---------|
| `connect` | → kernel | Declare client capabilities |
| `connected` | ← kernel | Confirm connection with client ID |
| `join` | → kernel | Join a session |
| `joined` | ← kernel | Confirm session join |
| `init` | ← kernel | System prompt + tools |
| `member_joined` | ← kernel | Notify session members |
| `message` | bidirectional | Chat message (text field) |
| `stream_start` | ← driver | Begin streaming response |
| `stream_delta` | ← driver | Text chunk |
| `stream_end` | ← driver | End streaming |
| `done` | ← driver | Turn complete |
| `tool_use` | ← driver | Request tool execution (name, id, input) |
| `tool_result` | → driver | Tool execution result (id, output) |
| `cancel` | → kernel | Cancel current turn |
| `error` | ← kernel | Error message |
| `hook` | ← kernel | Hook event to subscriber |
| `hook_result` | → kernel | Hook subscriber response |
| `status` | ← kernel | Status update (e.g. compacting) |

## Hook System

Skills subscribe to events via `hooks` field in `connect` message:

```json
{
  "type": "connect",
  "name": "my-hook",
  "sends": ["hook_result"],
  "receives": ["hook"],
  "hooks": [{"event": "before_message", "priority": 10}]
}
```

### Events and strategies

| Event | Strategy | Description |
|-------|----------|-------------|
| `before_message` | modifying | Intercept user messages; can modify text or block |
| `after_message` | void | Fires on `done`; audit logging |
| `before_tool_call` | modifying | Intercept EXEC; can modify command or block |
| `after_tool_call` | void | Fires after EXEC completes |
| `session_start` | modifying | Fires on join; can inject context into init |
| `session_end` | void | Fires on client disconnect |
| `before_spawn` | modifying | Intercept SPAWN; can modify or block |
| `after_spawn` | void | Fires after process spawned |
| `cancel` | void | Fires on cancel broadcast |

### Strategies

- **void**: fire-and-forget to all subscribers. No response needed.
- **modifying**: sequential by priority (highest first). Each subscriber receives
  current payload and responds with:
  - `{"action": "pass"}` — continue unchanged
  - `{"action": "modify", "payload": {...}}` — update payload for next subscriber
  - `{"action": "block", "reason": "..."}` — cancel the event
  Timeout: 5 seconds per subscriber (treated as pass).
- **claiming**: sequential; first `{"action": "claim"}` wins.

### session_start context injection

`session_start` is modifying. A hook can respond with:
```json
{"action": "modify", "payload": {"context": "Extra instructions..."}}
```
The kernel appends `context` to the system prompt in the `init` message for that session.

## Tool System

### Kernel tools (always available)

**EXEC** — run shell command asynchronously.
```json
{"type": "tool_use", "id": "t1", "name": "EXEC", "input": {"command": "ls -la"}}
```
Output capped at 16KB. Empty output → "OK".

**SPAWN** — start background process.
```json
{"type": "tool_use", "id": "t2", "name": "SPAWN", "input": {"command": "python3 skills/timer/run.py -s 60 -m 'done'"}}
```
Returns "PID X". Child receives `TABULA_SPAWN_TOKEN` env for depth tracking.
Checks: depth ≤ maxSpawnDepth (3), alive children < maxChildren (5).

**KILL** — terminate spawned process.
```json
{"type": "tool_use", "id": "t3", "name": "KILL", "input": {"pid": 12345}}
```

**LIST** — list spawned processes in current session.
```json
{"type": "tool_use", "id": "t4", "name": "LIST", "input": {}}
```

### Skill tools

Declared in SKILL.md frontmatter `tools` field. Kernel dispatches by name from
`toolExec` map. The exec command receives JSON input on stdin, writes result to stdout.
Each call is a separate process invocation.

Example: `python3 skills/weather/run.py tool get_weather` receives
`{"location": "Berlin"}` on stdin, outputs weather text on stdout.

## Skills Reference

### Drivers

| Skill | Provider | Default model |
|-------|----------|---------------|
| `driver-anthropic` | Anthropic (Claude) | claude-sonnet-4-6 |
| `driver-openai` | OpenAI | gpt-5 |
| `driver-mock` | Mock (testing) | — |

Drivers receive `message`, `tool_result`, `init`, `cancel`.
Send `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`.

### Gateways

| Skill | Description |
|-------|-------------|
| `gateway-cli` | Interactive terminal with raw mode input, Tab autocomplete, slash commands |
| `gateway-api` | OpenAI-compatible HTTP API (`/v1/chat/completions`, `/v1/responses`) |
| `gateway-test` | Sends hardcoded message after 2s, for automated testing |

### Subagents

| Skill | Provider |
|-------|----------|
| `subagent-anthropic` | Claude |
| `subagent-openai` | OpenAI |
| `subagent-mock` | Mock (testing) |

Usage: `SPAWN python3 skills/subagent-anthropic/run.py --id <id> --parent-session <session> --task "<task>"`
Optional: `--timeout N` (0=oneshot, default). Results delivered as messages to parent session.

### Infrastructure

| Skill | Description |
|-------|-------------|
| `cron` | Scheduled tasks. Uses OS crontab or built-in daemon. |
| `memory` | Persistent memory. Categories: fact, preference, decision, entity, note. `--long-term` injects into system prompt. |
| `sessions` | Cross-session messaging. Messages arrive as `<cross_session>` XML tags. |
| `hook-logger` | JSONL audit log of all hook events to `~/.tabula/logs/hooks.jsonl`. |
| `mcp` | MCP bridge to external servers. Config: `~/.tabula/mcp/servers.json`. |
| `timer` | One-shot delayed message. No LLM, direct WebSocket. |

### User tools

| Skill | user-invocable | Tools |
|-------|----------------|-------|
| `weather` | yes | `get_weather(location, format?)` |
| `apple-reminders` | no | — (via EXEC remindctl) |
| `clawhub` | no | — (via EXEC clawhub CLI) |

### Library

`skills/lib/` — shared Python modules:
- `kernel_client.KernelConnection` — thread-safe WebSocket wrapper
- `driver_runtime.DriverRuntime` — driver orchestration (streaming, tool calls, subagent collection)
- `subagent_runtime.SubagentRuntime` — subagent orchestration
- `providers.py` — LLM adapters: `AnthropicSession`, `OpenAISession`, `MockProvider`

## SKILL.md Format

```yaml
---
name: skill-name              # defaults to directory name
description: "Short text"     # shown in system prompt; no description = hidden
user-invocable: true          # expose as /name slash command (explicit only)
tools:                         # tool definitions array (JSON)
  [{"name": "...", "description": "...",
    "params": {"arg": {"type": "string", "description": "..."}},
    "required": ["arg"]}]
---

# Skill documentation body (markdown)
```

## Slash Commands

Skills with `user-invocable: true` become `/name` commands in gateway-cli.

`/weather chicago` → gateway reads SKILL.md body, sends as message:
`"{body}\n\nUser request: chicago"`. LLM receives instructions + request.

Builtin gateway commands: `/help` (list commands), `/exit` (quit).
Tab autocomplete: prefix match on all commands.

Discovery: `boot.py` `discover_slash_commands()` scans for `user-invocable: true`.

## Wire Protocol Examples

### Driver

```json
← {"type": "init", "prompt": "You are Tabula...", "tools": [...]}
← {"type": "message", "text": "Hello"}
→ {"type": "stream_start"}
→ {"type": "stream_delta", "text": "Hi "}
→ {"type": "stream_delta", "text": "there!"}
→ {"type": "stream_end"}
→ {"type": "done"}
```

### Gateway

```json
→ {"type": "message", "text": "What's the weather?"}
← {"type": "stream_start"}
← {"type": "stream_delta", "text": "Let me check..."}
← {"type": "stream_end"}
← {"type": "done"}
```

### Hook subscriber

```json
← {"type": "hook", "id": "h-abc123", "name": "before_message", "payload": {"text": "hello", "sender": "cli"}}
→ {"type": "hook_result", "id": "h-abc123", "action": "pass"}
```

## Limits

| Parameter | Default | Description |
|-----------|---------|-------------|
| maxSpawnDepth | 3 | Max nesting of SPAWN calls |
| maxChildren | 5 | Max alive subprocesses per session |
| MaxClients | 100 | Max WebSocket connections |
| hookTimeout | 5s | Max wait per hook subscriber |
| maxExecOutput | 16KB | Max bytes from EXEC/tool output |
| ShutdownTimeout | 3s | Grace period before SIGKILL on shutdown |
| spawnTokenTTL | 60s | Spawn token expiration |

## Creating a New Skill

1. `mkdir skills/<name>`
2. Write `SKILL.md` with frontmatter (`name`, `description`)
3. Write `run.py` entry point
4. If tool-skill: add `tools` to frontmatter, implement `run.py tool <name>` subcommand
5. If hook-skill: subscribe via `hooks` in `connect` message, handle `hook` messages
6. If daemon: add to `build_spawn()` in `boot.py`
7. If slash command: add `user-invocable: true` to frontmatter
