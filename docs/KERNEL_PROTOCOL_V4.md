# Kernel Protocol v4

Status: Accepted  
Decision: ADR 0025  
Compatibility: breaking replacement for kernel WebSocket protocol v3

## 1. Scope

Protocol v4 defines authoritative agent lifecycle communication between:

- external clients and kernel;
- driver workers and kernel;
- attached runtimes and kernel for driver process control.

Generic plugin Runtime API and runtime worker protocol remain separate contracts. Extension topics remain available but cannot perform the authoritative transitions defined here.

## 2. Guarantees

Kernel guarantees:

- durable acceptance after commit;
- serializable state transitions per session aggregate;
- command and input idempotency;
- ordered committed events and stable cursors;
- one active driver generation per session;
- fencing of stale driver mutations;
- at-least-once outbox delivery with stable identifiers.

Kernel does not guarantee:

- exactly-once network delivery;
- exactly-once provider calls;
- exactly-once side-effecting tools;
- automatic safe retry after an execution permit;
- durable storage of arbitrary large artifacts.

## 3. Identifiers and versions

All identifiers are opaque UTF-8 strings with bounded lengths defined by implementation limits. IDs are never reused within their scope.

| Field | Creator | Purpose |
| --- | --- | --- |
| `command_id` | command sender | idempotency of one command |
| `input_id` | client | immutable submitted input |
| `turn_id` | kernel | durable work created from input |
| `attempt_id` | kernel | one execution attempt for a turn |
| `driver_instance_id` | runtime | one worker process identity |
| `lease_id` | kernel | unguessable active lease identity |
| `driver_generation` | kernel | monotonic fencing token per session |
| `session_version` | repository | optimistic concurrency version |
| `event_id` | repository | stable committed event identity |
| `cursor` | repository | ordered subscription position |
| `sequence` | producer within fenced scope | ordered output or heartbeat sequence |

Clients must treat all IDs and cursors as opaque.

## 4. Actors and authority

### Client actor

A user, service, agent, or automation identity authenticated to the client API. A gateway is only an adapter for such an actor.

May issue, subject to authorization:

```text
session.create
session.get
session.list
session.subscribe
session.archive
session.delete
input.submit
turn.cancel
turn.resume
turn.retry
turn.discard
```

A client cannot register a driver, acquire a lease, assign attempts, permit execution, or publish driver output.

### Runtime actor

An authenticated attached runtime with a stable runtime identity and tenant placement scope.

May receive driver ensure/stop intents and report process lifecycle. It cannot complete turns or mutate driver execution state unless forwarding an authenticated worker channel defined by the execution API.

### Driver actor

A runtime-attested driver worker bound to one tenant, session, AgentSpec revision, and process instance.

May register, report ready/heartbeat, prepare assigned attempts, publish output for permitted attempts, and report terminal execution outcomes. Every mutation is fenced.

### Kernel actor

The only actor that creates turns and attempts, grants leases, assigns attempts, issues execution permits, commits terminal transitions, and advances the FIFO.

## 5. Transport envelope

Every frame uses a protocol-level envelope:

```json
{
  "v": 4,
  "kind": "command",
  "op": "input.submit",
  "id": "cmd_01J...",
  "tenant_id": "todo",
  "session_id": "main",
  "data": {}
}
```

Fields:

| Field | Required | Meaning |
| --- | ---: | --- |
| `v` | yes | exactly `4` |
| `kind` | yes | `command`, `result`, `event`, `query`, `reply`, or `error` |
| `op` | yes | typed operation name |
| `id` | commands/queries | command or request ID |
| `tenant_id` | scoped operations | tenant route |
| `session_id` | session operations | session route |
| `data` | operation-specific | typed payload |
| `meta` | no | trace and extension metadata; cannot override authority fields |

Authority fields such as actor identity, runtime identity, lease, generation, aggregate version, and cursor are either authenticated transport context or operation-specific typed data. Kernel ignores client attempts to override authenticated identity.

Errors use:

```json
{
  "v": 4,
  "kind": "error",
  "op": "input.submit",
  "id": "cmd_01J...",
  "data": {
    "code": "conflict",
    "message": "input_id already exists with different payload",
    "retryable": false,
    "session_version": 42
  }
}
```

Stable error codes:

```text
invalid_argument
cursor_expired
authentication_failed
permission_denied
not_found
conflict
version_conflict
invalid_transition
stale_driver
lease_expired
session_closed
recovery_required
storage_unavailable
runtime_unavailable
internal
```

`message` is diagnostic and not machine interpreted.

## 6. Session aggregate

### Session lifecycle

```text
open -> suspended -> open
open -> closed
suspended -> closed
```

These are the canonical durable and client-visible session states. Archive is reversible: it hides the session from default listings and rejects new input until `session.unarchive`. `session.delete` is logically irreversible and transitions to the `closed` tombstone state only after active work is terminal; physical repository cleanup happens later under adapter retention policy. Forced deletion of active work is a separate administrative operation, not ordinary `session.delete`.

### Execution projection

Execution projection is derived, not a separate mutable session state:

```text
idle
queued
preparing
executing
cancelling
recovery_required
degraded
```

Clients render this projection instead of inventing optimistic `thinking` after durable acceptance.

### Input

Input is immutable:

```json
{
  "input_id": "in_01J...",
  "content": {"type": "text", "text": "hello"},
  "source": {"actor_kind": "user", "actor_id": "usr_..."},
  "created_at": "2026-08-02T19:00:00Z"
}
```

Kernel canonicalizes the accepted payload and records its digest. Same `input_id` plus same canonical payload is idempotent. Same ID plus a different digest is `conflict`.

### Turn

Turn states:

```text
queued
preparing
executing
cancelling
recovery_required
completed
failed
cancelled
discarded
```

Terminal states are `completed`, `failed`, `cancelled`, and `discarded`. `recovery_required` is non-terminal and blocks FIFO advancement until an explicit recovery command commits.

### Attempt

Attempt states:

```text
assigned -> prepared -> permitted -> completed
         -> failed            -> failed
         -> cancelled         -> cancelled
                              -> uncertain
```

Only one attempt for a turn may be authoritative at a time. Creating a replacement attempt fences the previous attempt.

## 7. Client API

### `input.submit`

Command data:

```json
{
  "input_id": "in_01J...",
  "content": {"type": "text", "text": "hello"},
  "expected_session_version": 41
}
```

`expected_session_version` is optional for append-style input submission. Repository serialization determines FIFO order. Other commands use explicit expected versions where lost-update protection is required.

After atomic commit:

```json
{
  "v": 4,
  "kind": "result",
  "op": "input.accepted",
  "id": "cmd_01J...",
  "tenant_id": "todo",
  "session_id": "main",
  "data": {
    "input_id": "in_01J...",
    "turn_id": "turn_01J...",
    "position": 2,
    "session_version": 42,
    "cursor": "cur_..."
  }
}
```

No acceptance is sent before commit. A lost result is recovered by retrying the same command/input ID or querying the session. The input ID/payload pair is authoritative even if a client retries the same payload with a different command ID; a different payload for an existing input ID is `conflict`.

### Session queries and subscriptions

`session.get` returns a projection and a cursor from one repository read boundary.

`session.subscribe` requests events strictly after a cursor. Delivery is at least once. Event IDs and cursors allow deduplication. Repositories retain records in the latest 4096 combined event/outbox cursor positions per session. If `after_cursor` precedes that retained boundary, kernel returns `cursor_expired` with `snapshot_required: true`; the client fetches `session.get` and subscribes from the snapshot cursor. A cursor at the retained boundary remains valid.

Each committed event contains:

```json
{
  "event_id": "evt_...",
  "cursor": "cur_...",
  "session_version": 42,
  "type": "turn.queued",
  "occurred_at": "...",
  "data": {}
}
```

### Auxiliary session records

`session.record.append` and `session.record.list` store bounded, opaque JSON records associated with a tenant/session. They are for diagnostics and component-owned evidence such as edit diffs, hook dispatch audit, and subagent lifecycle metadata. They are not aggregate events and do not change the session version or committed-event cursor.

Append data contains `kind` and `payload`. Kernel derives `producer` from the authenticated client identity and returns the assigned numeric `id`, `kind`, `producer`, `payload`, and `created_at`. List queries may filter by `kind`, paginate with mutually exclusive `after_id` or `before_id`, and request at most 256 records.

Clients must use committed events for transcript and aggregate reconstruction. Session records cannot replace `session.get` or `session.subscribe`, drive session state, or carry unbounded artifact content.

### Cancellation and recovery

`turn.cancel` records cancellation intent. It does not claim that external work stopped. For an active attempt the same commit writes a fenced `turn.cancel` outbox delivery; the authenticated owning runtime confirms cancellation with the current attempt fence. A queued or recovery-required turn may become cancelled immediately. A terminal completion committed first wins; otherwise cancellation advances according to attempt state and driver acknowledgement/expiry. Every terminal outcome writes a durable `turn.state_changed` notification so clients replace optimistic pending state from authority.

For `recovery_required`:

- `turn.resume` continues the same uncertain attempt only with driver-provided reconciliation evidence accepted by policy;
- `turn.retry` terminally closes the uncertain attempt and creates a new attempt after explicit authorization;
- `turn.discard` terminally closes the turn without execution;
- `turn.cancel` requests cancellation and resolves to `cancelled` when safe/confirmed.

All are idempotent commands under command ID/digest deduplication and survive restart in the session event log/projection. Recovery authority belongs to a human actor or an explicitly configured recovery service. A normal gateway has no recovery authority of its own, and a driver may propose reconciliation evidence but cannot authorize its own uncertain retry.

## 8. Runtime driver-control API

### `driver.ensure`

Kernel to runtime:

```json
{
  "tenant_id": "todo",
  "session_id": "main",
  "agent_spec_revision": "sha256:...",
  "desired_generation": 7
}
```

Runtime must converge one matching worker or report a typed startup failure. Repeated ensure is idempotent. The session projection pins `driver_component_id` and `agent_spec_revision`; runtime resolves the component from its tenant manifest catalog. Driver components declare `kind.name = "driver"`, `worker.mode = "warm"`, and `worker.scope = "session"`, publish no generic plugin tools/hooks, and are never invokable through `invoke`.

### Process lifecycle

Runtime reports:

```text
driver.process_started
driver.process_ready
driver.process_failed
driver.process_exited
driver.process_stopped
```

These facts do not themselves grant execution authority. Kernel registration and lease grant do. Runtime creates a fresh `driver_instance_id` for every spawned process and includes the pinned component, AgentSpec revision, and desired generation in every lifecycle fact. Kernel replays `driver.ensure` from durable open-session projections whenever a runtime reattaches.

## 9. Driver execution API

### Registration and lease

Driver sends `driver.register` through a runtime-attested channel. Kernel validates session assignment, runtime identity, component identity, and AgentSpec revision.

Kernel returns `driver.lease_granted`:

```json
{
  "driver_instance_id": "drv_...",
  "lease_id": "lease_...",
  "driver_generation": 7,
  "expires_at": "...",
  "heartbeat_interval_ms": 5000,
  "session_version": 42
}
```

Heartbeat renews only the current lease and includes monotonically increasing heartbeat sequence. Registration binds the authenticated runtime ID, runtime-attested process instance, driver component, and pinned AgentSpec revision into the durable session projection. Heartbeats from another runtime, duplicate/out-of-order sequences, and mutations carrying an older fence are rejected without changing state. On transport disconnect kernel marks the lease suspect, commits `driver.state_changed` to the transactional outbox, and issues no new assignments. Reconnect before the lease deadline may continue the same generation and lease. Expiry or release clears authority while retaining the monotonic generation counter; takeover after expiry increments generation. A disconnect alone does not permit immediate takeover.

### Assignment

Kernel emits `turn.assign` only to the current lease:

```json
{
  "turn_id": "turn_...",
  "attempt_id": "attempt_...",
  "input": {},
  "prepared_context": {},
  "driver_generation": 7,
  "lease_id": "lease_..."
}
```

Assignment is committed with a stable attempt ID and transactional `turn.assign` outbox record before delivery. It carries the canonical input, opaque driver-preparation context, current fence, session version, and sequence/cursor context. The first-party preparation context contains the current tenant-visible tool catalog, per-prompt context after `before_prompt_build`, and transient `before_turn` context; the driver passes the tool catalog to its provider and appends both context fragments to its base system prompt. Redelivery returns the same active assignment. Driver must not execute external work.

### Preparation

Driver sends `turn.prepared` after validating configuration and constructing any driver-owned execution plan. It may instead send `turn.prepare_failed` with a typed retryable/non-retryable reason. Preparation cannot claim external side effects.

### Permit

Kernel commits the attempt as permitted and writes `turn.permit` to the transactional outbox before delivery. The permit has a stable ID derived from the attempt and is redeliverable only to the same fenced attempt. Acknowledgements from a different authenticated runtime, an older fence, an unprepared attempt, or a conflicting attempt are rejected.

Receipt of a valid permit is the earliest point at which driver may call a provider or side-effecting tool. Driver records any provider operation ID or checkpoint as soon as available.

### Output

Driver emits `turn.output` with:

```json
{
  "turn_id": "turn_...",
  "attempt_id": "attempt_...",
  "driver_generation": 7,
  "lease_id": "lease_...",
  "sequence": 12,
  "output_type": "stream.delta",
  "payload": {"text": "..."}
}
```

Kernel validates authenticated runtime ownership, fencing, documented output type, and sequence; it commits `attempt.output_appended` before appending the `turn.output` outbox notification. Duplicate sequence with identical type and canonical payload is idempotent; same sequence with different content is conflict. Sequence gaps are not buffered: kernel rejects the frame with `output.replay_required` and the next expected sequence. Driver retransmits from that sequence. If it cannot replay, the attempt becomes uncertain and the turn requires recovery.

Bounded output types are `stream.delta`, `reasoning`, `usage`, `provider.retry`, `provider.error`, `compaction`, and `tool.result`. Each payload is capped at 64 KiB.

Provider tool requests use a separate bidirectional execution exchange rather than `turn.output`. After permit, driver sends `turn.tool_call` with the complete attempt/fence tuple, provider tool-call ID, opaque tool name, and JSON input. Kernel acknowledges accepted dispatch with the correlated `driver.result` without waiting for tool completion. After the ordinary fenced tool pipeline reaches a terminal result, kernel sends `turn.tool_result` as a separate request through the same authenticated runtime. Runtime routes it to the exact worker attempt; the driver correlates it to the provider call and may continue generation. Tool request/result exchanges do not consume the ordered output sequence. The durable projection retains the newest 256 outputs or 1 MiB per attempt, whichever limit is reached first, and the newest 1024 outputs or 4 MiB across the session. Eviction removes the oldest output payloads first without resetting producer sequence; the next expected sequence follows the newest retained sequence. Large tool results and provider artifacts are stored externally and represented here by bounded metadata or references. Cursor replay is capped at 256 events per read and repository history at the latest 4096 combined event/outbox cursor positions per session; clients continue from the last returned cursor or recover through `session.get` after `cursor_expired`. The durable session projection plus retained events after its cursor is the authoritative reconnect model, so gateways do not reconstruct state from WebSocket timing.

### Terminal outcomes

Driver may send:

```text
turn.completed
turn.failed
turn.cancelled
turn.uncertain
```

Kernel commits one valid terminal/recovery transition. Later terminal messages are idempotent only if they match the committed outcome; conflicting outcomes return `invalid_transition`.

## 10. Failure semantics

| Failure window | Required outcome |
| --- | --- |
| client disconnect before input commit | no acceptance; retry command |
| client disconnect after input commit | input remains accepted; retry/query returns it |
| no runtime/driver | turn remains queued; kernel requests driver ensure |
| driver loss before permit | attempt is safely replaceable |
| driver loss after permit, no reconciliation evidence | attempt uncertain; turn `recovery_required` |
| driver loss after permit with accepted durable resume evidence | same attempt may resume under a new fenced lease |
| stale driver returns | all mutations rejected as `stale_driver` |
| runtime reconnect | desired workers reconciled from kernel state |
| kernel crash before commit | transition did not happen |
| kernel crash after commit before delivery | outbox redelivers with same IDs |
| cancel races with completion | first valid committed transition wins |
| storage unavailable | no false success; command fails retryably |

Kernel must never convert an uncertain permitted attempt to queued automatically.

## 11. Hooks, tools, and extension output

Hooks and tools remain bundle/plugin capabilities, but calls and results are correlated to tenant, session, turn, attempt, and driver generation.

Lifecycle hooks map to state transitions:

```text
before_message: during durable input preparation
before_turn: before attempt becomes prepared/permitted
after_turn: after committed terminal turn event
before_compaction: driver-owned output barrier correlated to attempt
```

Hook policy explicitly defines missing, unavailable, disconnected, busy, timeout, deny, modify, suspend, fail-open, and fail-closed outcomes. Hook payloads and durable hook audit records preserve `turn_id`, `attempt_id`, `driver_instance_id`, `lease_id`, `driver_generation`, and `turn_correlation_id` when the operation is attempt-scoped. A hook response from a stale or different attempt cannot mutate the current turn.

Tool calls and streamed results are attempt-scoped. A v4 tool call carries the complete attempt/fence tuple in metadata; partial tuples are invalid. Kernel validates that tuple against the active permitted attempt before policy dispatch, after synchronous hooks or approval, before runtime invocation, on every streamed result frame, and before the terminal result is recorded or broadcast. Cancellation, terminal transition, and fencing therefore prevent stale tool output from mutating a newer attempt. Tool lifecycle records and exchange continuations preserve the same tuple. Exchange replies remain identity-bound to the selected responder and the pending exchange ID. Artifact payloads remain external; protocol output may carry stable references.

Runtime stream callbacks are ordered and non-concurrent per call. Kernel preserves that order, cancels the runtime call with the turn, and rejects further frames once the authoritative attempt stops executing. Existing stream, reasoning, usage, provider retry/error, compaction, exchange, and tool topics map to bounded typed `turn.output` or interaction envelopes. Kernel orders and correlates these envelopes but does not interpret provider, compaction, hook, approval, or tool policy.

## 12. Repository contract

Conceptual interface:

```go
type SessionRepository interface {
    Load(ctx context.Context, key SessionKey) (Record, error)
    Commit(ctx context.Context, commit Commit) (CommitResult, error)
    ReadEvents(ctx context.Context, key SessionKey, after Cursor, limit int) ([]Event, error)
    List(ctx context.Context, query SessionQuery) (SessionPage, error)
}
```

`Commit` contains aggregate key, command ID, expected version, decided domain events, projection, and outbox entries. The adapter atomically persists them and returns current projection plus command version/cursor. Command deduplication stores only that compact boundary. Same-ID/same-digest retry bypasses version comparison, returns current projection with original command boundary and empty outbox, and sets `Duplicate`; same ID with another digest is a conflict.

Repository implementations must pass one conformance suite. The first adapters are in-memory and SQLite. Backend selection is deployment configuration. One session has one authoritative repository placement.

## 13. Cutover

Protocol v4 is introduced only after first-party driver and clients implement their respective APIs. Cutover removes:

- protocol v3 serving;
- capability-inferred turn receivers;
- managed-input metadata;
- gateway driver wake/retry;
- legacy inferred-routing status events;
- observational in-memory input queue as authoritative state;
- uncorrelated `turn.done` execution semantics.

There are no v3 aliases or dual-write paths after cutover.

## 14. Accepted review decisions

1. `cursor_expired` is a stable error and requires a fresh snapshot.
2. Output sequence gaps are rejected and replayed; kernel does not buffer gaps.
3. Disconnect marks a lease suspect and stops new assignments; reconnect before expiry may continue, while expiry fences and permits takeover.
4. Archive is reversible. Delete is a logical irreversible tombstone followed by deferred physical cleanup.
5. Protocol v4 starts a clean repository generation and does not migrate existing session JSON.
6. Uncertain recovery requires a human actor or explicitly configured recovery service; normal gateways and drivers cannot self-authorize retry.
