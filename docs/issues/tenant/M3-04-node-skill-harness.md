# M3-04 — Node skill harness

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Node skill harness, parallel structure to M3-02/M3-03.
Currently no node skills exist in `tabula-bundles/`; this slice
ships the harness so future node skills "just work" without a
runtime change.

Components:

- `internal/runtime/host/harness/node/`:
  - Selected by `harness_kind == "node"` (manifest reader from
    M3-01 detects `exec` strings starting with `node` or
    `node18`/`node20` etc.).
  - Same protocol-on-behalf pattern: harness handles worker
    protocol, subprocess reads stdin / writes stdout.
  - Env additions:
    - `NODE_PATH` augmented with
      `$TABULA_HOME/_lib/node/node_modules` so skill scripts
      can `require('@tabula/skill-sdk')`.
    - `NODE_NO_WARNINGS=1` to keep stdout clean.
- New shared lib: `tabula-bundles/_lib/node/` (deferred to a
  separate bundles-side issue if and when a node skill ships;
  do not ship empty skeleton in M3-04).

Scope guard: M3-04 ships the **runtime-side** harness only.
The node SDK ships when there's a real node skill to motivate
it. Document this in the harness package godoc so a future
contributor doesn't think it's missing.

## Acceptance criteria

- [ ] Synthetic test fixture node skill (created in this slice
      under `tabula/internal/runtime/host/harness/node/testdata/`)
      runs end-to-end through the harness.
- [ ] `NODE_PATH` set; harness gracefully handles
      `$TABULA_HOME/_lib/node/node_modules` not existing
      (warning, not failure).
- [ ] Crash / non-zero exit → `skill_exec_failed`.
- [ ] Cold mode: process exits after one call.
- [ ] No new bundle dependency in `tabula-bundles` (testdata
      lives in the kernel repo for now).
- [ ] Race-clean.

## Blocked by

- M3-02 (shared harness scaffolding)

## Notes

- This is the lowest-priority harness in M3 and could be cut
  if review feedback prefers smaller M3 scope. Counterargument:
  shipping the third harness pattern at the same time as the
  first two locks in the harness abstraction. If we defer it,
  the abstraction may calcify around bash+python only and need
  rework when the first node skill arrives. Recommended to
  keep in scope.
