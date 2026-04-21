---
name: tabula-guide
description: "Tabula architecture reference. Use `shell_exec cat skills/tabula-guide/SKILL.md` to read."
---

# Tabula Architecture Guide

## Overview

Tabula is a multi-agent LLM kernel. Components:

- **Kernel** (Go) — WebSocket hub routing messages between clients
- **Skills** — modular components: drivers, gateways, hooks, tools, subagents. Language-agnostic (any language that speaks WebSocket + JSON).
- **boot.py** — assembles config at startup: system prompt, tools, spawn list

All communication is JSON over WebSocket (`TABULA_URL`, default `ws://localhost:8089/ws`).

## Configuration Model

Tabula now distinguishes between **bootstrap config** and **skill config**.

### Bootstrap config

These are still runtime env vars because they must exist before boot and before
the kernel spawns child processes:

- `TABULA_HOME`
- `TABULA_BOOT`
- `TABULA_URL`
- `TABULA_PROVIDER`
- log/path env like `TABULA_LOG_FILE`, `TABULA_PATH`

`boot.py` and `tabula serve/run` still load `~/.tabula/.env` for bootstrap and
backcompat. That file is no longer the source of truth for migrated built-in
skills, but it still participates in early process setup.

### Skill config

Migrated built-in skills declare their config contract in `SKILL.config.json`
and load resolved values through `skills/lib/config.py`.

For core built-in skills, non-secret config now lives centrally in:

- `~/.tabula/config/global.toml`

Secrets still live in:

- `~/.tabula/secrets.json`

Typical precedence for migrated built-in skills:

1. canonical env (`TABULA_SKILL_*`, then legacy aliases)
2. `~/.tabula/config/global.toml`
3. `~/.tabula/secrets.json` for secret fields
4. schema defaults

Third-party skills from ClawHub may use a different model and are free to
define their own configuration story.

### Canonical env naming

Canonical skill env names use this pattern:

`TABULA_SKILL_<SKILL_ID_UPPER_UNDERSCORE>_<KEY_PATH_UPPER_UNDERSCORE>`

Examples:

- `TABULA_SKILL_DRIVER_OPENAI_MODEL`
- `TABULA_SKILL_GATEWAY_TELEGRAM_SESSION_IDLE_TTL`
- `TABULA_SKILL_MCP_POOL_URL`

Legacy aliases remain supported where needed (`OPENAI_API_KEY`,
`TELEGRAM_BOT_TOKENS`, etc.).

### Secret storage

Migrated skills can resolve secrets from:

- canonical env / env aliases
- `~/.tabula/secrets.json`
- structured secret refs declared in skill config (`store`, `env`, `file`)

Some skills intentionally share secrets. Example: `subagent-openai` falls back
to `driver-openai.api_key` if its own secret is not present.

### TABULA_HOME layout

Skill-owned files now follow a consistent layout under `TABULA_HOME`:

- `config/global.toml` — centralized config for core built-in skills
- `config/skills/` — extra structured config and third-party skill config
- `data/` — durable mutable records owned by skills
- `state/` — rebuildable indexes/caches
- `run/` — pid files and runtime-discovered endpoints
- `logs/` — skill-owned logs
- `secrets.json` — shared secret store for migrated built-ins

Examples:

- `~/.tabula/config/global.toml`
- `~/.tabula/data/sessions/<session>/history.jsonl`
- `~/.tabula/data/memory/palace/` (MemPalace ChromaDB + SQLite)
- `~/.tabula/run/mcp/pool.url`
- `~/.tabula/logs/hook-logger/hooks.jsonl`

### Boot-managed files

`boot.py` now also follows the structured `TABULA_HOME` layout for the files it
owns directly:

- `~/.tabula/config/skills/hook-permissions/permissions.json`
- `~/.tabula/config/skills/mcp/servers.json`
- `~/.tabula/state/subagent/prompt.txt`

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

`skills/lib/provider_selection.py` resolves the active provider.

Rules:

1. explicit provider override if supplied (`--provider`, gateway override, etc.)
2. `TABULA_PROVIDER` env if set
3. `~/.tabula/config/global.toml` (`provider = "..."`)

There is no silent fallback to another provider. If the selected provider is not
installed or not configured, startup fails with a clear error.

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
7. `## MCP Tools` — discovered MCP server tools
8. `## Environment` — provider, date, working directory

### Templates

At runtime, static prompt sections are read from the flat `templates/` surface:
`SYSTEM.md`, `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md`.

In the repository, the main distro keeps those source files under
`distrib/main/templates/` and install/dev flows expose them flat in
`TABULA_HOME/templates/`.

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
to `~/.tabula/state/subagent/prompt.txt` at boot time. The subagent runtime reads this file
instead of using the init prompt from the kernel. This keeps the kernel agnostic —
it always sends the same system prompt to all clients.

### Auto-spawn

`build_spawn()` starts: cron daemon (if no OS crontab), MCP pool (if configured),
sessions daemon, and hook skills.

### Skill discovery

`walk_skills()` scans `skills/` recursively and returns `(rel_path, SKILL.md_path)`
tuples. Provider filtering via
`include_skill()` excludes non-active `driver-*` and `subagent-*` skills.

## Kernel

Go WebSocket hub (`internal/kernel/`). Manages clients, sessions, spawned processes.

### Startup sequence

1. Resolve `TABULA_HOME`, load bootstrap env, read `TABULA_BOOT`
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
| `before_tool_call` | modifying | Intercept any tool call; can modify or block |
| `after_tool_call` | void | Fires after any tool call completes |
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

### Kernel tools (boot-controlled)

Built-in kernel tools are exposed by distro boot policy. The examples below show
the default main-distro set.

**shell_exec** — run shell command asynchronously.
```json
{"type": "tool_use", "id": "t1", "name": "shell_exec", "input": {"command": "ls -la"}}
```
Output capped at 16KB. Empty output → "OK".

**process_spawn** — start background process.
```json
{"type": "tool_use", "id": "t2", "name": "process_spawn", "input": {"command": "python3 skills/timer/run.py -s 60 -m 'done'"}}
```
Returns "PID X". Child receives `TABULA_SPAWN_TOKEN` env for depth tracking.
Checks: depth ≤ maxSpawnDepth (3), alive children < maxChildren (5).

**process_kill** — terminate spawned process.
```json
{"type": "tool_use", "id": "t3", "name": "process_kill", "input": {"pid": 12345}}
```

**process_list** — list spawned processes in current session.
```json
{"type": "tool_use", "id": "t4", "name": "process_list", "input": {}}
```

### Skill tools

Declared in SKILL.md frontmatter `tools` field. Kernel dispatches by name from
`toolExec` map. The exec command receives JSON input on stdin, writes result to stdout.
Each call is a separate process invocation.

For the full skill format specification, tool definitions, slash commands, and
wire protocol examples, see `skills/skill-contract/SKILL.md`.

## Skills Reference

### Drivers

| Skill | Provider | Default model |
|-------|----------|---------------|
| `driver-anthropic` | Anthropic (Claude) | claude-sonnet-4-6 |
| `driver-openai` | OpenAI | gpt-5.4 |
| `driver-mock` | Mock (testing) | — |

Drivers receive `message`, `tool_result`, `init`, `cancel`.
Send `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`.

### Gateways

| Skill | Description |
|-------|-------------|
| `gateway-cli` | Interactive terminal with raw mode input, Tab autocomplete, slash commands |
| `gateway-api` | OpenAI-compatible HTTP API (`/v1/chat/completions`, `/v1/responses`) |
| `gateway-telegram` | Telegram bot gateway with pairing via `/pair` |
| `gateway-test` | Sends hardcoded message after 2s, for automated testing |

### Subagents

| Skill | Provider |
|-------|----------|
| `subagent-anthropic` | Claude |
| `subagent-openai` | OpenAI |
| `subagent-mock` | Mock (testing) |

Usage: `process_spawn python3 skills/subagent-anthropic/run.py --id <id> --parent-session <session> --task "<task>"`
Optional: `--timeout N` (0=oneshot, default). Results delivered as messages to parent session.

### Infrastructure

| Skill | Description |
|-------|-------------|
| `cron` | Scheduled tasks. Uses OS crontab or built-in daemon. |
| `files` | Read, search, and edit files. Tools: `read`, `list_dir`, `glob`, `grep`, `write`, `edit`, `multiedit`, `apply_patch`. |
| `memory` | Persistent memory via MemPalace (local ChromaDB + SQLite KG). Wing/room/drawer model. No API keys. |
| `pair` | Universal pairing for gateways (Telegram, etc.). |
| `sessions` | Cross-session messaging. Messages arrive as `<cross_session>` XML tags. |
| `hook-logger` | JSONL audit log of all hook events to `~/.tabula/logs/hook-logger/hooks.jsonl`. |
| `mcp` | MCP bridge to external servers. Pool config lives in `~/.tabula/config/global.toml`; server definitions in `~/.tabula/config/skills/mcp/servers.json`. |
| `timer` | One-shot delayed message. No LLM, direct WebSocket. |

### User tools

| Skill | user-invocable | Tools |
|-------|----------------|-------|
| `weather` | yes | `get_weather(location, format?)` |
| `apple-reminders` | no | — (via shell_exec remindctl) |
| `clawhub` | no | — (via shell_exec clawhub CLI) |

### Library

`skills/lib/` — shared Python modules:
- `kernel_client.KernelConnection` — thread-safe WebSocket wrapper
- `driver_runtime.DriverRuntime` — driver orchestration (streaming, tool calls, subagent collection)
- `subagent_runtime.SubagentRuntime` — subagent orchestration
- `providers.py` — LLM adapters: `AnthropicSession`, `OpenAISession`, `MockProvider`

## SKILL.md Format

See `skills/skill-contract/SKILL.md` for the full specification: frontmatter fields,
tool definitions, slash commands, wire protocol, and skill creation guide.

## Slash Commands

Slash commands are user-invocable skills triggered by `/command` in the gateway.
Declared in SKILL.md frontmatter `commands` field:

```yaml
commands:
  - name: my-command
    description: "What this command does"
    body: "Instructions for the LLM when triggered"
```

Commands are discovered by `discover_slash_commands()` during boot and included
in the system prompt. When a user types `/command args`, the gateway sends a
message with the command body + args to the driver.

## Permissions

Tool permission rules defined in `~/.tabula/config/skills/hook-permissions/permissions.json`:

```json
{
  "rules": [
    {"tool": "shell_exec", "command": "rm -rf *", "effect": "deny"},
    {"tool": "shell_exec", "command": "git push *--force*", "effect": "deny"},
    {"tool": "write", "effect": "deny"},
    {"tool": "*", "effect": "allow"}
  ]
}
```

### Rule fields

- `tool` — tool name or glob pattern (`shell_exec`, `write*`, `*`)
- `command` — glob pattern for `shell_exec`/`process_spawn` command field (optional)
- `effect` — `allow` or `deny`

### Evaluation

**Specificity wins**: command-level rules override tool-level rules.
Within the same specificity, deny overrides allow.
No rules file or empty rules = allow all.

Example shell_exec allowlist — allow only specific commands:
```json
{
  "rules": [
    {"tool": "shell_exec", "command": "git *", "effect": "allow"},
    {"tool": "shell_exec", "command": "go *", "effect": "allow"},
    {"tool": "shell_exec", "effect": "deny"},
    {"tool": "*", "effect": "allow"}
  ]
}
```

### Two-layer enforcement

1. **Boot filtering**: `filter_denied_tools()` removes unconditionally denied tools
   from the init message — the LLM never sees them.
2. **Runtime hook**: `hook-permissions` skill subscribes to `before_tool_call` with
   priority 100. Evaluates rules at runtime, blocks denied calls. Only spawned when
   `config/skills/hook-permissions/permissions.json` exists.

## Wire Protocol

All communication is JSON over WebSocket. Message types are defined in
`skills/lib/protocol.py` (Python) and `internal/kernel/protocol.go` (Go).

### Protocol version

The kernel expects `version: 1` in the connect message. Legacy clients sending
`version: 0` (or omitting it) are accepted for backwards compatibility.

### Client → Kernel

| Message | Required fields | Purpose |
|---------|----------------|---------|
| `connect` | `name`, `sends`, `receives` | Register client with capabilities |
| `join` | `session` | Join a session |
| `message` | `text` | Chat message |
| `tool_use` | `id`, `name`, `input` | Request tool execution |
| `hook_result` | `id`, `action` | Response to hook event |
| `status` | `text` | Status update |
| `cancel` | — | Cancel current turn |

### Kernel → Client

| Message | Purpose |
|---------|---------|
| `connected` | Confirm connection with client ID |
| `joined` | Confirm session join |
| `init` | System prompt + tools for LLM |
| `member_joined` | Notify session members |
| `message` | Chat message from another client |
| `tool_result` | Tool execution result |
| `hook` | Hook event to subscriber |
| `error` | Error message |
| `stream_start`, `stream_delta`, `stream_end` | Streaming response chunks |
| `done` | Turn complete |
| `status` | Status update (e.g. compacting) |
| `cancel` | Cancel signal |

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

See `skills/skill-contract/SKILL.md` for step-by-step instructions.
