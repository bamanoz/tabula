# Protocols

Tabula speaks three distinct wire protocols:

1. **WebSocket protocol** — kernel ↔ client (gateways, TUIs, drivers).
2. **Runtime API protocol** — kernel ↔ `tabula-runtime`.
3. **Runtime worker protocol** — `tabula-runtime` ↔ plugin worker (NDJSON over stdin/stdout).

This document is the authoritative reference for both. If wire behavior
disagrees with this file, treat it as a bug in the code or in this file —
fix one of them.

Companion code:

- WS v4 envelopes and operations: `internal/kernel/client_v4.go`
- Internal extension bus shapes and topics: `internal/kernel/message.go`, `internal/kernel/protocol.go`
- Runtime API frames: `internal/runtime/wire/types.go`
- Runtime worker frames: `internal/runtime/worker/wire/types.go`

---

## 1. WebSocket protocol (kernel ↔ client)

The kernel serves protocol v4 only. The authoritative contract, operation list,
state machine, error codes, and examples are in `docs/KERNEL_PROTOCOL_V4.md`.

Every frame is a strict JSON envelope with `v: 4`, `kind`, `op`, `id`, scoped
`tenant_id`/`session_id`, and typed `data`. The first frame is the
`connection.open` command. Authentication uses the local token from
`$TABULA_HOME/run/kernel-client-token` or `TABULA_KERNEL_TOKEN`.

Clients use durable commands and queries such as:

- `session.create`, `session.get`, `session.list`, `session.subscribe`;
- `session.archive`, `session.unarchive`, `session.delete`;
- `input.submit` and fenced turn recovery commands;
- `tool.call` with streamed and terminal typed results.

Gateways are ordinary client actors. Drivers use the separate execution API and
never receive turns through client topic capabilities. Extension traffic uses
`extension.send` and `extension.event`; it cannot perform authoritative session,
input, turn, attempt, lease, or output transitions.

---

## 2. Runtime API protocol (kernel ↔ runtime)

The kernel does not start or speak directly to plugin workers. It accepts
authenticated runtime attachments, then routes tool calls and hook events to the
attached runtime.

### Transport

Runtime API frames use JSON over a transport-independent codec. The supported
attachment transports are local unix socket, runtime WebSocket/WSS, and stdio
when a runtime is launched through the SSH backend.

### Operations

See `internal/runtime/wire/types.go` for the authoritative Go structs.

| Operation | Direction | Purpose |
|-----------|-----------|---------|
| `hello` | runtime → kernel | authenticate and preview capabilities |
| `hello_ack` | kernel → runtime | accept or reject runtime attachment |
| `invoke` | kernel → runtime | invoke one plugin tool |
| `invoke_result` | runtime → kernel | terminal invoke result |
| `invoke_result_start` / `invoke_result_delta` / `invoke_result_end` | runtime → kernel | streamed successful invoke result |
| `cancel` / `cancel_ack` | kernel ↔ runtime | cancel one in-flight call |
| `health` / `health_resp` | kernel ↔ runtime | runtime liveness |
| `list_capabilities` / `list_capabilities_resp` | kernel ↔ runtime | current plugin capability catalog |
| `reload` / `reload_ack` | kernel ↔ runtime | refresh manifests/workers |
| `prepare_tenant` / `prepare_tenant_ack` | kernel ↔ runtime | synchronously initialize tenant-visible warm plugin workers and return their authoritative capabilities |
| `hook_event` / `hook_event_reply` | kernel ↔ runtime | deliver and answer plugin hook events |
| `catalog_update` | runtime → kernel | replace one plugin target capability |
| `plugin_send` | runtime → kernel | emit a bus message |
| `plugin_log` | runtime → kernel | emit structured diagnostics |
| `lifecycle_notice` | runtime → kernel | report plugin target lifecycle state |
| `driver.ensure` / `driver.ensure_ack` | kernel ↔ runtime | converge one pinned session driver worker |
| `driver.stop` / `driver.stop_ack` | kernel ↔ runtime | stop one session driver worker |
| `driver.lifecycle` | runtime → kernel | report session driver process lifecycle |
| `driver.register` / `driver.lease_granted` | runtime ↔ kernel | register and fence one session driver |
| `driver.ready` / `driver.heartbeat` / `driver.result` | runtime ↔ kernel | maintain readiness, lease, and correlated acknowledgements |
| `turn.assign` / `turn.prepared` / `turn.prepare_failed` | kernel ↔ runtime | prepare one fenced attempt without external work |
| `turn.permit` / `turn.cancel` | kernel → runtime | grant or revoke execution authority |
| `turn.output` / terminal turn operations | runtime → kernel | commit ordered output and terminal attempt state |
| `turn.tool_call` / `turn.tool_result` | runtime ↔ kernel | dispatch a provider tool request and return its terminal result |

Driver components use `kind.name = "driver"`, `worker.mode = "warm"`, and
`worker.scope = "session"`. They cannot publish plugin tools or hooks and are
not invokable through the generic plugin API. `driver.ensure` is idempotent for
the same tenant/session/component/spec/generation tuple; a newer desired
generation stops the prior worker before replacement. Runtime restart and
reattachment rebuild desired driver workers from durable session projections.
Before every `driver.ensure`, the kernel sends `prepare_tenant` and waits for the
runtime to initialize all warm tenant-visible plugin workers. The runtime reply
contains the complete worker-authoritative capability snapshot for those warm
targets. The kernel applies that snapshot before allowing the driver to start,
so the driver's first prompt after startup or reload sees the same tool schemas
as later prompts without timing sleeps. Failed optional targets remain failed in
the snapshot and do not block unrelated ready targets; cancellation or transport
failure aborts driver startup.

Only plugin targets are invokable. Skills are instruction artifacts and do not
publish executable Runtime API capabilities.

---

## 3. Runtime worker protocol (runtime ↔ plugin worker)

### Transport

NDJSON: one JSON object per line, `\n`-terminated. There is no JSON-RPC `id`
field. Request/reply correlation uses `call_id` in the frame payload.

### Versioning

The runtime worker surface is versioned as part of the M2 Runtime API rollout.
There is currently one supported worker protocol generation: NDJSON frames with
`op` names such as `init`, `init_ack`, `call`, `result`, `event`,
`event_reply`, `send`, `log`, `tools_updated`, and `shutdown`.

### Bump rules

- Optional field on existing params type → **no bump**. Plugins must
  tolerate unknown fields. SDK-side: deserialize lenient.
- Required field on existing params type → **bump**.
- Renaming/removing a field → **bump**.
- New method → **no bump** if optional and gated by capability detection;
  **bump** if mandatory.
- Changing semantics of an existing field → **bump**.

A bump is a major change. There is no minor-version concept on the wire.
Move all SDKs forward in lockstep with the kernel bump. Until both kernel
and SDK declare support for the new version, the new version is not
negotiated.

### Operations

See `internal/runtime/worker/wire/types.go` for the authoritative Go structs.

Direction reference:

| Operation       | Direction        | Reply                |
|-----------------|------------------|----------------------|
| `init`          | runtime → worker | `init_ack`           |
| `call`          | runtime → worker | `result`             |
| `event`         | runtime → worker | `event_reply`        |
| `shutdown`      | runtime → worker | none (process exits) |
| `send`          | worker → runtime | none                 |
| `log`           | worker → runtime | none                 |
| `tools_updated` | worker → runtime | none                 |
| `tool_call`     | driver worker → runtime | correlated `tool_result` after kernel tool completion |
| `tool_result`   | runtime → driver worker | none                 |

A `shutdown` frame may include diagnostic `reason` text and optional boolean
`final`. `final = true` means the runtime process is exiting; absent or false
means the runtime remains active and is replacing or evicting the worker. A
worker that supervises detached child processes uses `final`, not `reason`, to
decide whether those children must also stop.

### Restart contract

A worker process that exits (cleanly or otherwise) is restarted by
`tabula-runtime` according to runtime policy. Malformed worker frames or failed
worker initialization surface as runtime diagnostics and target lifecycle
changes rather than kernel-managed plugin restarts.

---

## 4. Plugin manifest `[requires]`

Every `plugin.toml` **must** declare a `[requires]` block. Missing or
malformed → manifest parse error, plugin refused.

```toml
[requires]
kernel           = ">=0.9.0,<1.0.0"
protocol_version = 1                                  # int or [1, 2]
# sdk            = "tabula-plugin-sdk>=1.0.0,<2.0.0" # only for SDK-backed workers
```

Fields:

- **`kernel`** — semver range. The kernel version (`$TABULA_HOME/VERSION`
  or the running binary's `VERSION`) must satisfy this range.
- **`protocol_version`** — current worker protocol generation understood by the
  runtime and SDK. M2 currently uses `1`.
- **`sdk`** *(optional)* — `<name><range>`. Declare this only when the worker
  intentionally depends on a Tabula SDK package. When present, the installed SDK
  package version (read from `_lib/<runtime>/`) must satisfy this range. `name`
  is one of `tabula-plugin-sdk` (Python) or `@tabula/skill-sdk` (TypeScript).

The distro installer and `tabula-runtime` manifest loader enforce this block at
install/load time. Runtime worker initialization remains authoritative for the
live tool catalog.

---

## 5. SDK ↔ protocol mapping

The SDK package version is independent of the worker protocol generation, but
the installed SDK must implement the current `op`-based worker frames.

| SDK package                 | Version | Supports protocol |
|-----------------------------|---------|-------------------|
| `tabula-plugin-sdk` (Python)| `1.x`   | `1`               |

The Python SDK source lives in `tabula-bundles/extensions/plugin-sdk/sdk/python/src/tabula_plugin_sdk` and installs through bundle package exports.
Bumping the worker protocol requires updating both `tabula-runtime` and the SDK
in lockstep.

---

## 6. Lock file fields

`tools/tabula-distro/src/tabula_distro/lock.py` records install-time
versions for reproducibility. Lock schema version `3` adds:

- `kernel_version` — the kernel binary version that performed the install.
- `plugin_protocol_version` — the kernel's `MaxPluginProtocolVersion` at
  install time.
- `sdk_versions` — map runtime name → SDK package version, e.g.
  `{"python": "0.1.0", "typescript": "0.1.0"}`. Read from the SDK
  packages staged into `_lib/`.

A reinstall on a different kernel version writes a fresh lock; the
installer compares `requires.kernel` against the new kernel before
staging.

---

## 7. Changelog

- **2026-05-05**: Updated for M2 runtime-owned worker protocol and removal of
  kernel-managed plugin stdio lifecycle.
