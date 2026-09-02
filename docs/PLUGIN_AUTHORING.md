# Plugin Authoring

Plugins are Tabula's executable extension shape. Use a plugin when a component
needs to publish tools, keep state, subscribe to kernel events, own child
processes, or update its tool catalog after startup. Instruction-only workflows
should stay as skills; see [SKILL_AUTHORING.md](SKILL_AUTHORING.md).

This document describes the runtime-owned plugin worker contract implemented by
`tabula-runtime` and demonstrated by the migrated bundle plugins.

## Plugin directory layout

```text
my-plugin/
├── plugin.toml
├── run.py          # plugin entry script (Python)
└── README.md       # optional human documentation
```

`plugin.toml` is the install/discovery manifest. The runtime worker is the
authoritative source for its active tools and hook subscriptions: after launch,
the worker replies to `init` with `init_ack`, and it may later send
`tools_updated` to replace its catalog.

## `plugin.toml`

Minimum manifest:

```toml
id = "my-plugin"
name = "My Plugin"
version = "0.1.0"
description = "Optional human summary"

[worker]
command = ["python3", "run.py"]  # argv; relative paths resolve from plugin root
mode = "warm"                      # or "cold"
scope = "tenant"                   # default; use "runtime" for one warm worker per runtime

[kind]
name = "gateway"                   # optional runtime composition class
singleton = false                   # optional: reject multiple plugins of this kind in one catalog

[[tools]]
name = "my_tool"
description = "Optional advisory metadata"
deadline_ms = 30000

[[hooks]]
event = "before_tool_call"
priority = 50
```

Validation rules enforced by `internal/runtime/host/manifest/manifest.go`:

- `id`, `name`, `version`, and `[worker].command` are required.
- `id` must match `^[a-z0-9_-]+$`.
- `version` must be SemVer-shaped (`X.Y.Z`, with optional prerelease/build).
- `[worker].mode` must be `warm` or `cold`; cold plugins cannot publish hooks.
- `[worker].scope` defaults to `tenant`. `runtime` is allowed only for warm
  plugins and shares one worker across all tenants served by the runtime.
- `command` must be a non-empty argv list. Relative paths resolve from the
  plugin root.
- advisory `[[tools]]` entries require non-empty `name`; `deadline_ms` must be
  non-negative.
- advisory `[[hooks]]` entries require non-empty `event`.
- optional `[kind]` metadata classifies plugins for runtime composition. The
  manifest declares what the plugin is; runtime config declares how kinds depend
  on each other. If any plugin declares `kind.singleton = true`, a catalog with
  more than one plugin of that kind is rejected.

Legacy `runtime`/`entry` manifests are still accepted for compatibility, but
new plugins should use `[worker]`.

Unknown manifest keys are tolerated for forward compatibility. The kernel
ignores tags/UI hints unless a distro or UI chooses to use them.

Runtime-side kind wiring lives in `$TABULA_HOME/config/runtime.toml`, not in
plugin manifests. Example:

```toml
[plugin_kinds.gateway]
depends_on = ["driver"]
```

When priming warm runtime plugins, `tabula-runtime` starts dependency kinds
first. If a configured dependency kind is missing or fails to become ready, the
dependent kind is not started during that prime pass.

## Plugin config

`plugin.toml` is not the runtime config file. A plugin owns its config contract
in code and should load resolved values through the SDK.

Python plugins use `load_plugin_config`:

```python
from tabula_plugin_sdk import load_plugin_config

settings = load_plugin_config(
    "my-plugin",
    defaults={"greeting": "hello"},
    env={"greeting": "TABULA_MY_PLUGIN_GREETING"},
    explicit={"greeting": cli_args.greeting},
)
```

The standard precedence is:

1. code defaults
2. `$TABULA_HOME/config/global.toml` under `[plugins.<plugin-id>]`
3. `$TABULA_HOME/config/plugins/<plugin-id>/config.toml`
4. tenant effective `$TABULA_TENANT_DIR/config/plugins/<plugin-id>/config.toml`,
   when present
5. environment variables declared by the plugin
6. explicit runtime or CLI arguments

Example plugin-local config:

```toml
# $TABULA_HOME/config/plugins/my-plugin/config.toml
greeting = "hello from config"
```

Equivalent global config:

```toml
# $TABULA_HOME/config/global.toml
[plugins.my-plugin]
greeting = "hello from global config"
```

## Lifecycle

1. `tabula-runtime` loads `plugin.toml` and starts the configured worker as a
   child process.
2. Runtime writes one NDJSON `init` frame to worker stdin. The frame carries
   `kernel_id`, `tenant_id`, `target_id`, and the normalized manifest.
3. Worker replies with `init_ack`, including its active tools and hook
   subscriptions.
4. Runtime publishes those capabilities to the kernel over the Runtime API.
5. Runtime sends `call`, `event`, and `shutdown` frames as needed.
6. Worker replies with `result` and `event_reply`, and may emit `send`, `log`,
   and `tools_updated` frames.

Runtime diagnostics are available from `tabula status --json` and the local
`GET /internal/snapshot/runtimes` diagnostics endpoint. The runtime snapshot is
loopback-only and should not be exposed through public ingress.

## Protocol framing and methods

The runtime↔worker channel is NDJSON over stdio: one UTF-8 JSON object per
line, maximum 10 MiB per line. Stdout is reserved for protocol messages; stderr
is captured and redacted into runtime diagnostics.

Every message has this shape:

```json
{"op":"result","call_id":"tc-123","ok":true,"data":{"ok":true}}
```

Supported worker operations:

| Direction | Operation | Purpose |
|-----------|-----------|---------|
| runtime → worker | `init` | initial target identity + manifest |
| worker → runtime | `init_ack` | active tool catalog and hook subscriptions |
| runtime → worker | `call` | invoke a worker-registered tool |
| worker → runtime | `result` | reply to a `call` |
| runtime → worker | `event` | deliver subscribed bus/hook event |
| worker → runtime | `event_reply` | reply to modifying/claiming events |
| worker → runtime | `send` | emit a bus message (`channel = "bus"`) |
| worker → runtime | `log` | structured log event |
| worker → runtime | `tools_updated` | atomically replace this worker's tool catalog |
| runtime → worker | `shutdown` | request graceful exit |

The worker protocol is versioned by the Runtime API / SDK release pair. See
`docs/PROTOCOL.md` for the current wire shapes. A `shutdown` frame with
`final = true` means the runtime itself is exiting; absent or false means only
the worker is being replaced. Detached child-process supervisors must stop
children on final shutdown and may preserve them for replacement workers under
the same runtime process. Persist the owning runtime PID (`os.getppid()` for a
direct Python worker child), reject adoption from a different runtime PID, and
monitor that PID so abrupt runtime loss does not orphan the child.

## Minimal Python worker example

The packaged Python SDK lives in the bundle `_lib` surface. A plugin can also
implement the worker protocol directly; this is the smallest complete shape:

```python
#!/usr/bin/env python3
from __future__ import annotations

import json
import sys

TOOLS = [{"name": "hello_ping", "description": "Return a greeting"}]


def send(frame: dict) -> None:
    sys.stdout.write(json.dumps(frame, separators=(",", ":")) + "\n")
    sys.stdout.flush()


for line in sys.stdin:
    if not line.strip():
        continue
    frame = json.loads(line)
    if frame.get("op") == "init":
        send({"op": "init_ack", "ready": True, "tools": TOOLS, "subscriptions": []})
    elif frame.get("op") == "call":
        args = frame.get("args") or {}
        send({
            "op": "result",
            "call_id": frame.get("call_id", ""),
            "ok": True,
            "data": {"ok": True, "text": "hello, " + (args.get("name") or "world")},
        })
    elif frame.get("op") == "shutdown":
        break
```

The installed SDK may wrap this protocol with higher-level helpers, but the
`op`-based frames above are the runtime contract.

## Hook event replies

For modifying or claiming hook events, return one of:

```python
{"action": "ok"}
{"action": "rewrite", "data": {...}}
{"action": "deny", "reason": "human-readable reason"}
{"action": "claim", "data": {...}}
```

The kernel maps these to its existing hook engine actions (`pass`, `modify`,
`block`, `claim`). `before_spawn` and `after_spawn` remain reserved event names;
process lifecycle semantics belong to domain plugins such as `subagents`.

## Child processes and shutdown

Plugins own their children. If a plugin spawns subprocesses, it should place
them in its own process group/session, trap shutdown, and terminate children
before exiting. The kernel owns level-one plugin supervision and kills the
plugin process group when necessary, but it does not reach into plugin-internal
state.

Plugins should handle `shutdown` and clean up before exiting.

## Metrics convention

There is no separate `metric` protocol method. Emit metrics through structured
`log` fields:

```python
api.log(
    "tool_call_completed",
    metric_name="tool_calls_total",
    metric_value=1,
    metric_kind="counter",
    tool="hello_ping",
)
```

Telemetry plugins can subscribe to log/bus events and export them elsewhere.

## Local validation

Run focused runtime and manifest checks:

```sh
go test ./internal/runtime/host/manifest ./internal/runtime/host/pool ./internal/runtime/worker/wire -count=1
```
