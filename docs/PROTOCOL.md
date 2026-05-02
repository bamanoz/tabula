# Protocols

Tabula speaks two distinct wire protocols:

1. **WebSocket protocol** — kernel ↔ client (gateways, TUIs, drivers).
2. **Plugin stdio protocol** — kernel ↔ plugin subprocess (NDJSON over stdin/stdout).

This document is the authoritative reference for both. If wire behavior
disagrees with this file, treat it as a bug in the code or in this file —
fix one of them.

Companion code:

- WS message types and constants: `internal/kernel/protocol.go`
- Plugin stdio messages, framing, helpers: `internal/kernel/plugin/protocol.go`

---

## 1. WebSocket protocol (kernel ↔ client)

### Versioning

Single integer `ProtocolVersion` (`internal/kernel/protocol.go`). The kernel
advertises its version in every `connected` message. Clients send their
version in `connect`. Mismatch is **rejected**: the kernel replies with an
error message and closes the socket.

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

Current version: `1`.

### Message envelope

JSON over WebSocket. Every message has a `type` field. Other fields depend
on the type. See `internal/kernel/protocol.go` for the full enum and
`validateMessage` for required-field rules.

Unknown fields on incoming messages are tolerated (forward compatibility).
Unknown `type` values are rejected with an error message but do not close
the socket.

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
`internal/kernel/plugin/protocol.go`.

### Versioning

The plugin stdio surface is wider than WS, plugin authors live outside this
repo, and SDK upgrades happen out-of-band. Therefore we **negotiate** the
version per handshake.

Each side declares what it supports:

- **Kernel** declares an inclusive range `[MinPluginProtocolVersion,
  MaxPluginProtocolVersion]` (constants in `internal/kernel/protocol.go`).
- **Plugin SDK** declares a tuple `SUPPORTED_PROTOCOL_VERSIONS` (e.g.
  `(1,)` in `tabula-plugin-sdk`).

The handshake works as follows:

1. Kernel spawns the plugin and sends `register_request` with all three
   integers: `min_protocol_version`, `max_protocol_version`, and (for
   backward compatibility with v1-only SDKs) `protocol_version` set equal
   to `max_protocol_version`.
2. Plugin computes the intersection of `SUPPORTED_PROTOCOL_VERSIONS` and
   `[min, max]`. Picks the **maximum** value in the intersection.
3. Plugin replies with `register` carrying the chosen `protocol_version`.
4. Kernel verifies the chosen value is within its own range. Out of range
   → non-restartable error, plugin terminated.
5. Empty intersection on the plugin side → plugin writes a structured
   error log and exits with non-zero status. Kernel observes EOF before
   `register` and reports a register failure.

This is an integer-only negotiation. There is no per-feature capability
flag. If we need finer granularity later, add capability bits inside the
chosen version, do not extend the negotiation.

Current versions:

- `MinPluginProtocolVersion = 1`
- `MaxPluginProtocolVersion = 1`

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

### Methods

See the `Method*` constants in `internal/kernel/plugin/protocol.go`. The
authoritative param types are the Go structs in the same file.

Direction reference:

| Method            | Direction        | Reply              |
|-------------------|------------------|--------------------|
| `register_request`| kernel → plugin  | `register`         |
| `tool_call`       | kernel → plugin  | `tool_result`      |
| `event`           | kernel → plugin  | `event_reply`      |
| `shutdown`        | kernel → plugin  | none (process exits) |
| `send`            | plugin → kernel  | none               |
| `log`             | plugin → kernel  | none               |
| `update_tools`    | plugin → kernel  | none               |

### Restart contract

A plugin process that exits (cleanly or otherwise) is restarted by the
kernel unless the failure is marked **non-restartable**. Non-restartable
failures include:

- Manifest parse/validation errors.
- Protocol version negotiation failure (out of range).
- Malformed `register` reply.
- Malformed-line threshold exceeded post-register.

Non-restartable errors are logged and the plugin is left dead until the
operator intervenes (reload, restart kernel, or fix the plugin).

---

## 3. Plugin manifest `[requires]`

Every `plugin.toml` **must** declare a `[requires]` block. Missing or
malformed → manifest parse error, plugin refused.

```toml
[requires]
kernel           = ">=0.9.0,<1.0.0"
protocol_version = 1                                  # int or [1, 2]
sdk              = "tabula-plugin-sdk>=0.1.0,<0.2.0"
```

Fields:

- **`kernel`** — semver range. The kernel version (`$TABULA_HOME/VERSION`
  or the running binary's `VERSION`) must satisfy this range.
- **`protocol_version`** — integer or array of integers. The plugin
  declares which stdio protocol versions it can speak. The kernel's
  `[Min, Max]` range must intersect with this set.
- **`sdk`** — `<name><range>`. The installed SDK package version (read
  from `_lib/<runtime>/`) must satisfy this range. `name` is one of
  `tabula-plugin-sdk` (Python) or `@tabula/skill-sdk` (TypeScript).

The distro installer enforces all three at install time and fails hard
on mismatch. The kernel re-validates `kernel` and `protocol_version`
at spawn time as a defense in depth (in case manifest was modified
post-install).

---

## 4. SDK ↔ protocol mapping

The SDK package version is independent of the protocol version, but each
SDK release declares which protocol versions it can speak via
`SUPPORTED_PROTOCOL_VERSIONS`.

| SDK package                 | Version | Supports protocol |
|-----------------------------|---------|-------------------|
| `tabula-plugin-sdk` (Python)| `0.1.x` | `1`               |
| `@tabula/skill-sdk` (TS)    | `0.1.x` | `1`               |

Both constants live in the SDK source:

- Python: `tabula_plugin_sdk.protocol.SUPPORTED_PROTOCOL_VERSIONS`
- TypeScript: `@tabula/skill-sdk` exports `SUPPORTED_PROTOCOL_VERSIONS`

Bumping `MaxPluginProtocolVersion` requires shipping an SDK release that
adds the new version to `SUPPORTED_PROTOCOL_VERSIONS`. Until that
release is the one installed in `_lib/`, the kernel will negotiate down
to the older version.

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

- **2026-05-02**: Document created. Codifies WS v1, plugin stdio v1,
  negotiation contract, `[requires]` block, SDK mapping, lock schema v3.
