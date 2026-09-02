# Kernel refactoring v2

**Status:** proposed  
**Source review:** 2026-08-09

This backlog follows the completed protocol-v4 refactoring. It removes duplicate authority, fixes two correctness gaps, and narrows the kernel to a policy-free authoritative coordination layer.

The kernel continues to own durable session state, ordered events and cursors, transactional delivery, driver leases and fencing, execution permits, cancellation races, recovery boundaries, tenant/runtime routing, and generic hook/tool lifecycle transport. Providers, prompts, output presentation, audit enrichment, approvals, distro composition, and product policy remain outside core.

## Required workflow

Every issue must begin by checking current source and accepted ADRs. Existing ADRs are immutable; architecture changes require a new ADR with `Supersedes` metadata where applicable.

Every implementation issue must:

- preserve durable acceptance, idempotency, cursors/replay, leases/fencing, permits, recovery, and tenant isolation;
- use clean cutover without compatibility aliases or parallel authority;
- add focused tests, with `-race -count=1` for affected Go packages;
- add installed testbed coverage for cross-process, restart, retention, or tenant-routing behavior;
- update protocol, architecture, ADR, and installed `tabula-guide` documentation when contracts change;
- keep canonical testbeds and generated templates synchronized.

## Issues

| ID | Title | Type | Blocked by |
| --- | --- | --- | --- |
| 01 | Preserve tenant scope on worker bus emissions | AFK | None |
| 02 | Redeliver active execution from authoritative projection | AFK | None |
| 03 | Remove command deduplication from aggregate projection | AFK | None |
| 04 | Centralize authoritative transition commits | AFK | 03 |
| 05 | Remove legacy Hub session persistence and lifecycle authority | AFK | None |
| 06 | Remove kernel-owned process supervision | AFK | None |
| 07 | Externalize concrete tool audit enrichment | AFK | None |
| 08 | Externalize tenant init metadata materialization | AFK | None |
| 09 | Generalize the bounded attempt-output contract | HITL | None |
| 10 | Move prompt preparation to the driver boundary | HITL | None |
| 11 | Consolidate durable tool-call coordination | AFK | 04 |
| 12 | Bound per-key lifecycle serialization state | AFK | None |
| 13 | Split protocol-v4 client command and query adapters | AFK | 04 |
| 14 | Decompose Hub and remove the shallow PolicyEngine wrapper | AFK | 05, 06, 11, 13 |
| 15 | Externalize exchange responder arbitration | HITL | None |
| 16 | Move plugin-kind startup policy to distro materialization | AFK | None |
| 17 | Prove the reduced kernel contract end to end | AFK | 01–16 |

## Dependency graph

```text
03 -> 04 -> 11 -> 14
04 -> 13 -> 14
05 -> 14
06 -> 14
01 ---------------------> 17
02 ---------------------> 17
07 ---------------------> 17
08 ---------------------> 17
09 ---------------------> 17
10 ---------------------> 17
12 ---------------------> 17
14 ---------------------> 17
15 ---------------------> 17
16 ---------------------> 17
```

## Completion condition

The backlog is complete when:

- same-named sessions remain isolated across tenants for every runtime-originated path;
- active assignment and permit redelivery survives event/outbox retention;
- one repository owns command deduplication and one aggregate owns durable session state;
- kernel code contains no concrete provider, prompt-builder, workspace, tool-name, user-role, or distro startup policy;
- runtime owns worker processes and distro materialization owns deployment composition;
- protocol adapters and tool coordination are independently testable modules rather than Hub state;
- installed crash/restart, cursor expiry, stale generation, cancellation race, tool execution, and multi-tenant suites pass after deletion of the superseded surfaces.
