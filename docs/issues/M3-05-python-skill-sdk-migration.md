# M3-05 — Python skill SDK migration

Status: open
Phase: M3
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Add the `tabula_skill_sdk` shared Python library (consumed by
the python skill harness in M3-03) and migrate existing python
skills to it where the new helpers reduce boilerplate.

This is intentionally **not** a forced rename: most existing
skills do `args = json.loads(sys.stdin.read())` and that keeps
working. Migration covers (a) skills with significant stdin/
result boilerplate that the SDK simplifies, and (b) any skill
relying on env vars that the new harness exposes (e.g.
`TABULA_SKILL_DIR`). Per `tabula-bundles/AGENTS.md`, no legacy
shims are introduced.

Components:

- `tabula-bundles/_lib/python/src/tabula_skill_sdk/`:
  - `__init__.py` exporting `read_args`, `write_result`,
    `fail`, `tool_name`, `skill_dir`.
  - `paths.py` — `skill_dir()`, `skill_state_dir()`
    (`$TABULA_HOME/state/skills/<skill-name>/`).
  - `io.py` — stdin/stdout JSON helpers.
  - `errors.py` — `fail(code, message, details=None)` writes
    error envelope to stdout, exits 1.
  - `__version__ = "0.1.0"`.
- Migrate skills that have meaningful boilerplate. Initial
  list (verify each before touching):
  - `base/timer/scripts/run.py`
  - `memory/memory-search/scripts/run.py`
  - `memory/memory-save/scripts/run.py`
  - `memory/memory-admin/scripts/run.py`
  - `code/todo/scripts/run.py`
  - `files/files/scripts/run.py` (if present — note: `files`
    skill is slated for M5 deletion; skip it here, do not
    waste migration effort)
- Skills that already use minimal stdin/stdout patterns and
  have no pain — leave them alone. Migration is opportunistic,
  not forced.
- Update `tabula-bundles/_lib/python/pyproject.toml` to declare
  the new package alongside `tabula_plugin_sdk`.
- Tests:
  - Unit tests for each SDK function.
  - Per-migrated-skill: existing skill tests still pass.

## Acceptance criteria

- [ ] `tabula_skill_sdk` package builds and installs into
      `$TABULA_HOME/_lib/python/`.
- [ ] Each migrated skill imports from `tabula_skill_sdk`,
      passes its existing tests, runs against the M3-03
      harness in a smoke test.
- [ ] No skill imports `tabula_plugin_sdk` (skills and plugins
      do not share SDK; verify with grep).
- [ ] Bundle tests / testbed coverage updated where SDK changes
      altered observable behavior.
- [ ] Skills that were not migrated still work as before
      (keep them on raw stdin/stdout).

## Blocked by

- M3-03 (harness exposes the env vars and PYTHONPATH the SDK
  relies on)

## Notes

- Skip the `files` skill: it is deleted in M5. Touching it
  here is wasted work.
- Keep the SDK surface minimal in v0.1.0. Add helpers when a
  second skill needs them, not speculatively.
- AGENTS.md "no legacy / no backward compatibility" applies:
  migrated skills delete their old stdin/stdout boilerplate,
  not wrap it with a fallback.
