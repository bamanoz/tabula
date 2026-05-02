# Creative Phase: Runtime API Contract

## 1. PROBLEM DEFINITION
- What needs to be designed: the stable Runtime API contract that every execution backend uses between the Tabula kernel and `tabula-runtime` during M1-M6.
- Constraints:
  - Use the accepted ADR direction: one Runtime API for plugins and skills; do not re-open the runtime-daemon design.
  - M1 is behavior-free scaffolding; no transport or production dispatch change before the planned cutover issues.
  - `tenant_id` is mandatory for every `Invoke`, including the M1-M3 single-tenant/default period.
  - Target is the wire object `{kind, id}`, never display strings such as `skill:timer`.
  - Use only the canonical wire error roster from `docs/issues/AMENDMENTS.md` C8; do not introduce stale aliases.
  - Keep plugin-internal errors out of the Runtime API wire error namespace.
- Success criteria:
  - BUILD can implement `internal/runtime/wire/` without naming drift.
  - Contract tests can assert wire, worker, interface, mock, cancellation, timeout, disconnect, protocol-error, and concurrency semantics.
  - Later transports (`unix`, `wss`, `ssh stdio`) reuse the same envelope unchanged.
- Non-functional requirements:
  - Backward compatibility shims are not allowed; this is a new internal contract.
  - Failure semantics must be deterministic enough for testbed and SECURITY evidence.
  - JSON shapes must remain stable once M1 lands.

## 2. OPTIONS

### Option A: Typed envelope with operation-specific payloads
- Description: Define a small discriminated envelope (`op`, optional `call_id`) plus typed payload structs for `Hello`, `HelloAck`, `Invoke`, `InvokeResult`, `Cancel`, `CancelAck`, `Health`, `HealthResp`, `ListCapabilities`, `ListCapabilitiesResp`, `Reload`, and `ReloadAck`.
- Architecture: Lives in `internal/runtime/wire/`; codec/transports consume it, runtime/backend interfaces expose typed calls.
- Advantages:
  - Strong Go tests for required fields and canonical errors.
  - Transport-agnostic and suitable for mocks.
  - Keeps `Invoke` tenant-scoped while system ops remain tenantless.
  - Supports exact JSON round-trip fixture tests.
- Disadvantages:
  - Requires explicit decode/validation layer rather than raw `map[string]any` passthrough.
  - Adding an op needs a code change and a fixture.
- Risk factors:
  - A careless decoder could accept missing `tenant_id` or stale errors unless validation is centralized.

### Option B: Schema-first generic JSON messages
- Description: Keep frames as generic JSON maps validated mostly by tests/schema documents.
- Architecture: Transports pass maps through; higher layers inspect fields at runtime.
- Advantages:
  - Fastest initial scaffolding.
  - Flexible for experimental ops.
- Disadvantages:
  - Higher risk of stale names (`tenant_denied`, `target_not_authorized`) leaking into implementation.
  - Harder to prove `tenant_id`, `{kind,id}`, and error roster invariants.
  - Mock runtime assertions become less precise.
- Risk factors:
  - Runtime errors shift from decode-time to production invocation paths.

### Option C: Transport-coupled message structs
- Description: Define message structs inside the first transport package and reuse them indirectly.
- Architecture: `unix`/codec package becomes owner of the Runtime API shape.
- Advantages:
  - Minimal package count during M2.
  - Easy to co-locate frame-size and decode code.
- Disadvantages:
  - Violates ADR separation: backends should terminate at the same RuntimeConn and wire contract.
  - WSS/SSH would inherit unix-local assumptions.
  - Makes M1 behavior-free contract tests less clear.
- Risk factors:
  - Transport-specific quirks become accidental API.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 5 | 4 |
| Performance | 2 | 5 | 5 | 5 |
| Maintainability | 5 | 5 | 2 | 2 |
| Scalability | 4 | 5 | 3 | 3 |
| Security | 5 | 5 | 2 | 3 |
| **Weighted Total** | | **92** | 59 | 62 |

## 4. DECISION
**Selected: Option A — typed envelope with operation-specific payloads.**

Justification: Option A best preserves the accepted architecture: the Runtime API is a contract above every backend, not a byproduct of one transport. It also gives BUILD a concrete place to enforce the amendment-normalized error roster, mandatory tenant attribution, and target object shape before production dispatch changes begin.

Trade-offs accepted:
- Slightly more M1 scaffolding and fixture work.
- New ops must be added deliberately with tests rather than by opportunistic map fields.

## 5. IMPLEMENTATION GUIDELINES

### Canonical operation roster
- `Hello`: runtime → kernel handshake. Carries `runtime_id`, token/auth material, protocol version, and optional capability preview.
- `HelloAck`: kernel → runtime accept/reject. Reject uses `Error{code,message,retryable}` and closes connection.
- `Invoke`: kernel → runtime tenant-scoped tool call.
- `InvokeResult`: runtime → kernel terminal result for an `Invoke`.
- `Cancel`: kernel → runtime explicit abort for an in-flight `call_id`.
- `CancelAck`: runtime → kernel acknowledgement that cancellation was observed and worker termination is underway/done.
- `Health` / `HealthResp`: system liveness, no `tenant_id` field.
- `ListCapabilities` / `ListCapabilitiesResp`: system capability listing, no `tenant_id` field; tenant-aware filtering arrives through M4 status/capability layers, not by making these Invoke variants.
- `Reload` / `ReloadAck`: deliberate worker/config refresh, distinct from reconnect.

### Wire object shapes
- Envelope fields use stable snake_case JSON.
- `call_id` is an opaque string generated by the kernel; the runtime must correlate but not parse it.
- `Invoke.args` and `InvokeResult.data` use raw JSON to avoid coupling wire types to tool schemas.
- `Target` wire form is exactly:
  ```json
  {"kind": "skill", "id": "timer"}
  ```
  or
  ```json
  {"kind": "plugin", "id": "fs"}
  ```
- `Target.kind` allowed values: `skill`, `plugin`.
- Display/log identifiers like `skill:timer`, `plugin:fs`, and `TABULA_TARGET_ID` values are not wire target forms.
- `tenant_id`, `runtime_id`, and `kernel_id` validation uses `^[a-z0-9][a-z0-9-]{0,62}$` with the reserved-name rules defined in the tenant/config design doc.

### Canonical wire error roster

| Code | Boundary | Retryable | Primary source | Notes |
|------|----------|-----------|----------------|-------|
| `unauthorized` | wire | false | handshake/auth | Token/cert failure. Do not log plaintext token. |
| `runtime_unavailable` | wire | true | transport/backend | Disconnect or no live connection. In-flight calls fail fast. |
| `runtime_busy` | wire | true | runtime pool | Cold worker overflow/contention. |
| `unknown_runtime` | wire | false | registry/auth | Runtime id absent or cert/registry mismatch. |
| `tenant_unknown` | wire | false | kernel routing | Tenant id is syntactically valid but not known to the kernel. |
| `tenant_forbidden` | wire | false | whitelist | Runtime/tenant binding is disallowed. Replaces stale `tenant_denied`. |
| `target_unknown` | wire | false | capability resolution | Target is not present in runtime capabilities. |
| `target_forbidden` | wire | false | policy | Target exists but tenant/policy forbids it. Replaces stale `target_not_authorized`. |
| `tool_not_found` | wire | false | target/tool dispatch | Target exists, requested tool does not. |
| `timeout` | wire | false | kernel deadline | Per-call deadline expired; kernel also sends `Cancel`. |
| `cancelled` | wire | false | explicit abort | User/driver sent explicit cancel. Distinct from timeout. |
| `protocol_error` | wire | false | decode/protocol | Malformed frame, unknown op, missing required fields. |
| `internal_error` | wire | false | catch-all | Unexpected kernel/runtime failure. Use sparingly with logs. |
| `skill_exec_failed` | wire | false | skill harness | Skill subprocess/harness returned non-zero. |

Plugin-internal codes are not Runtime API wire codes:
- `fs_outside_root` belongs in plugin/tool result envelopes.
- `exec_denied` belongs in plugin/tool result envelopes.

### Cancellation, timeout, and disconnect semantics

| Scenario | Trigger | Kernel terminal result | Runtime action | Late frame handling |
|----------|---------|------------------------|----------------|--------------------|
| Explicit cancel | User/driver abort sends `Cancel{call_id}` | `cancelled`, retryable false | SIGTERM worker/op → 5s → SIGKILL; send `CancelAck` | Late `InvokeResult` ignored after terminal state. |
| Timeout | Per-call deadline expires | `timeout`, retryable false | Kernel sends `Cancel`; runtime terminates as above | Late `CancelAck` may be logged; call already terminal. |
| Disconnect | Transport closes while call pending | `runtime_unavailable`, retryable true | Runtime reconnects separately; old in-flight calls are not recovered | Results on old connection cannot arrive; after reconnect duplicate `call_id` is rejected/protocol error. |
| Protocol error | Bad frame/unknown op/missing required field | `protocol_error`, retryable false where correlated | Close or reject frame depending severity | No partial success. |

Rules:
- First terminal event wins for a `call_id`.
- Timeout is not an explicit user cancellation and must not be reported as `cancelled`.
- Missing `tenant_id` on `Invoke` is a `protocol_error`, not `tenant_unknown`.
- System ops must not grow optional/empty `tenant_id` fields as a shortcut.

### Contract-test strategy
- M1-01: marshal/unmarshal round trip for every op; required field validation for `Invoke.tenant_id`, `target.kind`, `target.id`, and known `op`.
- M1-05/M1-06: mock `RuntimeConn` tests for every canonical error code, cancellation, timeout, disconnect, protocol error, and concurrent invoke correlation.
- Race tests must include at least: result-vs-cancel race, timeout-vs-disconnect race, and concurrent calls to the same target with distinct call IDs.
- Fixtures should include rejected stale names to prevent aliases: `tenant_denied`, `target_not_authorized`, `fs_outside_root`, `exec_denied` at wire level.

### BUILD handoff checklist
- Implement the roster as constants with no extra aliases.
- Keep decode validation centralized enough that all transports share it.
- Add docstrings to every wire field explaining direction and requiredness.
- Make status/capability JSON evolution additive in later milestones; do not overload Runtime API errors to carry status snapshots.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 8
    failure_isolation: 9
    constraint_fit: 10
    simplicity: 8
  ai_slop_flags: none
  verdict: PASS
  notes: The selected contract isolates protocol invariants from transports and directly encodes the amendment-normalized wire/error constraints BUILD must preserve.
