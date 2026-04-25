---
name: pylib
description: "Shared kernel-level Python runtime library. Provides `kernel_client` (WebSocket wrapper), `protocol` (message/hook/tool constants), `paths` (TABULA_HOME conventions), `config` (typed skill config loader), and `filelock`. Driver- and provider-specific code lives in the `drivers` bundle, not here."
---
# skills/_pylib — kernel Python runtime library

The narrow runtime surface every Tabula skill can rely on.

Everything here is deliberately kernel-oriented — it talks to the kernel, honours
the wire protocol, reads `$TABULA_HOME`, or exposes machinery so small it has
no sensible home anywhere else. Distro-specific code (drivers, subagents,
prompt assembly, compaction, provider selection, gateway XML) does **not**
live here; it lives in the relevant distro or bundle.

## Modules

### `skills._pylib.kernel_client`

`KernelConnection` — thread-safe WebSocket wrapper around the kernel protocol.

```python
from skills._pylib.kernel_client import KernelConnection

conn = KernelConnection("ws://localhost:8089/ws")
conn.send({"type": "connect", "name": "my-skill", ...})
msg = conn.recv(timeout=5.0)      # dict or None on close
conn.close()
```

### `skills._pylib.protocol`

Symbolic names for everything crossing the wire: `MSG_*`, `TOOL_*`, `HOOK_*`,
`DEFAULT_KERNEL_TOOLS`. Skills **should** import from here rather than typing
literals.

### `skills._pylib.paths`

`$TABULA_HOME` layout conventions. Each kind of skill file (config, data,
state, run, logs) has a canonical location; helpers return the `Path`.

### `skills._pylib.config`

`load_skill_config` / `load_global_config` / `SkillConfigError` — typed loader
for `SKILL.config.json` entries combining env vars, `config/skills/<id>.toml`,
`secrets.json`, and schema defaults.

### `skills._pylib.filelock`

Cross-platform advisory file locking used by `cron`, task queues and anything
that multiplexes a single on-disk JSON.

## What used to live here

`providers`, `driver_runtime`, `subagent_runtime`, `prompt_builder`,
`compaction`, `provider_selection` all moved to the `drivers` bundle
(`skills/_drivers/`). A distro that wants LLM drivers or subagents should
include that bundle via its `distro.toml`.
