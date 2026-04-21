---
name: skill-contract
description: "Skill format spec. Use `shell_exec cat skills/skill-contract/SKILL.md` to read. To discover skills: `shell_exec ls skills/`"
---
# Tabula Skill Format

This document defines how skills work in Tabula. Read it to understand how to
discover, use, and create skills.

## What is a skill?

A runtime skill is a directory under `./skills/` containing at least:

- `SKILL.md` — describes what the skill does and how to use it.
- An executable entry point (e.g., `run.py`).

Skills communicate with the kernel via **WebSocket**, not stdin/stdout.

## SKILL.md format

Every skill directory must have a `SKILL.md` with optional YAML frontmatter:

```
---
name: skill-name
description: "Short description shown in system prompt"
user-invocable: true
tools:
  [{"name": "tool_name", "description": "What the tool does",
    "params": {"arg": {"type": "string", "description": "Argument"}},
    "required": ["arg"]}]
---

# Skill Name

Full documentation body (not injected into system prompt).

## Usage

How to run this skill.
```

### Frontmatter fields

- `name` — skill identifier (defaults to directory name)
- `description` — short description injected into system prompt as a one-liner.
  Skills without description are hidden from the system prompt.
- `user-invocable: true` — exposes skill as a `/name` slash command in gateway CLI.
  Only explicit `true` counts; absent = not invocable.
- `tools` — JSON array of tool definitions (see Tool-Skills below)

## Discovering skills

To see available skills:

    shell_exec ls skills/

To read a skill's documentation:

    shell_exec cat skills/<name>/SKILL.md

## Running a skill

Skills are started via process_spawn and connect to the kernel WebSocket:

    process_spawn python3 skills/<name>/run.py [args]

The skill connects to `TABULA_URL` (env var), sends a `connect` message
declaring its message types, then joins a session via `join`.

### Skill lifecycle

1. **Connect**: skill opens WebSocket, sends `connect` with `name`, `sends`,
   `receives`. Kernel responds with `connected`.
2. **Join**: skill sends `join` with `session` name. Kernel responds with
   `joined` and sends `init` (system prompt + tools) if applicable.
3. **Message loop**: skill sends/receives JSON messages via WebSocket.
4. **Exit**: skill closes the WebSocket connection.

### Wire protocol example

```json
{"type": "connect", "name": "my-skill", "sends": ["done"], "receives": ["message"]}
{"type": "connected", "id": "c1"}
{"type": "join", "session": "main"}
{"type": "joined", "session": "main"}
{"type": "message", "text": "hello from skill"}
```

Spawned skills can include a `token` field in the `connect` message to inherit
their parent's spawn depth (received via `TABULA_SPAWN_TOKEN` env var).

When a client joins a session, the kernel broadcasts `member_joined` to other
session members: `{"type": "member_joined", "name": "my-skill", "session": "main"}`.

## Skill categories

### Drivers (LLM backends)

Connect to LLM APIs, handle streaming and tool use.
Receives: `message`, `tool_result`, `init`, `cancel`.
Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`.

### Gateways (user interfaces)

Relay messages between users and the kernel.
Sends: `message`. Receives: `stream_start`, `stream_delta`, `stream_end`, `done`.

### Hook skills

Subscribe to kernel events. Declared via `hooks` field in `connect` message:

```json
{
  "type": "connect",
  "name": "my-hook",
  "sends": ["hook_result"],
  "receives": ["hook"],
  "hooks": [
    {"event": "before_message", "priority": 10},
    {"event": "after_message", "priority": 0}
  ]
}
```

Hook events: `before_message`, `after_message`, `before_tool_call`,
`after_tool_call`, `session_start`, `session_end`, `before_spawn`, `after_spawn`,
`cancel`.

`before_tool_call` fires for **all** tools (kernel tools and skill tools).

The kernel sends a `hook` message to subscribers:
```json
{"type": "hook", "id": "h-abc123", "name": "before_message", "payload": {"text": "hello", "sender": "cli"}}
```

Subscribers respond with a `hook_result`:
```json
{"type": "hook_result", "id": "h-abc123", "action": "pass"}
```

Strategies:
- **void**: fire-and-forget (after_*, session_end, cancel). No response needed.
- **modifying**: sequential by priority (before_*, session_start). Can pass,
  modify, or block. `session_start` hooks can inject context into the init
  message via `{"context": "extra text"}` in the modify payload.
- **claiming**: sequential by priority; first `{"action": "claim"}` wins,
  remaining subscribers are skipped.

### Tool-skills

Skills that provide real LLM tools (tool_use/tool_result cycle). Declared
via `tools` field in SKILL.md frontmatter:

```
---
name: weather
description: "Get weather"
tools:
  [{"name": "get_weather", "description": "Get weather for a city",
    "params": {"location": {"type": "string", "description": "City"}},
    "required": ["location"]}]
---
```

The skill must have a `run.py` with a `tool` subcommand:

    python3 skills/weather/run.py tool get_weather

The kernel pipes the tool input as JSON on **stdin** and reads the result
from **stdout**. Each tool call is a separate process invocation.

Example `run.py`:

```python
import json, sys

def tool_get_weather(params):
    location = params["location"]
    # ... do something ...
    return json.dumps({"temperature": "20C"})

if __name__ == "__main__":
    if sys.argv[1] == "tool":
        params = json.load(sys.stdin)
        print(globals()[f"tool_{sys.argv[2]}"](params))
```

Tool definitions use kernel format:
- `name` — globally unique tool name
- `description` — what the tool does
- `params` — object of `{name: {type, description}}`
- `required` — list of required parameter names

## Slash commands (user-invocable skills)

Skills with `user-invocable: true` in frontmatter are exposed as `/name` slash
commands in the gateway CLI.

```
---
name: weather
description: "Get weather"
user-invocable: true
---

# Weather

Instructions for the LLM when this command is invoked...
```

When user types `/weather москва`, the gateway:

1. Reads the SKILL.md body (everything after frontmatter)
2. Appends user arguments: `"{body}\n\nUser request: москва"`
3. Sends the combined text as a regular message to the kernel
4. LLM receives the skill instructions + user request and acts accordingly

Gateway also provides builtin commands (`/help`, `/exit`) that are handled
locally without sending to the kernel.

Tab autocomplete is supported: typing `/we` + Tab completes to `/weather `.

### Discovery

`boot.py` provides `discover_slash_commands()` which scans all SKILL.md files
for `user-invocable: true` and returns `[{name, description, body}]`. Gateway
loads this at startup.

## Shared library

`skills/lib/` provides Python helpers:

- `kernel_client.py` — WebSocket connection to kernel
- `driver_runtime.py` — base class for LLM drivers
- `subagent_runtime.py` — base class for subagents
- `providers.py` — LLM provider adapters (Anthropic, OpenAI)
- `compaction.py` — conversation compaction utilities
- `filelock.py` — cross-process file locking

## Project files

User-editable files in `~/.tabula/` are injected into the system prompt:

- `IDENTITY.md` — agent identity: name, personality, language (main agent only)
- `SOUL.md` — personality, tone, style (main agent only)
- `USER.md` — user context: name, timezone, preferences (main agent only)
- `AGENTS.md` — workspace instructions and behavioral rules (main + subagents)

Subagents receive a minimal prompt (no skills, memory, SOUL, or USER).
The subagent runtime reads `~/.tabula/state/subagent/prompt.txt` written by boot.py at startup.

## Creating new skills

1. Create directory: `shell_exec mkdir -p skills/<name>`
2. Write `SKILL.md` with frontmatter and documentation
3. Write `run.py` entry point
4. If it's a tool-skill: add `tools` to frontmatter, implement `tool` subcommand
5. If it's a hook-skill: add hook subscriptions in `connect` message
6. If it's a daemon: add spawn entry in `boot.py`'s `build_spawn()`

### Naming conventions

- Skills named `driver-<provider>` or `subagent-<provider>` are filtered by
  `TABULA_PROVIDER` — only the active provider's skills are loaded.
- Tool names must not collide with kernel tools (`shell_exec`, `process_spawn`, `process_kill`, `process_list`).
  Duplicate tool names across skills trigger a warning; the last one wins.

Repository note: the source tree may organize reusable skill sets differently
inside the repo, but the installed agent-facing runtime contract stays flat:
`boot.py`, `templates/`, and `skills/`. Skills available to the agent should be
materialized as ordinary directories under `skills/`, not exposed as an extra
bundle layer.
