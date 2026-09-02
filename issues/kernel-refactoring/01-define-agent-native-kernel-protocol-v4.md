# Define agent-native kernel protocol v4

**Type:** HITL  
**Status:** completed

## What to build

Write and obtain maintainer approval for the architecture decision and complete protocol specification that make kernel the authoritative agent control plane.

Define `Session`, `Input`, `Turn`, `Attempt`, driver registration, lease/generation fencing, execution permits, terminal outcomes, recovery, client commands, subscriptions, and runtime supervision. Specify which transitions are durable, which actor may issue each command, and which messages belong to the client, execution, runtime-control, and extension planes.

This is a breaking protocol v4 decision. Do not design a v3 compatibility mode.

## Required design work

- Add a new ADR and explicitly supersede any older decisions that conflict with first-class driver/session semantics.
- Write command/event schemas and transition tables, including required IDs, versions, generations, sequence numbers, and error codes.
- Specify authentication and authorization for client actors, runtimes, and driver instances.
- Define commit, outbox, replay, cursor, lease, timeout, and fencing semantics.
- State the exact guarantee boundary: kernel transitions are atomic and idempotent; external provider/tool side effects are not claimed exactly once.
- Define startup, reconnect, takeover, cancel, close, archive, delete, suspend, and recovery behavior.
- Define whether existing persisted sessions need migration or may start a new protocol generation.
- Review the protocol against `protocol-audit.md` and classify every active v3 topic as retained extension output, replaced typed lifecycle, or removed.

## Acceptance criteria

- [x] Maintainer approves ADR 0025 and the protocol v4 specification.
- [x] Every authoritative state transition has one owner and one durable command/event path.
- [x] Client API and driver execution API are distinct.
- [x] Driver is a first-class authenticated role; gateway is not a special lifecycle role.
- [x] Runtime process ownership is explicit without moving process execution into kernel.
- [x] State transition tables cover duplicate, stale, concurrent, and invalid commands.
- [x] Failure matrix covers kernel, runtime, driver, gateway, hook, tool, and storage failures.
- [x] Recovery semantics do not automatically repeat an uncertain external side effect.
- [x] Repository requirements are backend-neutral and testable.
- [x] Protocol audit has a disposition for every current first-party producer and consumer.

## Accepted review decisions

The maintainer selected a dedicated `cursor_expired` error, replay without kernel gap buffering, disconnect grace until lease expiry, reversible archive plus logical delete, a clean v4 repository generation without v3 session migration, and recovery authority limited to humans or explicitly configured recovery services.

## Blocked by

None. This is the architecture gate for the refactoring.
