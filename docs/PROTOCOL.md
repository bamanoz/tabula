# Protocols

Tabula speaks two distinct wire protocols:

1. **WebSocket protocol** — kernel ↔ client (gateways, TUIs, drivers).
2. **Runtime worker protocol** — `tabula-runtime` ↔ plugin worker (NDJSON over stdin/stdout).

This document is the authoritative reference for both. If wire behavior
disagrees with this file, treat it as a bug in the code or in this file —
fix one of them.

Companion code:

- WS message types and constants: `internal/kernel/protocol.go`
- Plugin stdio messages, framing, helpers: `internal/kernel/plugin/protocol.go`

---

## 1. WebSocket protocol (kernel ↔ client)

### Versioning

Single integer `ProtocolVersion` (`internal/kernel/protocol.go`). Current
version: `3`.

Clients must send `v: 3` in every client->kernel frame. The first frame must be
`hello`, and successful handshake returns `hello_ack`. Mismatch is rejected with
an `error` frame.

There is **no negotiation** on this channel. Any breaking change bumps the
integer; any client that does not match exactly is rejected. This is
intentional: the WS surface is small, the client population is in-tree
(gateways, TUIs), and we control both sides.

`ProtocolVersion` bumps:

- Renaming or removing a field on an existing message → **bump**.
- Adding a new optional field → **no bump** (clients must tolerate unknown fields).
- Adding a new message type → **no bump** if old clients can ignore it; **bump**
  otherwise.
- Changing semantics of an existing field → **bump**.

Canonical examples live in `docs/KERNEL_PROTOCOL_EXAMPLES.md`.

### Message envelope

JSON over WebSocket. Every message has a `type` field. Other fields depend
on the type. See `internal/kernel/protocol.go` for the full enum and
`validateMessage` for required-field rules.

Unknown fields on incoming messages are tolerated (forward compatibility).
Unknown `type` values are rejected with an error message but do not close
the socket.

### Kernel client `hello`

Kernel WebSocket clients must authenticate in the first `hello` frame:

```json
{
  "v": 3,
  "type": "hello",
  "data": {
    "name": "gateway-cli-main",
    "auth_token": "ktk_...",
    "send_topics": ["message.user", "turn.cancel"],
    "receive_topics": ["session.init", "stream.delta", "turn.done", "error"]
  }
}
```

`auth_token` is the local token from `$TABULA_HOME/run/kernel-client-token` or
`TABULA_KERNEL_TOKEN`. The old `token` field is not a client auth field; non-empty
values are rejected because kernel-managed spawn tokens were removed.

After `hello_ack`, clients join with `join` and then exchange domain messages as
`event`, `request`, and `reply` frames. Important topics include:

- `message.user`
- `session.init`
- `session.member_joined`
- `stream.start`, `stream.delta`, `stream.end`
- `reasoning.start`, `reasoning.delta`, `reasoning.end`
- `tool.call`, `tool.result`
- `exchange.choose`, `exchange.approve`
- `usage.update`
- `turn.done`, `turn.cancel`

`hook_reply` frames are valid only for the client or runtime subscriber that
received the matching `hook` frame.

---

## 2. Plugin stdio protocol (kernel ↔ plugin)

### Transport

NDJSON: one JSON object per line, `\n`-terminated. Per-line cap: `MaxLineSize`
= 10 MiB (`internal/kernel/plugin/protocol.go`). Blank lines are skipped.
Lines exceeding the cap return `ErrLineTooLong`.

Malformed lines do not kill the plugin immediately. The kernel logs a
warning and tolerates up to `malformedThreshold` (3) malformed lines within
`malformedWindow` (10s). Past that, the plugin process is terminated with a
non-restartable error.

There is no JSON-RPC `id` field. Request/reply correlation uses `callId`
inside the params payload. See `Message` and methods in
`internal/runtime/worker/wire`.

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

### Restart contract

A worker process that exits (cleanly or otherwise) is restarted by
`tabula-runtime` according to runtime policy. Malformed worker frames or failed
worker initialization surface as runtime diagnostics and target lifecycle
changes rather than kernel-managed plugin restarts.

---

## 3. Plugin manifest `[requires]`

Every `plugin.toml` **must** declare a `[requires]` block. Missing or
malformed → manifest parse error, plugin refused.

```toml
[requires]
kernel           = ">=0.9.0,<1.0.0"
protocol_version = 1                                  # int or [1, 2]
sdk              = "tabula-plugin-sdk>=1.0.0,<2.0.0"
```

Fields:

- **`kernel`** — semver range. The kernel version (`$TABULA_HOME/VERSION`
  or the running binary's `VERSION`) must satisfy this range.
- **`protocol_version`** — current worker protocol generation understood by the
  runtime and SDK. M2 currently uses `1`.
- **`sdk`** — `<name><range>`. The installed SDK package version (read
  from `_lib/<runtime>/`) must satisfy this range. `name` is one of
  `tabula-plugin-sdk` (Python) or `@tabula/skill-sdk` (TypeScript).

The distro installer and `tabula-runtime` manifest loader enforce this block at
install/load time. Runtime worker initialization remains authoritative for the
live tool catalog.

---

## 4. SDK ↔ protocol mapping

The SDK package version is independent of the worker protocol generation, but
the installed SDK must implement the current `op`-based worker frames.

| SDK package                 | Version | Supports protocol |
|-----------------------------|---------|-------------------|
| `tabula-plugin-sdk` (Python)| `1.x`   | `1`               |

The Python SDK surface lives in `tabula-bundles/_lib/python/src/tabula_plugin_sdk`.
Bumping the worker protocol requires updating both `tabula-runtime` and the SDK
in lockstep.

---

## 5. Lock file fields

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

## 6. Changelog

- **2026-05-05**: Updated for M2 runtime-owned worker protocol and removal of
  kernel-managed plugin stdio lifecycle.
