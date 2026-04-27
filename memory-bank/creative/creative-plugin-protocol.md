# Creative Phase: Plugin Protocol (D0.1 Freeze)

Status: **FROZEN** 2026-04-26 — BUILD may proceed.
Scope: kernel ↔ plugin stdio JSON-RPC channel (separate from `kernel.ProtocolVersion` wire protocol).

## 1. PROBLEM DEFINITION

Freeze the complete kernel↔plugin protocol so Phase 2 BUILD (PluginRuntime) and
Phase 3 (reference plugin) can implement against an unambiguous spec. Must
cover happy path AND negative paths; must accommodate MCP plugin requirements
(dynamic tool discovery); must define version handshake; must specify timeouts
and crash recovery.

Constraints:
- Language-agnostic (Python first, TS later).
- Co-existence with kernel's `ProtocolVersion=1` (WebSocket wire protocol) — no conflation.
- No changes to existing kernel WebSocket protocol.
- Minimum surface: extend later, not now.

## 2. DECISIONS

### 2.1 Stdio Framing — **NDJSON (newline-delimited JSON)** [D0.1]

**Decision**: One JSON object per line, `\n`-terminated, UTF-8.

**Options considered**:
- (A) NDJSON — one message per line.
- (B) LSP-style `Content-Length: N\r\n\r\n<body>` framing.

**Justification**:
- NDJSON is trivially debuggable (`cat`, `tee` work).
- Matches the de-facto convention in Claude/Anthropic agent ecosystem.
- Plugins can be written in shell with `jq` if needed.
- Content-Length only wins for binary embedded payloads, which we do not need (binary goes through `send` channel side-band, not RPC body).
- Simpler parser: 1 line = 1 message.

**Rules**:
- Every message MUST be valid UTF-8 JSON object on a single line, terminated with `\n`.
- Lines longer than 10 MiB are rejected as malformed.
- Embedded `\n` inside string values must be escaped as `\\n` (standard JSON).
- A blank line is ignored (allows pretty-prints to be flushed without crashing).
- `stdout` is the protocol channel; `stderr` is free-form plugin diagnostic output (kernel forwards to its logger at INFO level, never parses).

### 2.2 Plugin Protocol Version — **`PluginProtocolVersion = 1`** [D2.13]

**Decision**: Add new constant `PluginProtocolVersion = 1` in
`internal/kernel/protocol.go`, distinct from existing `ProtocolVersion = 1`.

```go
// PluginProtocolVersion is the kernel↔plugin stdio JSON-RPC protocol version.
// Independent SemVer from ProtocolVersion (WebSocket wire protocol).
const PluginProtocolVersion = 1
```

**Handshake**: kernel sends `PluginProtocolVersion` in the `register`-request
preamble; plugin echoes its supported version in the `register`-reply. Mismatch
→ kernel logs error and refuses to start the plugin.

**Bump policy**:
- Bump (breaking change): adding a required field to an existing message; removing a method; changing semantics of an existing field.
- Adding a new optional field or a new method (e.g. `update_tools`): no bump.
- Kernel MUST refuse to start a plugin whose declared version > kernel's `PluginProtocolVersion`.
- Kernel MAY support N-1 plugin protocol version for one release cycle (best effort), declared via `min_supported_plugin_version` constant if/when needed (deferred until v2 ships).

### 2.3 Lifecycle State Machine

```
[spawned] → kernel writes "register_request" → plugin replies "register" → [registered]
[registered] → kernel may send tool_call / event → plugin replies tool_result / event_reply
[any] → kernel writes "shutdown" → plugin must exit within ShutdownGracePeriod (3s) → [exited]
[any] → plugin EOF (stdin closed) → plugin should exit voluntarily within 1s → kernel SIGTERMs PG → SIGKILLs after 3s
```

**`register_request` (kernel → plugin)** is sent immediately after spawn:
```json
{"method":"register_request","params":{"protocol_version":1,"plugin_id":"mcp","config":{...}}}
```

This delivers `config` (resolves D0.1 config-delivery question — see §2.4)
and the kernel's protocol version in one message, before the plugin commits
to its tools/subscriptions list.

### 2.4 Config Delivery — **register_request payload** (not env vars)

**Decision**: kernel sends `config` (merged plugin defaults + user override)
inside the first `register_request` message. Env vars carry only minimal
bootstrap information:

| Env var | Purpose |
|---------|---------|
| `TABULA_PLUGIN_ID` | plugin id (also in payload, redundant for human debugging) |
| `TABULA_PLUGIN_PROTOCOL_VERSION` | mirror of `PluginProtocolVersion` for SDK pre-handshake checks |

**Justification**: env vars have OS limits (typically 128 KiB on Linux) and
poor encoding for nested structures; plugin SDK can dispatch on
`register_request` cleanly without parsing env JSON.

### 2.5 Message Schema (full)

All messages have the shape `{"method": "<name>", "params": {...}}`. No
JSON-RPC `id` field — request/response correlation uses `callId` inside
`params` for `tool_call`/`event` flows.

#### Kernel → Plugin

**`register_request`** (sent once, at spawn):
```json
{
  "method": "register_request",
  "params": {
    "protocol_version": 1,
    "plugin_id": "mcp",
    "config": {...}
  }
}
```

**`tool_call`** (per LLM tool invocation):
```json
{
  "method": "tool_call",
  "params": {
    "callId": "tc-xxx",
    "name": "git_status",
    "args": {...},
    "session": "sess-abc",
    "deadline_ms": 30000
  }
}
```

**`event`** (per bus event the plugin subscribes to):
```json
{
  "method": "event",
  "params": {
    "callId": "h-xxx",
    "event": "before_tool_call",
    "data": {...},
    "session": "sess-abc"
  }
}
```

`callId` in `event` is required only when the event uses a `modifying` /
`claiming` strategy (kernel needs `event_reply`); for `void` events `callId`
is omitted.

**`shutdown`** (graceful stop):
```json
{"method":"shutdown","params":{}}
```

#### Plugin → Kernel

**`register`** (reply to `register_request`):
```json
{
  "method": "register",
  "params": {
    "protocol_version": 1,
    "plugin_id": "mcp",
    "tools": [
      {
        "name": "git_status",
        "description": "...",
        "schema": {...},
        "deadline_ms": 30000
      }
    ],
    "subscriptions": [
      {"event":"before_tool_call","priority":80,"timeout_ms":null}
    ]
  }
}
```

`tools[]` and `subscriptions[]` may be empty (`[]`). `deadline_ms` per tool
is optional; default = 30000 (see §2.7).

**`tool_result`** (reply to a `tool_call`):
```json
{
  "method": "tool_result",
  "params": {
    "callId": "tc-xxx",
    "result": {...}        // OR
    "error":  "string"     // mutually exclusive with result
  }
}
```

**`event_reply`** (reply to a `modifying`/`claiming` event):
```json
{
  "method": "event_reply",
  "params": {
    "callId": "h-xxx",
    "action": "ok" | "rewrite" | "deny" | "claim",
    "data": {...},        // for "rewrite"/"claim"; merged onto event payload
    "reason": "string"    // for "deny"
  }
}
```

The `action` values map directly to existing kernel hook actions:
`ok`→`pass`, `rewrite`→`modify`, `deny`→`block`, `claim`→`claim`.

**`send`** (plugin emits a bus event):
```json
{
  "method": "send",
  "params": {
    "channel": "bus",
    "type": "...",
    "payload": {...},
    "session": "sess-abc"   // optional; null = global
  }
}
```

**`log`** (structured log + metric convention):
```json
{
  "method": "log",
  "params": {
    "level": "info" | "warn" | "error" | "debug",
    "msg": "string",
    "fields": {...}
  }
}
```

**Metric convention** (no separate `metric` method, per D2.12 ratification):
fields object MAY contain `metric_name`, `metric_value`, `metric_kind` (one of
`counter`, `gauge`, `histogram`). Telemetry-aggregating plugins (e.g.
`telemetry-otel`) subscribe to the log stream via the bus and
re-export. Adding a dedicated `metric` method is deferred until a concrete
volume / latency requirement appears.

**`update_tools`** (incremental tool catalog updates, post-register) [D2.10]:
```json
{
  "method": "update_tools",
  "params": {
    "tools": [
      {"name":"mcp__server__foo","description":"...","schema":{...},"deadline_ms":60000}
    ],
    "removed": ["mcp__server__old"]
  }
}
```

Semantics: `tools` is the **full new authoritative list** of currently-active
tools for this plugin. `removed` is an optional convenience list for kernel
diff/log clarity. Kernel atomically replaces this plugin's tool set in its
dispatch map. No reply from kernel. Plugin may call `update_tools` any
number of times after `register`. Tool name collisions across plugins are
resolved last-writer-wins with a WARN log (collisions are a configuration
bug, not a runtime concern).

**Tool naming**: kernel does NOT enforce any namespace prefix. Plugins are
free to use `mcp__server__name` or any other convention. UI grouping is a UI
concern, not a kernel concern.

### 2.6 Error Semantics [D2.11]

| Negative path | Kernel behaviour |
|---------------|------------------|
| Malformed JSON line from plugin | Log WARN with first 256 bytes; drop line; continue. After 3 consecutive malformed lines within 10s → SIGTERM plugin, restart per crash-recovery policy. |
| `tool_result` not received before deadline | Synthesize `tool_result {error: "plugin timeout after Nms"}` → return to LLM. Plugin is NOT killed; the `callId` is removed from pending map. If the late reply arrives, log WARN and drop. |
| `event_reply` not received before timeout | For `modifying`+`HookSecurity` events: block (fail-closed), per existing `dispatchModifying` semantics. For `modifying`+`HookDomain`: pass through (fail-open). For `claiming`: continue iteration. Identical to existing Client hook semantics — single code path via `HookSubscriber` (D2.16). |
| Plugin process EOF / crash mid-call | All pending `tool_call` callIds fail with `{error: "plugin crashed"}`; pending event callIds are released (fail-closed for security, fail-open for domain). Supervisor restarts per policy (§2.8). |
| Plugin sends `tool_result` for unknown `callId` | Log WARN; drop. |
| Plugin sends `event_reply` for unknown `callId` | Log WARN; drop. |
| `register` schema validation fails (missing required field, bad type) | Log ERROR with details; SIGTERM plugin; mark plugin as `failed`; do NOT auto-restart (configuration error, not transient). Kernel boot continues with other plugins. |
| Plugin declares protocol_version > kernel's | Log ERROR; SIGTERM; do NOT auto-restart. |
| `register` not received within 10s of spawn | Log ERROR; SIGTERM; treat as crash for restart policy. |
| `update_tools` with malformed schema | Log WARN with details; reject the update (keep prior tool set); continue (do NOT kill plugin). |
| `send` with unknown channel | Log WARN; drop. |

### 2.7 Tool Call Deadline

- Default: **30000 ms** (30 s).
- Per-tool override: `deadline_ms` field in `register.tools[]`.
- Per-call override: kernel includes the resolved deadline in `tool_call.deadline_ms` so the plugin sees what the kernel will enforce.
- Long-running tools (MCP filesystem walks, etc.) MUST declare an appropriate `deadline_ms` in their tool spec; kernel does not silently extend it.
- Maximum allowed `deadline_ms` value: 600000 (10 min). Larger values are clamped with WARN log.

### 2.8 Crash Recovery Policy

- **Backoff**: exponential, starting 1s, doubling each restart, capped at 30s.
- **Max restarts**: 5 within a 60s rolling window. After the 5th crash, plugin is marked `failed` and not restarted automatically. Operator must restart kernel or manually re-register.
- **Reset**: counter resets after the plugin runs cleanly for ≥120s.
- **Restart triggers**: process exit (any code), EOF on stdin/stdout pipe, repeated malformed-message threshold (§2.6).
- **No restart**: register-time failures (schema validation, version mismatch, register-timeout); shutdown initiated by kernel.

### 2.9 Child-PG Ownership (Two-Tier Supervision)

- Kernel spawns the plugin process as a **process group leader** (`setsid` or `Setpgid: true` in `SysProcAttr`); kernel records the PG id.
- On `shutdown` request: kernel sends `shutdown` JSON-RPC, waits up to `ShutdownTimeout` (3s), then `killpg(pgid, SIGTERM)`, waits 3s more, then `killpg(pgid, SIGKILL)`.
- The plugin SDK is responsible for:
  - calling `setpgid(0, 0)` only if it inherits a different PG from kernel (not needed since kernel sets it at spawn);
  - trapping SIGTERM and forwarding `killpg(0, SIGTERM)` to its own children before exiting;
  - this pattern is documented in `PLUGIN_AUTHORING.md`.
- Kernel does NOT track plugin grandchildren.

## 3. ACCEPTANCE CRITERIA (BUILD checklist)

- [ ] `internal/kernel/protocol.go` declares `PluginProtocolVersion = 1`.
- [ ] PluginRuntime implements NDJSON line reader/writer with 10 MiB line guard.
- [ ] All 9 message types (`register_request`, `register`, `tool_call`, `tool_result`, `event`, `event_reply`, `send`, `log`, `shutdown`, plus optional `update_tools`) round-trip in unit tests.
- [ ] Negative-path matrix (§2.6) covered by unit tests.
- [ ] Reference plugin (`examples/plugin-hello`) exercises every message type at least once.
- [ ] Crash recovery policy (§2.8) covered by an integration test (kill plugin, observe restart with backoff).

## Rubric Review

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    surface_minimality: 8
    composability: 8
    invariant_strength: 9
    migration_safety: 8
    debuggability: 9
  ai_slop_flags: none
  verdict: PASS
  notes: NDJSON + register_request preamble is the lowest-surface design that still cleanly resolves config delivery, version handshake, and MCP dynamic tool catalog (via update_tools). All negative paths explicit.
```
