---
name: skill-contract
description: "Skill format spec. Use `EXEC cat skills/skill-contract/SKILL.md` to read. To discover skills: `EXEC ls skills/`"
---
# Tabula Skill Format

This document defines how skills work in Tabula. Read it to understand how to
discover, use, and create skills.

## What is a skill?

A skill is a directory under `./skills/` containing at least:

- `SKILL.md` — describes what the skill does and how to use it.
- An executable entry point (e.g., `run.py`).

Skills communicate with the kernel via **WebSocket**, not stdin/stdout.

## SKILL.md format

Every skill directory must have a `SKILL.md` with optional YAML frontmatter:

```
---
name: skill-name
description: "Short description shown in system prompt"
inject: none
tools:
  [{"name": "tool_name", "description": "What the tool does",
    "params": {"arg": {"type": "string", "description": "Argument"}},
    "required": ["arg"]}]
---

# Skill Name

Full documentation body. Injected into system prompt unless inject: none.

## Usage

How to run this skill.
```

### Frontmatter fields

- `name` — skill identifier (defaults to directory name)
- `description` — short description injected into system prompt
- `inject: none` — hides skill from system prompt (for internal skills like
  drivers, gateways, hooks)
- `tools` — JSON array of tool definitions (see Tool-Skills below)

## Discovering skills

To see available skills:

    EXEC ls skills/

To read a skill's documentation:

    EXEC cat skills/<name>/SKILL.md

## Running a skill

Skills are started via SPAWN and connect to the kernel WebSocket:

    SPAWN python3 skills/<name>/run.py [args]

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
{"type": "join", "session": "main"}
{"type": "message", "text": "hello from skill"}
```

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
`after_tool_call`, `session_start`, `before_spawn`, `after_spawn`.

Strategies:
- **void**: fire-and-forget (after_*, session_start). No response needed.
- **modifying**: sequential by priority (before_*). Can pass, modify, or block.

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

## Shared library

`skills/lib/` provides Python helpers:

- `kernel_client.py` — WebSocket connection to kernel
- `driver_runtime.py` — base class for LLM drivers
- `subagent_runtime.py` — base class for subagents
- `providers.py` — LLM provider adapters (Anthropic, OpenAI)

## Creating new skills

1. Create directory: `EXEC mkdir -p skills/<name>`
2. Write `SKILL.md` with frontmatter and documentation
3. Write `run.py` entry point
4. If it's a tool-skill: add `tools` to frontmatter, implement `tool` subcommand
5. If it's a hook-skill: add hook subscriptions in `connect` message
6. If it's a daemon: add spawn entry in `boot.py`'s `build_spawn()`
