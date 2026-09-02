# ADR 0025 - Agent-native kernel control plane

Date: 2026-08-02
Status: Accepted
Supersedes: ADR 0003 and ADR 0005 upon acceptance
Partially supersedes: ADR 0008 turn lifecycle delivery semantics upon acceptance
Superseded by: nothing

## Context

The protocol v3 kernel routes exact topics between clients and infers an agent turn executor from capabilities: a client that receives `message.user` and sends `turn.done`. The kernel also keeps an in-memory FIFO for managed user input and persists observational session snapshots.

This creates several competing sources of truth:

- kernel infers driver readiness from connected clients and topic declarations;
- gateways optimistically track inputs and may retry session joins to wake a driver;
- runtime owns worker processes but not the desired driver lifecycle;
- the driver owns provider execution, while kernel has no explicit acceptance or execution boundary;
- persisted snapshots cannot distinguish work that is safe to retry from work that may already have produced external side effects.

A disconnect between gateway submission and driver delivery can therefore leave the browser in `thinking`, lose an input, or make recovery depend on timing. Making all participants generic topic subscribers does not solve these races. It turns Tabula into a message broker while moving agent correctness into gateways and bundle conventions.

Tabula is an agent platform. Its kernel should understand the durable lifecycle that makes an agent reliable while remaining independent of provider, model, prompt, tool implementation, gateway product, distro, and driver implementation.

## Decision

Make kernel the authoritative control plane for agent sessions.

Kernel owns the durable state machines for:

- `Session`, whose canonical durable/client states are `open`, `suspended`, and `closed`;
- immutable `Input`;
- ordered `Turn`;
- execution `Attempt`;
- active driver lease and generation;
- cancellation and recovery intent;
- committed output ordering and client subscription cursors.

### Driver is a first-class execution role

Driver is not inferred from arbitrary topic subscriptions. A driver is a replaceable bundle component hosted by runtime and registered through an authenticated execution protocol.

For each session, kernel grants at most one active driver lease. The lease has a monotonically increasing generation and an unguessable lease identifier. Every driver mutation is fenced by session, instance, generation, lease, turn, and attempt identifiers. Kernel rejects stale generations and invalid transitions.

Kernel knows driver lifecycle and execution authority. It does not know provider, model, prompt construction, driver package, process command, or retry policy internal to a provider.

### Gateway is a client, not a lifecycle role

Web, API, TUI, CLI, automation, and agent-to-agent adapters use the same authenticated client API. They submit commands, query projections, and subscribe to committed events.

Gateway disconnect does not change session, driver, turn, or attempt state. Gateways do not start drivers, probe readiness, retry delivery, or keep an authoritative input queue.

### Runtime owns processes

Kernel records desired driver state and sends typed ensure/stop commands to the selected attached runtime. Runtime starts, monitors, isolates, and stops driver workers and reports process lifecycle.

Runtime does not own session input ordering, turn transitions, execution retry, or recovery decisions. Kernel does not spawn or supervise operating-system processes directly.

### Durable input and turn state

A client submits an immutable input with a client-generated `input_id` and `command_id`. Kernel replies with acceptance only after the repository atomically commits the input, its turn, domain events, and outbox records.

The same input ID and payload is idempotent. Reusing an input ID with a different payload is a conflict. Per-session turn order follows committed aggregate order, independent of client connection timing.

### Explicit execution boundary

Driver execution uses these authoritative stages:

```text
turn.assign -> turn.prepared -> turn.permit -> output -> terminal
```

Before `turn.permit`, the driver must not call a provider or execute a side-effecting tool for that attempt. A lost pre-permit attempt may be automatically replaced.

After permit, loss of execution authority makes the attempt uncertain unless the driver can prove a safe resume through a durable provider operation ID or checkpoint. Kernel moves the turn to `recovery_required`; it does not silently repeat possibly completed external side effects.

Recovery is explicit through resume, retry, discard, or cancel commands.

### Backend-neutral persistence

Kernel depends on a domain `SessionRepository`, not SQLite or files. A repository commit atomically provides:

- expected-version compare-and-swap;
- command and input deduplication;
- ordered domain event append;
- aggregate projection update;
- transactional outbox append;
- stable aggregate version and event cursor.

SQLite is the default local adapter. The reference adapter uses a pure-Go driver, WAL mode, `synchronous=FULL`, transactional versioned migrations, and an explicit corruption error path; it serializes writers while allowing durable reopen/replay. Other adapters may be provided if they pass the same conformance suite and preserve the contract. Artifacts remain outside kernel ownership and outside this repository.

### Committed state and delivery

Kernel state changes become visible only after repository commit. Outbox delivery may be at least once. Consumers deduplicate by stable command, event, turn, attempt, and sequence identifiers.

Kernel guarantees atomic and idempotent transitions in its own state. It does not claim exactly-once provider calls, side-effecting tools, network delivery, or arbitrary extension handlers.

### Protocol planes

Protocol v4 separates:

- client command/query/subscription API;
- driver execution API;
- kernel/runtime driver-control API;
- extension and plugin traffic.

Correctness-critical agent transitions use typed operations. Arbitrary topics remain available for extension traffic but cannot establish driver authority, input acceptance, turn completion, or recovery.

The complete wire and state contract is `docs/KERNEL_PROTOCOL_V4.md`.

### Breaking cutover

Protocol v4 replaces protocol v3. The implementation does not preserve a v3 compatibility mode, deprecated message aliases, capability-inferred drivers, or dual session persistence paths.

Existing observational session JSON files are not migrated. Protocol v4 starts a new authoritative repository generation. The legacy `DiskSessionStore` may continue writing and hydrating Hub-local observational, tool-lifecycle, and diagnostic metadata, but no protocol-v4 command, query, supervision, fencing, recovery, or execution transition reads it as lifecycle state. It cannot create, import, modify, or resume a durable v4 `SessionRepository` aggregate.

## Consequences

Positive:

- input acceptance and turn state no longer depend on WebSocket timing;
- gateway reconnect cannot lose or duplicate an accepted input;
- runtime and driver restarts have explicit ownership and fencing;
- stale driver events cannot complete a newer turn generation;
- kernel restart can reconstruct queued, active, and recovery-required work;
- all client adapters observe one authoritative session projection;
- future persistence adapters do not change kernel domain semantics.

Negative:

- kernel becomes a richer agent-specific state machine rather than a small topic router;
- protocol v4 requires coordinated core, bundle, distro, SDK, gateway, driver, and testbed changes;
- transactional persistence and outbox delivery add implementation complexity;
- permitted attempts may require human or driver-specific recovery when an external outcome is uncertain;
- protocol v3 clients and persisted session snapshots are not directly compatible.

## Rejected alternatives

### Treat kernel as an arbitrary-topic broker

Rejected because readiness, retries, deduplication, and turn ownership move into gateways and conventions. Multiple clients then maintain conflicting state and cannot close disconnect races reliably.

### Publish driver readiness and gate every input in gateway

Rejected as the correctness mechanism because readiness can become stale between probe and input delivery. It is useful for diagnostics but cannot replace durable acceptance and fenced assignment.

### Let runtime own the turn queue

Rejected because session state would split between kernel persistence and runtime process lifecycle. Remote runtimes and runtime restart would make kernel projections non-authoritative.

### Automatically retry every lost attempt

Rejected because a provider or tool may have completed an external side effect before the process or connection was lost. Automatic retry is safe only before permit or with explicit idempotency/reconciliation evidence.

### Embed runtime process management into kernel

Rejected because process isolation, language dependencies, remote runtimes, and worker restart remain separate concerns. Logical control-plane ownership does not require one process.

## Verification

Implementation must prove:

- duplicate and concurrent input submission;
- driver startup after input acceptance;
- runtime and driver restart before and after permit;
- stale generation fencing;
- kernel crash and repository reopen at each commit/outbox boundary;
- cancellation/completion races;
- snapshot plus cursor resume;
- hook and tool failure during attempts;
- multi-gateway and multi-tenant isolation;
- installed end-to-end recovery without gateway wake/retry logic.
