# Plugin Authoring

Plugins are Tabula's long-lived extension shape. Use a plugin when a component
needs to keep state, subscribe to kernel events, own child processes, or update
its tool catalog after startup. Per-call, stateless tool providers should stay
as skills; see [SKILL_AUTHORING.md](SKILL_AUTHORING.md).

This document describes the in-tree PluginRuntime contract implemented by the
kernel and demonstrated by `examples/plugin-hello/`.

## Plugin directory layout

```text
my-plugin/
├── plugin.toml
├── run.py          # plugin entry script (Python)
└── README.md       # optional human documentation
```

`plugin.toml` is the install/discovery manifest. The runtime process is the
authoritative source for its active tools and hook subscriptions: after launch,
the plugin replies to `register_request` with `register`, and it may later send
`update_tools` to replace its catalog.

## `plugin.toml`

Minimum manifest:

```toml
id = "my-plugin"
name = "My Plugin"
version = "0.1.0"
runtime = "python"          # python (only Python is supported today)
entry = "run.py"            # relative to this directory
description = "Optional human summary"

[[tools]]
name = "my_tool"
description = "Optional advisory metadata"
deadline_ms = 30000

[[hooks]]
event = "before_tool_call"
priority = 50
```

Validation rules enforced by `internal/kernel/plugin/manifest.go`:

- `id`, `name`, `version`, `runtime`, and `entry` are required.
- `id` must match `^[a-z0-9_-]+$`.
- `version` must be SemVer-shaped (`X.Y.Z`, with optional prerelease/build).
- `runtime` must be `python`. Skill SDKs exist in TypeScript, but plugins are
  Python-only today. See `docs/plans/HERMES_COMPARISON_FOLLOWUPS.md` §P0.1.
- `entry` must be relative and must not contain `..`.
- advisory `[[tools]]` entries require non-empty `name`; `deadline_ms` must be
  non-negative.
- advisory `[[hooks]]` entries require non-empty `event`.

Unknown manifest keys are tolerated for forward compatibility. The kernel
ignores tags/UI hints unless a distro or UI chooses to use them.

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
4. environment variables declared by the plugin
5. explicit runtime or CLI arguments

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

1. Kernel loads `plugin.toml` and starts the configured runtime as a process
   group leader.
2. Kernel writes one NDJSON `register_request` message to plugin stdin.
3. Plugin replies with `register`, including `protocol_version`, `plugin_id`,
   tools, and subscriptions.
4. Kernel installs the plugin's tools/subscriptions into the unified dispatch
   and hook tables.
5. Kernel sends `tool_call`, `event`, and `shutdown` messages as needed.
6. Plugin replies with `tool_result` and `event_reply`, and may emit `send`,
   `log`, and `update_tools` messages.

If a plugin crashes, the kernel supervisor restarts it with exponential backoff
(default 1s → 30s, at most 5 restarts per 60s window). Manifest/schema/protocol
handshake failures are non-restartable.

Plugin diagnostics are available from `GET /internal/snapshot/plugins` only for
local loopback callers. The endpoint returns operational metadata including
plugin ids, status, PID, restart count, last error, registered tool names,
subscriptions, and registration time. Do not expose it through public ingress or
unauthenticated reverse proxies; deployments that need remote diagnostics should
bind the kernel to loopback and place any remote access behind a separate
authenticated private channel.

## Protocol framing and methods

The kernel↔plugin channel is NDJSON over stdio: one UTF-8 JSON object per line,
maximum 10 MiB per line. Stdout is reserved for protocol messages; stderr is
forwarded to the kernel log.

Every message has this shape:

```json
{"method":"tool_result","params":{"callId":"tc-123","result":{"ok":true}}}
```

Supported methods:

| Direction | Method | Purpose |
|-----------|--------|---------|
| kernel → plugin | `register_request` | initial config + plugin protocol version |
| plugin → kernel | `register` | active tool catalog and hook subscriptions |
| kernel → plugin | `tool_call` | invoke a plugin-registered tool |
| plugin → kernel | `tool_result` | reply to a `tool_call` |
| kernel → plugin | `event` | deliver subscribed bus/hook event |
| plugin → kernel | `event_reply` | reply to modifying/claiming events |
| plugin → kernel | `send` | emit a bus message (`channel = "bus"`) |
| plugin → kernel | `log` | structured log and metric convention |
| plugin → kernel | `update_tools` | atomically replace this plugin's tool catalog |
| kernel → plugin | `shutdown` | request graceful exit |

The plugin protocol version is separate from the WebSocket client protocol version.
The kernel advertises its supported range `[MinPluginProtocolVersion,
MaxPluginProtocolVersion]` in `register_request`; the plugin must echo a
compatible version in `register`. See `docs/PROTOCOL.md` for the negotiation
details and bump rules.

## Python SDK example

The Phase 3 reference SDK lives in `examples/plugin-sdk-python/`, and the live
reference plugin is `examples/plugin-hello/`.

```python
from tabula_plugin_sdk import PluginAPI, run


def configure(api: PluginAPI) -> None:
    @api.tool(
        "hello_ping",
        description="Return a greeting",
        schema={"type": "object", "properties": {"name": {"type": "string"}}},
        deadline_ms=5000,
    )
    def hello_ping(args, ctx):
        name = args.get("name") or "world"
        api.send("hello_plugin_event", {"name": name}, session=ctx.get("session", ""))
        api.log(
            "hello_ping_called",
            metric_name="hello_ping_total",
            metric_value=1,
            metric_kind="counter",
        )
        return f"hello, {name}"

    @api.on("before_tool_call", priority=50)
    def before_tool_call(data, _ctx):
        if data.get("tool") == "hello_ping" and data.get("input", {}).get("name") == "blocked":
            return {"action": "deny", "reason": "blocked by hello plugin"}
        return {"action": "ok"}


if __name__ == "__main__":
    run(configure)
```

The SDK currently exposes the minimal runtime surface used by the live tests:
`api.tool`, `api.on`, `api.on_shutdown`, `api.send`, `api.log`,
`api.update_tools`, and `run(configure)`. Future packaged SDK authority moves to
`tabula-bundles` during the library relocation phase.

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

`examples/plugin-hello` demonstrates this with `hello_spawn_child` and an
`api.on_shutdown` cleanup callback.

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

Run the reference plugin smoke tests:

```sh
scripts/test-plugin-hello.sh
go test ./internal/kernel/ -run 'TestPluginHello' -count=1 -timeout 30s
```

For manifest/parser and runtime-level checks:

```sh
go test ./internal/kernel/plugin/ -count=1
```
