# Prove crash recovery in installed multi-tenant testbeds

**Type:** AFK  
**Status:** completed

## What to build

Add canonical testbed coverage for the installed v4 lifecycle across tenants and process boundaries. Tests must execute the real installed kernel, runtime, driver, and gateway paths rather than inspect catalogs only.

## Required scenarios

- input before driver startup;
- runtime restart while input is queued;
- driver crash before and after permit;
- kernel restart with queued and active turns;
- stale generation after takeover;
- duplicate submit from two gateways;
- reconnect snapshot plus cursor replay;
- cancellation/completion race;
- hook/tool failure during an attempt;
- tenant isolation and session persistence.

## Acceptance criteria

- [x] Canonical testbed suite covers every required scenario.
- [x] Generated testbed template is synchronized.
- [x] Tests execute installed components and assert observed lifecycle outcomes.
- [x] Failure diagnostics include session, input, turn, attempt, generation, and cursor identifiers.
- [x] Testbed runs are stable under repetition and relevant race tests pass in core.
- [x] Generated artifacts are cleaned and not committed.

## Verification

The canonical installed suite and generated template are byte-identical. Two
consecutive clean isolated runs passed all 9 scenarios, including dynamic tenant
installation, default-tenant continuity, runtime/kernel restarts, stale takeover,
cursor replay, duplicate submission, cancellation races, and attempt-scoped tool
failure. The supporting `tabula-distro` suite passed all 196 tests.

## Blocked by

Issues 05 and 15.
