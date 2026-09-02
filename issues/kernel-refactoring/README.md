# Agent-native kernel refactoring

**Status:** completed

This backlog replaces the current capability-inferred message routing around agent turns with an authoritative, durable agent state machine.

Tabula kernel is an agent control plane, not a generic message broker. It owns session, input, driver, turn, attempt, cancellation, and recovery state. Runtime owns process execution and isolation. Drivers implement agent execution. Gateways and other clients submit commands and observe state, but do not coordinate driver readiness or retry delivery.

## Target model

- `Session` is a durable aggregate and remains valid without connected clients.
- `Input` is immutable and idempotently accepted by `input_id`.
- `Turn` is durable work created from one input and ordered in a per-session FIFO.
- `Attempt` distinguishes safe pre-execution retry from uncertain external side effects.
- `Driver` is a first-class authenticated execution role, not inferred from arbitrary topic subscriptions.
- One driver generation owns a session at a time; leases and fencing reject stale mutations.
- Kernel commits its own transitions exactly once, but does not claim exactly-once provider or side-effecting tool execution.
- Runtime ensures driver processes exist, but does not own turn state.
- Gateways use a client command/query/subscription API and have no readiness probe, wake loop, or delivery retry queue.
- Arbitrary topics remain an extension plane and do not carry correctness-critical lifecycle transitions.
- Durable state is accessed through a domain `SessionRepository`; SQLite is the first adapter, not part of the kernel contract.
- Artifacts remain outside kernel ownership and are out of scope.

## Required workflow

Every issue must begin by checking current source and accepted ADRs. The design conversation captured here is direction, not a substitute for source verification.

Architecture-level changes require a new ADR. Existing ADRs are immutable; supersede them explicitly where decisions change. Protocol v4 is a breaking cutover with no v3 compatibility path.

Every implementation issue must:

- preserve unrelated worktree changes;
- add focused tests, using `-race -count=1` for affected Go packages;
- update current protocol, architecture, and installed `tabula-guide` documentation when behavior changes;
- add or update installed testbed coverage for cross-process, restart, persistence, or fan-out behavior;
- keep canonical testbed files and the generated template synchronized;
- avoid generated build artifacts.

## Protocol audit

See [protocol-audit.md](protocol-audit.md). It separates:

- dead v3 surface safe to remove independently;
- active v3 surface that must remain until v4 replacement;
- capability-inferred agent semantics to remove at v4 cutover;
- active Runtime API and worker protocol operations that are not cleanup candidates.

## Issues

| ID | Title | Type | Blocked by |
| --- | --- | --- | --- |
| 01 | Define agent-native kernel protocol v4 | HITL | None |
| 02 | Remove confirmed dead protocol v3 surface | AFK | None |
| 03 | Implement the pure session aggregate state machine | AFK | 01 |
| 04 | Define SessionRepository and its conformance suite | AFK | 01, 03 |
| 05 | Add the default SQLite SessionRepository adapter | AFK | 04 |
| 06 | Deliver durable idempotent input submission end to end | AFK | 03, 04, 05 |
| 07 | Add runtime-owned driver supervision and authenticated registration | AFK | 01, 03 |
| 08 | Enforce driver leases, generations, and fencing | AFK | 04, 07 |
| 09 | Deliver explicit turn assignment, preparation, and execution permits | AFK | 06, 08 |
| 10 | Persist sequenced turn output and resumable subscriptions | AFK | 04, 09 |
| 11 | Deliver cancellation, interruption, and explicit recovery | AFK | 09, 10 |
| 12 | Integrate hooks and tools with durable attempts | AFK | 09, 11 |
| 13 | Migrate the driver component and SDK to execution protocol v4 | AFK | 07, 08, 09, 11, 12 |
| 14 | Migrate gateways and client SDKs to the v4 client API | AFK | 06, 10, 11 |
| 15 | Cut over to protocol v4 and remove inferred turn routing | AFK | 13, 14 |
| 16 | Prove crash recovery in installed multi-tenant testbeds | AFK | 05, 13, 14, 15 |

## Dependency graph

```text
01 -> 03 -> 04 -> 05 -> 06
01 -> 07 -> 08 -> 09 -> 10 -> 11 -> 12 -> 13
04 -> 08
06 -> 09
04 -> 10
06 -> 14
10 -> 14
11 -> 14
13 -> 15
14 -> 15
05 -> 16
15 -> 16

02 is independent and may be completed before the v4 work.
```

## Completion condition

The refactoring is complete only when the installed system demonstrates all of these cases without gateway-owned recovery logic:

- [x] Input submitted before driver startup is durably accepted and later executed.
- [x] Duplicate input submission creates one turn.
- [x] Two gateways can submit and observe one session consistently.
- [x] Driver crash before execution permit retries safely.
- [x] Driver crash after execution permit produces `recovery_required`, never indefinite `thinking`.
- [x] Stale driver output after takeover is rejected.
- [x] Kernel restart restores queued and active state from the repository.
- [x] Cancellation/completion races have one committed terminal result.
- [x] Event subscriptions resume from a durable cursor.
- [x] Session and turn semantics are identical through Web, API, CLI, and test clients.

## Final verification

- Core: `go build ./...`, `go test ./... -race -count=1` (731 tests in 39 packages), `go vet ./...`, `golangci-lint run`, and gofmt verification passed.
- Installer: all 196 `tabula-distro` tests passed, including sequential multi-tenant runtime config compilation.
- Bundles/frontends: affected Python suites, TypeScript SDK tests/typecheck, 125 gateway-web tests, and the production frontend build passed.
- Installed proof: the 9-scenario crash-recovery suite passed twice from clean isolated homes using installed kernel, runtime, driver, tools, and tenant installer.
- Testbed synchronization: canonical and generated crash-recovery files are byte-identical.
- Cleanup: generated bytecode, dependency directories, and build output created during verification were removed.
