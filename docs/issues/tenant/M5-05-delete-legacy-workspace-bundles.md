# M5-05 — Atomic deletion: `files/files`, `base/shell`, `code/workspace`

Status: done
Phase: M5
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/bundle, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§10)

## What to build

The atomic cleanup. After M5-01..04, the new `fs` and `exec`
plugins are in place, tenants carry workspace config, hooks
are migrated. Now physically delete the legacy bundles.

Per `tabula-bundles/AGENTS.md` "no legacy" rule: delete
in-place, no shim, no rename-to-deprecated.

Components:

- Delete directories:
  - `tabula-bundles/files/files/` (skill).
  - `tabula-bundles/base/shell/` (skill).
  - `tabula-bundles/code/workspace/` (skill).
  - `tabula-bundles/files/` (parent dir if now empty).
- Update `tabula-bundles/_lib/` if any helper was specific to
  these bundles and now unreferenced.
- Update distros that pin these bundles:
  - `tabula-distrib/claw/distro.toml` — replace skill pins
    with plugin pins for `workspace/fs` and `workspace/exec`.
  - `tabula-distrib/coder/skills/gateway-tui/` — separate
    distro; check for references but expected none.
- Update testbed fixtures referencing deleted skills:
  - Replace any `testbed-*` fixture that invoked `files`,
    `shell`, or `workspace` with the equivalent fs/exec
    plugin call.
- Update agent rules / system prompts referencing the deleted
  skills:
  - `tabula/rules/Core/command-execution.md` (mentions shell?
    audit before edit).
  - Any distro-side prompts that name `files.*` or
    `shell.run`.
- Update documentation:
  - `tabula/docs/SKILL_AUTHORING.md` examples.
  - `tabula/docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`.
  - `tabula/docs/plans/COMPETITIVE_LESSONS.md`.
  - `tabula/FOLLOWUP_TASKS.md` — delete entries that are now
    done.
  - `tabula-bundles/BUNDLE_REWORKS.md` — mark as completed
    or delete if it was scoped to this work.
- Lint guard:
  - Add CI check (or a make target) that greps for
    `files/files`, `base/shell`, `code/workspace`,
    `Hub.ProjectRoot`, `TABULA_PROJECT_ROOT`,
    `init.meta.project_root` across all three repos. Match
    only in archive / changelog locations; production code
    must be clean.

## Acceptance criteria

- [x] Three skill directories physically deleted.
- [x] No reference to deleted skill names in any plugin.toml,
      SKILL.md, or distro manifest in any of the three repos.
- [x] Distro tests pass after migration.
- [x] Tabula-distrib testbed coverage of fs/exec replaces
      previous coverage of files/shell.
- [x] All cross-repo grep guards pass.
- [x] Documentation updated.
- [x] No `Hub.ProjectRoot` or `TABULA_PROJECT_ROOT` strings
      anywhere except changelogs / ADRs / removed-feature
      notes.

## Implementation notes

- Deleted `tabula-bundles/files/`, `tabula-bundles/base/shell/`, and
  `tabula-bundles/code/workspace/`.
- Updated generated and canonical testbed manifests to use `workspace/fs` and
  `workspace/exec`; removed legacy `files` and `shell` suites.
- Updated `tabula-distrib` claw/coder manifests to pin the `workspace` bundle
  instead of the deleted `files` bundle.
- Removed `workspace_info` / `workspace_set_root` expectations from code suite
  and subagent type allowlists.
- `tabula status --json` now passes through runtime `targets`, and the testbed
  runner accepts hook-only/cold-skill runtime surfaces during readiness checks.

## Validation evidence

- `PYTHONPATH=_lib/python/src:. python3 -m unittest workspace.exec.tests.test_exec_plugin workspace.fs.tests.test_fs_plugin _lib.python.tests.test_contract` in `../tabula-bundles`
- `go test ./cmd/tabula`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m unittest tools.tabula-testbed.src.tabula_testbed_runner.runner_test`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --suite exec-plugin --suite code`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --suite exec-plugin --suite code --bootstrap-check --home /tmp/tabula-m505-workspace`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --testbed-dir /Users/mak/src/tabula-distrib/testbed --suite fs-plugin --suite exec-plugin --suite code --bootstrap-check --home /tmp/tabula-m505-distrib`

## Blocked by

- M5-01, M5-02 (replacements exist)
- M5-03 (boundary hook migration done)
- M5-04 (workspace config in place)

## Notes

- This is the workspace-decomposition equivalent of M2-07
  (atomic cutover). Run after the new path has soaked in
  testbed for at least one cycle.
- Cross-repo coordination: this PR touches `tabula-bundles`,
  `tabula-distrib`, and `tabula` doc/test references. Land
  each repo's portion in a coordinated set of PRs (bundles
  → distrib → tabula docs). Do NOT split across multiple
  release windows; any window where the kernel expects
  `Hub.ProjectRoot` but plugin expects template would break
  prod.
- After this lands, M5 is done. The kernel is now:
  - workspace-agnostic (no project root concept),
  - multi-tenant (M4),
  - stdio-free (M2),
  - process-mgmt-free (M3).
