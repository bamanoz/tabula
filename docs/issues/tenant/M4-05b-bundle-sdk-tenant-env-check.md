# M4-05b — Bundle SDK: tenant env defense-in-depth check

Status: done
Phase: M4
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, area/security, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
Companion to: `M4-05` (runtime-side enforcement)
Amendment: AMENDMENTS.md C5

## What to build

Worker-layer of the three-layer tenant enforcement (ADR §6).
At worker startup, both SDKs read `TABULA_TENANT_ID` and
`TABULA_KERNEL_ID`; if either is missing/empty, the SDK
refuses to invoke any tool handler and exits with
`internal_error` envelope.

Components:

- `tabula_plugin_sdk.runtime`:
  - Module-load checks that `TABULA_TENANT_ID` and
    `TABULA_KERNEL_ID` are set and non-empty.
  - On failure, write `{ok: false, error: {code:
    "internal_error", message: "missing tenant/kernel context"}}`
    to stdout and exit 1 before the dispatch loop runs.
- `tabula_plugin_sdk.runtime.current_tenant_id() -> str`:
  returns `$TABULA_TENANT_ID`.
- `tabula_plugin_sdk.runtime.current_kernel_id() -> str`:
  returns `$TABULA_KERNEL_ID`.
- Same for `tabula_skill_sdk.runtime`.

## Acceptance criteria

- [x] Worker started without `TABULA_TENANT_ID` writes
      structured error and exits 1; harness surfaces clean
      `internal_error` to runtime.
- [x] Worker started with both env vars runs normally.
- [x] `current_tenant_id()` / `current_kernel_id()`
      accessible to plugin/skill code.
- [x] Tests cover happy path + each missing-env path.

## Implementation notes

- Added `tabula_plugin_sdk.runtime` and `tabula_skill_sdk.runtime`
  with `current_tenant_id()`, `current_kernel_id()`, and shared
  runtime-context enforcement helpers.
- `tabula_plugin_sdk.run()` rejects startup after reading the worker
  `init` frame but before calling plugin configure code if either env
  var is missing.
- `tabula_skill_sdk.read_args()` rejects skill execution before tool
  input is read if either env var is missing.
- Python cold-worker fixture now uses the SDK helpers so installed
  smoke verifies the helpers under real runtime-spawned env.

## Validation evidence

- `PYTHONPATH=_lib/python/src python3 -m unittest _lib.python.tests.test_contract _lib.python.tests.test_skill_sdk`
- `PYTHONPATH=_lib/python/src python3 -m unittest discover -s _lib/python/tests`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite skill-cold-execution --suite skill-failure-modes --bootstrap-check --home /tmp/tabula-m405b-smoke2`

Known unrelated validation gap:

- `PYTHONPATH=_lib/python/src python3 -m unittest discover -s drivers`
  currently fails in `drivers/test_driver_agents.py` because a test
  fake for `build_main_system_prompt` does not accept the existing
  `workspace_path` keyword argument. This predates and is unrelated to
  tenant env enforcement.

## Blocked by

- M4-05 (runtime sets the env vars on workers it spawns)
- M4-03b (SDK refactor scope alignment)

## Notes

- The point of this slice is defense in depth, not primary
  enforcement. M4-05 is the primary; this catches sloppy
  refactors that drop the env or misconfigured runtimes that
  ship without tenant context.
