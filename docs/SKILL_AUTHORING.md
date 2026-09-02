# Skill Authoring

This document explains how to write **skills**: prompt/instruction artifacts
with optional resources. Executable tools and long-lived components are
**plugins**; author those with [PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md).

## When to write a skill vs a plugin

Pick a **skill** if your component only needs to add instructions, workflow
guidance, references, slash-command text, or helper resources.

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
  (`base/`, `workspace/`, `drivers/`, `memory/`, `caveman/`, `code/`);
- shared SDK/runtime libraries are packaged with bundles, primarily under
  `tabula-bundles/_lib`.

The active distro plus its bundles are fanned out into `$TABULA_HOME/skills/`
by `tabula-install distro install` or project-scoped `tabula-agent install/apply`.

External Agent Skills installed by ecosystem tools such as `npx skills` should
use `$TABULA_WORKSPACE/skills/`, `$TABULA_WORKSPACE/.agents/skills/`, or
`${XDG_CONFIG_HOME:-~/.config}/agents/skills/`, not `$TABULA_HOME/skills/`.
Configured `external_roots` are exact directories whose immediate children are
skills. Workspace discovery registers the two workspace skill directories even
before they exist, so an agent can create the first skill there.

When the skills plugin is installed, use its self-contained `skill_read`,
`skill_write`, `skill_edit`, and `skill_delete` tools. Runtime skills under
`$TABULA_HOME/skills` are read-only; external roots are writable. The tools can
manage all skill-local resources, including `references/`, `scripts/`, and
assets, without requiring the generic filesystem plugin. `skill_write` and
`skill_edit` validate `SKILL.md` frontmatter and require `name` to match the
containing directory.

---

# Authoring a skill

A skill is a directory with a `SKILL.md` manifest plus optional references,
assets, and helper scripts. Skills are prompt/instruction artifacts. They do
not publish executable tools.

## Minimum viable skill

`my-skill/SKILL.md`:

```md
---
name: my-skill
description: "Short one-line description"
---

# my-skill

Explain what the skill does and how to use it.
```

## `SKILL.md` frontmatter

Required fields:

- `name` — skill identifier; must match the directory name.
- `description` — one-line summary; injected into the system prompt.

Optional:

- `user-invocable: true` — exposes the skill as a `/name` slash command in
  the gateway. The body of `SKILL.md` becomes the instruction text.

```md
---
name: weather
description: "Get weather for a city"
user-invocable: true
---

# weather

Full documentation body.
```

## Executable capabilities

Executable tools belong to plugins, not skills. If your workflow needs a real
tool, add a plugin with `plugin.toml` and register the tool there. The skill can
then explain when to use that plugin tool.

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

For plugin authors: do **not** open WebSocket directly. Use the runtime worker
protocol via `tabula-runtime`; the runtime owns the kernel transport for you.

---

# Naming conventions

Some names carry behavior by convention (not enforced by the kernel):

- `gateway-*` — user interface plugin.
- `hook-*` — bus subscriber plugin.
- `gateway-*` — user interface apps/plugins.

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
Anthropic-compatible `name`, `description`, and optional `user-invocable`.
Executable tool contracts are plugin-owned and documented separately in
[PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md).

# Recommended workflow

1. Decide skill vs plugin (see top of this doc).
2. Create the directory.
3. Write `SKILL.md` first.
4. Implement the smallest useful entry point.
5. Test it as a subprocess.
6. Only then add config complexity or helper abstractions.

For most new user-facing guidance or workflow presets, start with a skill. For
most new executable capabilities, start with a plugin.

# Examples in the repo

- skill: `tabula-guide/` and plugin: `workspace/fs/` in
  [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles);
- distro materialization: `claw` and `code` in `tabula-distrib`.
