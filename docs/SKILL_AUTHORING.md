# Skill Authoring

This document explains how to write a skill using the current built-in
assistant convention.

It is practical on purpose: what files to create, what contracts matter, and
which parts are stable versus still evolving.

## Scope of this document

At the platform level, Tabula does not require that a skill be expressed as a
directory with `SKILL.md` and `run.py`. The kernel only cares about the wire
protocol, executable commands, and the boot JSON it receives.

This document is about the current built-in `assistant` convention, because
that is the main user-facing extension model in the repo today.

## What a skill is in the assistant convention

In the built-in assistant convention, a skill is commonly represented as a
directory that contains at least:

```text
my-skill/
├── SKILL.md
└── run.py
```

Here, `SKILL.md` is both:

- machine-readable metadata for boot
- human-readable documentation for users and agents

`run.py` is one common executable entrypoint used by built-in skills. It is not
required by the platform.

There is no separate format for "skills written by the agent" inside this
convention. The same structure is used whether a human writes the skill, a
bundle installs it, or the agent creates it for itself.

## Where skills live

At runtime, the active skill surface is flat:

```text
~/.tabula/skills/
```

In source, skills may live in one of three places:

- distro-specific skills in [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib):
  `familiar/skills/`, `guardian/skills/`, `ouroboros/skills/`
- shared bundles in [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles):
  `base/`, `files/`, `drivers/`, `memory/`, `caveman/`
- the kernel-side runtime contract in this repo: `skills/_lib/`

The active distro plus its declared bundles are fanned out into
`~/.tabula/skills/` by `tabula-distro install`.

When you write a new skill for your local agent, the important place is the
runtime surface the boot script sees.

## The minimum viable skill

`SKILL.md`:

```md
---
name: my-skill
description: "Short one-line description"
---

# my-skill

Explain what the skill does and how to use it.
```

One possible executable entrypoint:

```python
#!/usr/bin/env python3
from __future__ import annotations

import os
import sys

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)


def main() -> None:
    print("hello from my-skill")


if __name__ == "__main__":
    main()
```

This skill does not yet connect to the kernel or expose tools, but it shows the
common built-in file shape.

## `SKILL.md`

In the assistant convention, `SKILL.md` is the first thing boot reads.

Current commonly used frontmatter fields:

- `name` — skill identifier; defaults to directory name if omitted
- `description` — one-line summary; injected into the system prompt
- `user-invocable: true` — exposes the skill as `/name` in the CLI gateway
- `tools` — JSON array of tool definitions

Example:

```md
---
name: weather
description: "Get weather for a city"
user-invocable: true
tools:
  [
    {
      "name": "get_weather",
      "description": "Get weather for a location",
      "params": {
        "location": {"type": "string", "description": "City or place"}
      },
      "required": ["location"]
    }
  ]
---

# weather

Full documentation body.
```

### What boot uses from `SKILL.md`

Depending on distro, boot may use `SKILL.md` to:

- discover tools
- discover slash commands
- inject descriptions into the system prompt
- filter or group skills by naming convention

Assistant boot is dynamic and reads many skills recursively. Guardian boot is
mostly fixed and does not depend on flexible skill discovery the same way.

## Skill types

The kernel does not have a hardcoded "skill type" field, but in practice there
are four common shapes.

### 1. Tool skill

In the assistant convention, a tool skill exposes one or more LLM tools through
`SKILL.md` frontmatter and usually implements them through an executable
entrypoint. If no explicit `exec` is supplied, assistant boot currently assumes
the `run.py tool <name>` convention.

Example shape:

```text
files/
├── SKILL.md
└── run.py
```

Common built-in fallback shape:

```python
#!/usr/bin/env python3
from __future__ import annotations

import json
import sys


def tool_echo(params: dict) -> str:
    return params.get("text", "")


TOOLS = {
    "echo": tool_echo,
}


def main() -> None:
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        tool_name = sys.argv[2]
        handler = TOOLS[tool_name]
        params = json.load(sys.stdin)
        print(handler(params))
        return
    raise SystemExit("usage: run.py tool <tool_name>")


if __name__ == "__main__":
    main()
```

Execution model in assistant today:

- the driver emits `tool_use`
- the kernel spawns an executable command for the tool
- if `exec` was omitted, assistant boot may synthesize something like
  `python3 skills/foo/run.py tool echo`
- JSON params are piped on stdin
- stdout becomes the tool result

This means each tool call is process-isolated.

### 2. Gateway skill

A gateway is a long-running process that joins a session and relays messages
between a user-facing interface and the kernel.

Examples:

- `gateway-cli`
- `gateway-api`
- `gateway-telegram`

Typical protocol shape:

- sends: `message`, sometimes `cancel`, sometimes direct `tool_use`
- receives: stream events, `done`, `error`, status messages

Gateways usually:

1. connect to `TABULA_URL`
2. send `connect`
3. join a session
4. optionally spawn the active driver if one is not running
5. relay user messages and display streamed output

### 3. Hook skill

A hook skill subscribes to kernel lifecycle events using the `hooks` field in
its `connect` message.

Examples:

- `hook-logger`
- `hook-permissions`

Minimal pattern:

```python
conn.send({
    "type": "connect",
    "name": "my-hook",
    "sends": ["hook_result"],
    "receives": ["hook"],
    "hooks": [
        {"event": "after_message", "priority": 0},
    ],
})
```

Hook skills may be:

- **void** — observe only
- **modifying** — pass, modify, or block
- **claiming** — first claimer wins

If you subscribe to a modifying hook, you must send `hook_result`.

### 4. Driver or subagent skill

These are advanced skill types. In most cases you should not write one from
scratch unless you are extending provider support or changing core runtime
behavior.

Shared runtimes exist for these in the `drivers` bundle
([`tabula-bundles`](https://github.com/bamanoz/tabula-bundles)):

- `skills._drivers.driver_runtime`
- `skills._drivers.subagent_runtime`

If you need a normal capability, a tool skill is almost always the right shape.

## Connecting to the kernel

Long-running skills connect over WebSocket using `skills/_lib/kernel_client.py`.

Minimal pattern:

```python
#!/usr/bin/env python3
from __future__ import annotations

import os
import sys

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills._lib.kernel_client import KernelConnection
from skills._lib.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE


def main() -> None:
    url = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "my-daemon",
        "sends": [MSG_MESSAGE],
        "receives": [MSG_MESSAGE],
    })
    conn.recv()  # connected
    conn.send({"type": MSG_JOIN, "session": "main"})
    conn.recv()  # joined
    conn.send({"type": MSG_MESSAGE, "text": "hello"})
    conn.close()


if __name__ == "__main__":
    main()
```

`KernelConnection.send()` injects the protocol version automatically if you do
not provide it.

## Slash commands

If assistant-style `SKILL.md` contains:

```md
user-invocable: true
```

then the CLI gateway exposes the skill as `/name`.

The body of `SKILL.md` becomes the instruction text for that slash command.
The gateway appends the user's arguments and sends the result as a normal
message.

This is the easiest way to create a lightweight behavior preset without adding
a new tool.

## Tool definitions

Tool definitions in frontmatter use the kernel tool schema shape:

- `name` — globally unique tool name
- `description` — short explanation
- `params` — object of parameter definitions
- `required` — list of required parameter names

Example:

```json
{
  "name": "get_weather",
  "description": "Get weather for a location",
  "params": {
    "location": {"type": "string", "description": "City or place"}
  },
  "required": ["location"]
}
```

Tool names must not collide with kernel tools:

- `shell_exec`
- `process_spawn`
- `process_kill`
- `process_list`

## Naming conventions that already matter

Some names have behavior attached by convention.

- `driver-<provider>` and `subagent-<provider>` are filtered by the active
  provider in assistant boot.
- `gateway-*` implies a user interface skill in project convention.
- `hook-*` implies a hook subscriber in project convention.

The kernel itself does not enforce these names, but boot logic and team
conventions do.

## Environment variables you can rely on

Common runtime variables:

- `TABULA_HOME` — root of the installed runtime, usually `~/.tabula`
- `TABULA_URL` — kernel WebSocket URL
- `TABULA_PROVIDER` — active provider in assistant-like distros
- `TABULA_SESSION` — current session for some tool invocation paths
- `TABULA_SPAWN_TOKEN` — inherited by spawned children for spawn-depth policy

Depending on how the skill is launched, more variables may exist. Do not assume
everything is always present.

## When to use `skills/_lib/`

Use `skills/_lib/` when it removes boilerplate and matches an existing pattern.

Good candidates:

- `kernel_client.KernelConnection`
- protocol constants from `skills._lib.protocol`
- path helpers from `skills._lib.paths`
- config loading helpers already used by existing skills

Be careful with deeper imports.

### Current stability note

There are three different contracts in Tabula, and they are not equally stable:

1. **Wire protocol** (`internal/kernel/protocol.go`, `skills/_lib/protocol.py`)
   — closest to stable
2. **assistant `SKILL.md` frontmatter contract** — mostly stable in practice,
   not yet explicitly versioned
3. **`skills/_lib/` runtime API** — useful, but not yet a formally versioned
   public package

If you are writing reusable community skills, prefer depending on:

- the wire protocol
- a small subset of `skills/_lib/` helpers and, if needed, the current
  assistant fallback `run.py tool <name>` convention
- a small subset of `skills/_lib/` helpers

Do not assume every helper inside `skills/_lib/` is permanent API.

## Where to put docs

Keep the skill's operational documentation inside its own `SKILL.md`.

Useful sections include:

- what the skill does
- how to run it
- protocol shape
- config keys
- environment variables
- important caveats

Remember: the user and the agent both read this file.

## Recommended development flow

1. create the skill directory
2. write `SKILL.md` first
3. implement the smallest useful `run.py`
4. test it as a subprocess or long-running skill, depending on its type
5. only then add config complexity or helper abstractions

For most new capabilities, start with a tool skill before reaching for a
gateway, hook, or custom runtime.

## Examples in the repo

Useful reference skills (in their respective repos):

- tool skill: `files/files/` in [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles)
- hook skill: `base/hook-logger/` in `tabula-bundles`
- gateway: `familiar/skills/gateway-cli/` in [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib)
- subagent runtime: `drivers/subagent-openai/` in `tabula-bundles`
- minimal fixed distro boot: `guardian/boot.py` in `tabula-distrib`

## Future stabilization work

Still to be made explicit:

- versioned familiar `SKILL.md` contract
- narrower, documented public surface for `skills/_lib/`
- clearer contract for community-distributed skills and bundles

Until then, write skills conservatively: simple file layout, small dependency
on shared runtime internals, explicit docs.
