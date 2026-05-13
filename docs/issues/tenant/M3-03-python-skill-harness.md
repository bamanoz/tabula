# M3-03 — Python skill harness

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Python skill harness, parallel to M3-02 but with one decisive
difference: most existing python skills already use a shared
`scripts/run.py` pattern (see `tabula-bundles/base/timer/SKILL.md`
or `base/skill-contract`). The harness should keep that
contract identical — author still writes `python3 scripts/run.py
tool <name>` in their `exec`, reads args from stdin as JSON,
writes result to stdout.

Components:

- `internal/runtime/host/harness/python/`:
  - Same shape as M3-02 bash harness: harness owns the
    worker-protocol side, subprocess speaks "args on stdin,
    result on stdout".
  - Selected by manifest `harness_kind == "python"`.
  - Env additions on top of bash harness env:
    - `PYTHONUNBUFFERED=1` (already conventional for stdio
      protocols).
    - `PYTHONPATH` augmented with `$TABULA_HOME/_lib/python/`
      so skill scripts can `import tabula_skill_sdk` (the
      shared helper library).
- New shared lib: `tabula-bundles/_lib/python/src/tabula_skill_sdk/`:
  - `read_args() -> dict` — reads stdin JSON.
  - `write_result(data: dict) -> None` — writes stdout JSON.
  - `fail(code: str, message: str) -> NoReturn` — exits 1 with
    error envelope on stdout (so harness doesn't need stderr
    parsing for structured errors).
  - `tool_name() -> str` — returns `$TABULA_TOOL_NAME`.
  - `skill_dir() -> Path` — returns `$TABULA_SKILL_DIR`.
  - Note: this SDK is for **skill authors**; do NOT confuse
    with `tabula_plugin_sdk` (M2-04, plugin authors). They are
    separate libraries because skills and plugins have
    different lifecycle assumptions.

### Cross-repo split

- `tabula` — harness implementation, manifest hookup, tests.
- `tabula-bundles` — `_lib/python/src/tabula_skill_sdk/`
  package, plus migration of existing python skills to import
  it (separate slice: see M3-05).

This issue is the `tabula` half. M3-05 covers bundle migration.

## Acceptance criteria

- [ ] Python skill `testbed-echo` (or a new `testbed-python-
      echo` if the existing one is bash) runs end-to-end through
      this harness.
- [ ] `PYTHONPATH` includes `$TABULA_HOME/_lib/python/`.
- [ ] `PYTHONUNBUFFERED=1` set.
- [ ] Skill that calls `tabula_skill_sdk.fail("bad_input",
      "missing field")` produces a clean
      `WorkerResult{ok: false, error: {...}}` — error envelope
      on stdout, not stderr.
- [ ] Skill that crashes (uncaught exception, non-zero exit) →
      `skill_exec_failed` with stderr tail.
- [ ] Concurrent invocations isolated per tenant.
- [ ] Cold mode: harness process exits after one call.
- [ ] Race-clean.

## Blocked by

- M3-02 (shared harness scaffolding)

## Notes

- Skill author surface stays close to today's:
  `args = json.loads(sys.stdin.read())` works as it always
  did. The SDK adds convenience but is not mandatory — a
  python skill that uses raw stdin/stdout and never imports
  `tabula_skill_sdk` runs fine.
- The shared `_lib/python/` install path already exists per
  `tabula/AGENTS.md` "Installer And Artifacts" (the installer
  fans out `_lib`). Confirm it does for python skills' import
  path before merging.
