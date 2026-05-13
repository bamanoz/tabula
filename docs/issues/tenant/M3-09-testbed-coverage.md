# M3-09 — Testbed coverage for unified worker model

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/testbed, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Extend the testbed runner (`tabula/tools/tabula-testbed/`) to
exercise the unified worker model. Per `tabula/AGENTS.md`
"Tests" rule, install/fan-out behavior must be tested via
testbed, not kernel unit tests.

Components:

- New testbed suite: `skill-cold-execution`:
  - Install runtime + a small bash-skill, python-skill, and
    (synthetic) node-skill.
  - For each: invoke the tool, assert success, assert
    subprocess exited (cold mode).
  - Assert sequential calls spawn distinct PIDs.
  - Assert env vars (`TABULA_SKILL_DIR`, `TABULA_TOOL_NAME`,
    `TABULA_TENANT_ID`) reach the subprocess.
- New testbed suite: `skill-failure-modes`:
  - Skill that exits non-zero → `WorkerResult{ok: false}`.
  - Skill that calls `tabula_skill_sdk.fail(...)` → structured
    error envelope.
  - Skill that hangs → cancel via timeout produces
    `cancelled` error code; subprocess SIGKILL'd.
  - Runtime crash mid-skill-call → kernel surfaces
    `runtime_unavailable`.
- New testbed suite: `skill-concurrency`:
  - 8 parallel calls of the same skill → 8 parallel
    subprocesses, all complete.
  - Pool limit reached → some calls `runtime_busy` (or
    structured error per M3-06's wire choice).
- Update `runtime-smoke` (M2-08) to add at least one skill
  call in addition to the plugin call already there.
- Keep canonical testbed files and the generated testbed
  template in sync (existing AGENTS.md rule).

## Acceptance criteria

- [ ] Three new testbed suites added under
      `tools/tabula-testbed/.../testbeds/`.
- [ ] All three suites pass on macOS and Linux CI.
- [ ] `runtime-smoke` exercises at least one skill end-to-end.
- [ ] Generated testbed template files synced with canonical
      sources.
- [ ] Suites use installed bundle path (per AGENTS.md, not
      source-tree-relative).

## Blocked by

- M3-08 (final layout stable; running these against an
  intermediate state would be wasted work)

## Notes

- Test bundle fixtures live in `tabula-bundles/test-fixtures/`
  per repo convention; add new minimal `testbed-skill-*`
  bundles there if the existing fixtures don't cover the cases.
- Per `tabula-bundles/AGENTS.md` "Testbed Coverage": these
  suites must execute installed behavior, not just import
  source files. The `runtime.Pool` integration tests in
  `tabula` already cover unit-level behavior; testbed adds
  install-fan-out coverage.
