# Skill Authoring

This document explains how to write **skills**: per-call, stateless tool
providers. Long-lived components are **plugins**; author those with
[PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md). The architectural rationale lives
in [plans/SKILL_PLUGIN_ARCHITECTURE.md](plans/SKILL_PLUGIN_ARCHITECTURE.md).

## When to write a skill vs a plugin

Pick a **skill** if your component:

- exposes one or more tools that run to completion per call;
- has no persistent state between calls;
- doesn't need to subscribe to bus events;
- doesn't spawn its own children.

Pick a **plugin** if your component:

- runs continuously (driver, gateway, daemon);
- subscribes to lifecycle events (`before_tool_call`, `session_start`, ...);
- holds state, caches connections, or supervises children;
- registers tools dynamically (e.g. MCP first-class `mcp__server__tool`).

Most new capabilities should start as a skill. Reach for a plugin only when
one of the long-lived requirements actually applies.

## Where things live

At runtime, the active surface is flat:

```text
$TABULA_HOME/skills/
```

In source, components may live in three places:

- distro-specific in `tabula-distrib/<name>/skills/...` or
  `tabula-distrib/<name>/plugins/...`;
- shared in [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles)
  (`base/`, `files/`, `drivers/`, `memory/`, `caveman/`, `coder-*/`);
- shared SDK/runtime libraries are packaged with bundles (the Phase 3 reference
  Python plugin SDK lives in `examples/plugin-sdk-python/`; final SDK authority
  moves to `tabula-bundles` during library relocation).

The active distro plus its bundles are fanned out into `$TABULA_HOME/skills/`
by `tabula-distro install`.

External Agent Skills installed by ecosystem tools such as `npx skills` should
use `$TABULA_WORKSPACE/skills/`, `$TABULA_WORKSPACE/.agents/skills/`, or
`${XDG_CONFIG_HOME:-~/.config}/agents/skills/`, not `$TABULA_HOME/skills/`.
Claw discovers those roots as instruction-only skills.

---

# Authoring a skill

A skill is a directory with a `SKILL.md` manifest and one or more executable
entry points referenced by `tools[].exec`. The kernel runs the `exec`
command per tool call, pipes JSON params on stdin, and reads the result
from stdout.

## Minimum viable skill

`my-skill/SKILL.md`:

```md
---
name: my-skill
description: "Short one-line description"
tools:
  - name: my_tool
    description: "What this tool does"
    params:
      text: { type: string, description: "Input text" }
    required: [text]
    exec: "<venv_python> skills/my-skill/scripts/run.py tool my_tool"
---

# my-skill

Explain what the skill does and how to use it.
```

`my-skill/run.py`:

```python
#!/usr/bin/env python3
from __future__ import annotations
import json, sys


def tool_my_tool(params: dict) -> str:
    return params.get("text", "").upper()


TOOLS = {"my_tool": tool_my_tool}


def main() -> None:
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        handler = TOOLS[sys.argv[2]]
        params = json.load(sys.stdin)
        print(handler(params))
        return
    raise SystemExit("usage: run.py tool <tool_name>")


if __name__ == "__main__":
    main()
```

That's the whole skill. The script name is arbitrary; only the `exec`
command matters.

## `SKILL.md` frontmatter

Required fields:

- `name` — skill identifier, defaults to the directory name.
- `description` — one-line summary; injected into the system prompt.
- `tools` — array of `{name, description, params, required, exec}`.

Optional:

- `user-invocable: true` — exposes the skill as a `/name` slash command in
  the gateway. The body of `SKILL.md` becomes the instruction text.

```md
---
name: weather
description: "Get weather for a city"
user-invocable: true
tools:
  - name: get_weather
    description: "Get weather for a location"
    params:
      location: { type: string, description: "City or place" }
    required: [location]
    exec: "<venv_python> skills/weather/scripts/run.py tool get_weather"
---

# weather

Full documentation body.
```

## Tool execution model

- Driver emits `tool_use`.
- Kernel resolves the tool name to its `exec` command.
- Kernel spawns the command, pipes JSON params on stdin.
- Stdout becomes the tool result.
- Each call is fully process-isolated.

This means skills can be written in any language. The `exec` command can
target a Python script, a Node script, a shell script, or a compiled
binary.

## Slash commands

If `user-invocable: true` is set, the CLI gateway exposes the skill as
`/name`. The body of `SKILL.md` becomes the instruction text. The gateway
appends the user's arguments and sends the result as a normal message.

This is the easiest way to create a behavior preset without adding a tool.

---

# Plugins

Long-lived extensions, hook subscribers, dynamic tool providers, drivers, and
gateways are plugins. Author them with [PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md).

---

# Connecting to the kernel directly

Long-running components should normally be plugins and use
[PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md). Low-level clients that still need a
raw WebSocket connection can use the shared Python kernel client helper (today
available in the legacy runtime package, moving to `tabula_plugin_sdk` during
library relocation):

```python
#!/usr/bin/env python3
from __future__ import annotations
import os, sys

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from tabula_plugin_sdk.kernel_client import KernelConnection
from tabula_plugin_sdk.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE


def main() -> None:
    url = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "my-daemon",
        "sends": [MSG_MESSAGE],
        "receives": [MSG_MESSAGE],
    })
    conn.recv()                                # connected
    conn.send({"type": MSG_JOIN, "session": "main"})
    conn.recv()                                # joined
    conn.send({"type": MSG_MESSAGE, "text": "hello"})
    conn.close()


if __name__ == "__main__":
    main()
```

`KernelConnection.send()` injects the protocol version automatically.

For plugin authors: do **not** open WebSocket directly. Use the
`register(api)` API instead — the plugin runtime owns the kernel transport
for you.

---

# Naming conventions

Some names carry behavior by convention (not enforced by the kernel):

- `gateway-*` — user interface plugin.
- `hook-*` — bus subscriber plugin.
- `coder-*` bundle prefix — components used by the `coder` distro.

These are project conventions; distros and bundles enforce them.

# Environment variables

Common runtime variables your component can rely on:

- `TABULA_HOME` — root of the installed runtime, usually `~/.tabula`.
- `TABULA_URL` — kernel WebSocket URL.
- `TABULA_PROVIDER` — active LLM provider.
- `TABULA_SESSION` — current session for some tool invocation paths.

Subagent child credentials, if any, are private to the subagent plugin contract;
do not rely on a common runtime spawn-token environment variable.

Do not assume every variable is always present.

# When to use `tabula_plugin_sdk`

Use it when it removes boilerplate and matches an existing pattern:

- `kernel_client.KernelConnection`;
- protocol constants from `tabula_plugin_sdk.protocol`;
- path helpers from `tabula_plugin_sdk.paths`;
- config helpers already used by existing components.

Be careful with deeper imports: only the small set above is treated as
public surface today.

# Stability

The `SKILL.md` frontmatter contract is the reusable skill authoring surface:
Anthropic-compatible `name`, `description`, `tools`, and `user-invocable`, plus
Tabula-specific `tools[].exec`. Plugin contracts are documented separately in
[PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md).

# Recommended workflow

1. Decide skill vs plugin (see top of this doc).
2. Create the directory.
3. Write `SKILL.md` first.
4. Implement the smallest useful entry point.
5. Test it as a subprocess.
6. Only then add config complexity or helper abstractions.

For most new capabilities, start with a skill.

# Examples in the repo

- skill: `files/files/` and `code/git/` in
  [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles);
- minimal fixed distro boot: `guardian/boot.py` in `tabula-distrib`.
