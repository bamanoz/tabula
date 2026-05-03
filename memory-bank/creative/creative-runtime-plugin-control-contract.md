# Creative Phase: Runtime Plugin Control Contract

## 1. PROBLEM DEFINITION
- What needs to be designed: the M2-07 Runtime API and worker-protocol extension that lets runtime-hosted plugins preserve dynamic catalog, hook, send, log, and lifecycle behavior after kernel stdio plugin deletion.
- Constraints:
  - Preserve the accepted typed Runtime API envelope in `internal/runtime/wire/`; this is a companion contract, not a replacement for `creative-runtime-api-contract.md`.
  - Keep one kernel↔runtime connection per attached runtime; do not add a second kernel-side plugin transport and do not let the kernel spawn plugin workers.
  - Preserve current M2-required plugin behavior from companion evidence: dynamic `update_tools`, runtime hook registration/results, plugin bus `send`, lifecycle visibility, and gateway/plugin diagnostics.
  - Keep kernel generic: runtime owns worker processes, manifests, and worker protocol adaptation; kernel owns tool catalog, hook routing, status/read models, and policy decisions.
  - M2-08 status/readiness must work without kernel plugin process ownership or stale plugin PID assumptions.
  - No backward-compatibility shim is needed; worker protocol may break to a new major SDK contract.
- Success criteria:
  - BUILD can implement `internal/runtime/wire`, `internal/runtime/conn`, `internal/runtime/worker/wire`, runtime target management, and runtime-backed kernel adapters without schema ambiguity.
  - The contract supports migrated MCP dynamic tools, dynamic-tools fixture updates/removals, hook mutator/blocker/recorder behavior, and gateway lifecycle/log evidence.
  - `tabula status --json` can report attached runtime and runtime-hosted target readiness without reviving `plugin.Handle` process ownership.
- Non-functional requirements:
  - Hook and catalog correctness must fail closed where dropping or corrupting frames would create unsafe or stale kernel state.
  - Async frame handling must not silently ignore supported frames.
  - Backpressure behavior must be explicit per frame type.

## 2. OPTIONS

### Option A: Single RuntimeConn with callback sink ownership
- Description: keep one Runtime API connection and extend `RuntimeConn`/`conn.Conn` with a Hub-owned async sink for runtime-originated frames (`catalog_update`, `hook_event_reply`, `plugin_send`, `plugin_log`, `lifecycle_notice`). Kernel-originated hook delivery uses a dedicated `SendHookEvent` write path on the same connection.
- Architecture: `internal/runtime/conn` remains the single read loop and demultiplexer; kernel adapters implement fast sink callbacks; runtime daemon adapts worker frames to Runtime API async frames.
- Advantages:
  - Smallest change from the accepted M1 contract.
  - Preserves one authenticated transport/session per runtime.
  - Avoids pushing queue ownership and overflow semantics into every caller.
  - Makes fatal vs best-effort async behavior explicit in one place.
- Disadvantages:
  - Sink callbacks must be fast and carefully classified to avoid stalling the read loop.
  - `RuntimeConn` grows more responsibility than pure request/response.
- Risk factors:
  - Poor sink implementations could block all frame processing; BUILD must keep sink work O(1) and move expensive work behind kernel-owned queues if ever needed.

### Option B: Single RuntimeConn with caller-consumed event channel
- Description: keep one connection but expose `Events() <-chan RuntimeEvent` and require a supervisor/Hub loop to consume catalog/hook/send/log/lifecycle events.
- Architecture: `conn.Conn` owns bounded/unbounded event queues; higher layers decide routing and backpressure.
- Advantages:
  - Cleaner separation between transport and business logic.
  - Easier to unit-test transport routing in isolation.
- Disadvantages:
  - Forces queue sizing and overflow policy into every caller.
  - Makes lossless hook/catalog handling harder because a slow consumer becomes a transport concern.
  - Risks deadletter or dropped-event behavior at exactly the point the cutover needs precise semantics.
- Risk factors:
  - Unbounded channels risk memory growth; bounded channels force overflow decisions that are unsafe for catalog/hook frames.

### Option C: Separate control connection for async plugin-control traffic
- Description: keep the current request/response RuntimeConn and add a second control connection for catalog/hook/send/log/lifecycle frames.
- Architecture: local runtime child opens or reuses another logical channel, with separate auth/attach and reconnect handling.
- Advantages:
  - Strong transport separation between invoke traffic and control traffic.
  - Backpressure on one lane cannot directly block the other.
- Disadvantages:
  - Violates the current one-runtime/one-connection simplicity.
  - Doubles attach/reconnect/auth state and makes status/debugging harder.
  - Adds a larger surface just before the atomic M2-07 deletion gate.
- Risk factors:
  - Reconnect skew between invoke and control channels could leave dispatch and catalog state inconsistent.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 3 | 2 |
| Performance | 3 | 5 | 4 | 3 |
| Maintainability | 5 | 5 | 3 | 2 |
| Scalability | 4 | 5 | 3 | 2 |
| Security | 5 | 5 | 4 | 2 |
| **Weighted Total** | | **97** | 68 | 43 |

## 4. DECISION
**Selected: Option A — single RuntimeConn with callback sink ownership.**

Justification: the callback-sink approach preserves the already-accepted single Runtime API transport while solving the concrete M2-07 gap: runtime-originated async frames must no longer be dropped by `conn.Conn.readLoop`, and kernel-originated hook events need a first-class path that is not overloaded onto `Invoke`. It keeps runtime as the only worker/process host, keeps kernel generic, and gives BUILD one precise place to enforce lossless catalog/hook semantics and best-effort log semantics.

Trade-offs accepted:
- `RuntimeConn` is no longer purely RPC-style; it becomes a mixed request/response + async control connection.
- Sink implementations must stay cheap and deterministic; expensive fan-out work belongs behind Hub-owned structures, not in transport code.
- Worker protocol must intentionally break to an op-discriminated major-version shape.

## 5. IMPLEMENTATION GUIDELINES

### 5.1 Base contract boundary
- Keep `creative-runtime-api-contract.md` as the accepted base contract for envelope/error principles.
- Add a companion extension only for plugin-control semantics.
- `Hello.Capabilities` and `ListCapabilitiesResp.Targets` must upgrade from `[]string` tool names to the richer catalog model below, but they remain snapshot views. Runtime-originated `catalog_update` is the authoritative mutation path after attach/reload.

### 5.2 Canonical capability model

```json
{
  "target": {"kind": "plugin", "id": "base-mcp"},
  "tools": [
    {
      "name": "mcp_add_server",
      "description": "Add an MCP server",
      "schema": {"type": "object"},
      "deadline_ms": 30000
    }
  ],
  "hooks": [
    {
      "event": "before_tool_call",
      "priority": 100,
      "timeout_ms": 5000
    }
  ]
}
```

- `ToolSpec`
  - `name` string, required, unique per target.
  - `description` string, optional.
  - `schema` JSON object/boolean schema, optional but preserved exactly when provided.
  - `deadline_ms` integer, optional, `>= 0`; `0` means use kernel default.
- `HookSpec`
  - `event` string, required.
  - `priority` integer, optional, default `0`.
  - `timeout_ms` nullable integer, optional; `null` = kernel default, `0` = wait indefinitely, `>0` = explicit timeout.
- `Capability`
  - `target` required.
  - `tools` authoritative tool specs for that target.
  - `hooks` authoritative hook subscriptions for that target.

### 5.3 Runtime API async operation roster and directionality

#### `catalog_update` — runtime → kernel, async, lossless

```json
{
  "op": "catalog_update",
  "target": {"kind": "plugin", "id": "base-mcp"},
  "tools": [{"name": "mcp__weather__forecast", "schema": {"type": "object"}}],
  "removed": ["mcp__weather__old_forecast"],
  "hooks": [{"event": "before_tool_call", "priority": 100}],
  "revision": 3,
  "source": "update_tools"
}
```

- Required fields: `op`, `target`, `tools`, `hooks`, `revision`.
- Optional fields: `removed`, `source`.
- `revision` is a positive per-target monotonic integer within one live runtime attachment.
- `source` is diagnostic only; allowed values: `init`, `update_tools`, `reload`.
- Semantics:
  - `tools` is the full new authoritative tool set.
  - `removed` is advisory only for logging/diff clarity.
  - `hooks` is the full authoritative hook set currently active for the target.
  - Kernel validates tool/hook metadata, replaces runtime-backed tool dispatch entries for that target, and rebuilds runtime-backed hook subscribers.

#### `hook_event` — kernel → runtime, async request on the same connection

```json
{
  "op": "hook_event",
  "call_id": "hk_01J...",
  "target": {"kind": "plugin", "id": "hook-blocker"},
  "event": "before_tool_call",
  "reply_mode": "modifying",
  "session": "sess_123",
  "data": {"tool": "fs_read", "id": "toolu_1"}
}
```

- Required fields: `op`, `target`, `event`, `reply_mode`.
- `call_id` is required when `reply_mode != "none"`; omitted for fire-and-forget observability hooks.
- `reply_mode` allowed values: `none`, `modifying`, `claiming`.
- `data` is raw JSON payload as currently emitted by kernel hook dispatch.
- `SendHookEvent(ctx, HookEvent)` or equivalent dedicated write path is required; do not overload `Invoke`.

#### `hook_event_reply` — runtime → kernel, correlated, lossless until hook timeout

```json
{
  "op": "hook_event_reply",
  "call_id": "hk_01J...",
  "action": "rewrite",
  "data": {"tool": "fs_read", "path": "/safe/path"},
  "reason": "rewrote to tenant-safe path"
}
```

- Required fields: `op`, `call_id`, `action`.
- `action` allowed values: `ok`, `rewrite`, `deny`, `claim`.
- Kernel maps these to existing hook-engine actions: `ok → pass`, `rewrite → modify`, `deny → block`, `claim → claim`.
- Unknown or expired `call_id` after timeout/cancel is dropped with warning, not fatal.

#### `plugin_send` — runtime → kernel, async, required for M2-07

```json
{
  "op": "plugin_send",
  "target": {"kind": "plugin", "id": "observer"},
  "channel": "bus",
  "type": "observer_event",
  "session": "sess_123",
  "payload": {"message": "hello"}
}
```

- Required fields: `op`, `target`, `channel`, `type`.
- `channel` is currently a closed enum with only `bus` allowed.
- Delivery is lossless to the kernel boundary; after kernel acceptance, downstream session fan-out follows existing bus behavior.

#### `plugin_log` — runtime → kernel, async, required for M2-07 but best-effort

```json
{
  "op": "plugin_log",
  "target": {"kind": "plugin", "id": "gateway-telegram"},
  "level": "info",
  "msg": "worker started",
  "fields": {"mode": "polling"}
}
```

- Required fields: `op`, `target`, `level`, `msg`.
- `level` allowed values: `debug`, `info`, `warn`, `error`.
- `fields` is an optional JSON object. Sensitive values must be sanitized before emission.
- These frames never use Runtime API wire error codes.

#### `lifecycle_notice` — runtime → kernel, async, lossless latest-state diagnostic

```json
{
  "op": "lifecycle_notice",
  "target": {"kind": "plugin", "id": "gateway-telegram"},
  "state": "ready",
  "pid": 42421,
  "message": "worker initialized"
}
```

- Required fields: `op`, `target`, `state`.
- Optional fields: `pid`, `message`.
- `state` allowed values: `starting`, `ready`, `stopping`, `exited`, `crashed`.
- `pid` is diagnostic only and does not restore kernel process ownership.

### 5.4 RuntimeConn ownership and sink contract
- Choose callback sink ownership, not a caller-consumed event channel.
- `conn.Conn.readLoop` must route both response frames and async frames; supported async frames may never be silently ignored.
- The sink contract should be kernel-facing and explicit, e.g. one callback per async frame type plus a protocol-error callback.
- Sink rules:
  - `catalog_update`, `hook_event_reply`, `plugin_send`, `lifecycle_notice`: handled synchronously and must return quickly; any expensive fan-out happens after a fast in-memory state update.
  - `plugin_log`: may be downgraded to fire-and-forget logging with rate-limited drop warnings.
- Unknown async Runtime API op or wrong-direction frame is `protocol_error` and detaches the runtime.

### 5.5 Worker protocol envelope and demultiplexing
- Replace the current shape that assumes the caller already knows the next concrete frame.
- Every worker frame gets an `op` discriminator; the runtime owns one single-reader router per worker process.
- Canonical worker ops:
  - runtime → worker: `init`, `call`, `event`, `shutdown`
  - worker → runtime: `init_ack`, `result`, `event_reply`, `tools_updated`, `send`, `log`, `error`
- `call_id` is a shared correlation namespace across `call`/`result` and `event`/`event_reply`; duplicate in-flight ids are protocol-fatal.

#### Worker frame shapes

```json
{"op":"init","kernel_id":"main","tenant_id":"default","target_id":"base-mcp","manifest":{},"env":{}}
{"op":"init_ack","ready":true,"tools":[{"name":"mcp_add_server"}],"subscriptions":[{"event":"before_tool_call","priority":100}]}
{"op":"call","call_id":"toolu_1","tool":"mcp_add_server","args":{"name":"weather"}}
{"op":"result","call_id":"toolu_1","ok":true,"data":{"status":"ok"}}
{"op":"event","call_id":"hk_1","event":"before_tool_call","reply_mode":"modifying","session":"sess_123","data":{"tool":"fs_read"}}
{"op":"event_reply","call_id":"hk_1","action":"deny","reason":"blocked"}
{"op":"tools_updated","revision":2,"tools":[{"name":"mcp__weather__forecast"}],"removed":["mcp__weather__old_forecast"]}
{"op":"send","channel":"bus","type":"observer_event","session":"sess_123","payload":{"message":"hi"}}
{"op":"log","level":"info","msg":"server added","fields":{"server":"weather"}}
{"op":"shutdown","reason":"reload"}
{"op":"error","error":{"code":"protocol_error","message":"unknown op"}}
```

- `init_ack`
  - Expands startup registration instead of adding a separate `register` frame.
  - Required fields when `ready=true`: `tools`, `subscriptions`.
  - When `ready=false`, `error` is required.
- `tools_updated`
  - Required fields: `revision`, `tools`.
  - `removed` is advisory only.
  - `subscriptions` do not change here; initial subscriptions come from `init_ack` and remain fixed for M2/M3 unless a later milestone adds explicit hook-subscription updates.
- No dedicated worker lifecycle op is added.
  - Worker lifecycle is derived by the runtime from process spawn, `init_ack`, `shutdown`, `error`, EOF, and exit status.
  - Runtime translates that state into Runtime API `lifecycle_notice` for the kernel.

### 5.6 Backpressure, loss, and fatality rules

| Frame | Ordered | Loss policy | Fatal on malformed? | Notes |
|-------|---------|-------------|---------------------|-------|
| `catalog_update` | yes, per target | lossless | yes | Stale or invalid catalog is unsafe; reject and detach. |
| `hook_event` | yes | lossless send or runtime_unavailable | yes on encode/route bug | Kernel timeout semantics still decide fail-open/fail-closed by hook type. |
| `hook_event_reply` | yes | lossless until kernel hook timeout; late replies may be dropped | yes for malformed action/shape | Unknown expired `call_id` after timeout is warn+drop. |
| `plugin_send` | yes | lossless to kernel boundary | yes | Unsupported `channel` is protocol-fatal because silent bus loss changes behavior. |
| `plugin_log` | no hard ordering guarantee needed | best-effort, drop allowed with sanitized warning | no, drop+warn | Preserve availability over perfect log retention. |
| `lifecycle_notice` | latest state per target matters | lossless latest-state update | yes | Kernel status/readiness depends on it. |
| worker `log` | no | best-effort within runtime | no, drop+warn | Runtime may rate-limit. |
| worker `tools_updated` | yes, per target | lossless | yes | Runtime must not continue with stale tool catalog. |

Additional rules:
- First terminal outcome still wins for any invoke or hook `call_id`.
- Runtime disconnection fails pending tool calls with `runtime_unavailable` and pending hook waits as disconnect/timeouts under existing hook-engine semantics.
- `plugin_log` drops must never emit unsanitized user payloads, tokens, or raw auth material.

### 5.7 Readiness and status model for runtime-hosted targets
- Use **manifest-seeded, worker-confirmed readiness**.
- Kernel runtime attachment remains runtime-scoped (`attached`, runtime daemon `pid`, connection health).
- Add a runtime-backed per-target read model owned by the kernel registry/Hub status layer with these states:
  - `manifest_loaded`: manifest known, no authoritative worker catalog yet.
  - `initializing`: runtime is starting or reinitializing the worker.
  - `ready`: authoritative catalog/hooks installed and target invokable.
  - `failed`: worker init/update crashed or failed.
  - `stale`: runtime detached or target invalidated by reload; kept only for diagnostics until replaced.
- State transitions:
  - manifest discovery seeds `manifest_loaded`.
  - `lifecycle_notice(state="starting")` → `initializing`.
  - first valid `catalog_update` plus `lifecycle_notice(state="ready")` → `ready`.
  - `lifecycle_notice(state="crashed")` or init failure → `failed`.
  - reload/detach → `stale`, remove dispatch entries, await fresh `catalog_update`.
- `tabula status --json` for M2-08 should rely on:
  - runtime attachment: `id == "local"`, `attached == true`, runtime daemon `pid > 0`;
  - at least one runtime-hosted target in `ready` with non-empty authoritative tool evidence from a migrated smoke plugin.
- Do not keep or fake kernel-owned plugin handles solely for status.

### 5.8 M2-07 acceptance scope for `plugin_send` and `plugin_log`
- `plugin_send`: **required** for M2-07 acceptance. No deferral is accepted because it is part of the current kernel plugin contract and companion evidence did not authorize removing bus semantics.
- `plugin_log`: **required for M2-07 acceptance**, but best-effort delivery is acceptable. Gateway Telegram lifecycle/diagnostic evidence depends on preserving structured plugin log visibility or an explicitly equivalent runtime-managed replacement; no such replacement is currently approved.

### 5.9 BUILD handoff checklist
- Extend `internal/runtime/wire` decode/validation for the richer capability model and the five async plugin-control ops.
- Extend `internal/runtime/api` / mocks with async sink ownership and a dedicated hook-event send path.
- Refactor `internal/runtime/conn` read loop so no supported async frame is ignored.
- Replace `internal/runtime/worker/wire` with an op-discriminated envelope and single-reader decoder/router.
- Add a runtime target manager that combines manifests, `init_ack`, `tools_updated`, lifecycle detection, and Runtime API async emission.
- Add runtime-backed kernel tool/hook adapters and target status read model before deleting kernel stdio plugin ownership.
- Add tests for malformed/late async frames, per-target revision ordering, dynamic catalog replacement, hook reply timeout behavior, send/log routing, and status snapshot readiness.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 9
    failure_isolation: 9
    constraint_fit: 10
    simplicity: 8
  ai_slop_flags: none
  verdict: PASS
  notes: The chosen sink-based single-connection design preserves the accepted Runtime API boundary while adding the smallest semantics-preserving bridge needed for dynamic plugin control.
