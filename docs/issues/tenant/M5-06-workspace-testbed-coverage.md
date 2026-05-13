# M5-06 — Workspace decomposition testbed coverage

Status: done
Phase: M5
Type: AFK
Repo: tabula
Labels: needs-triage, area/testbed, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)

## What to build

Testbed suites that exercise the new fs+exec plugin world,
including the per-tenant workspace divergence cases that
weren't possible under the global `Hub.ProjectRoot` model.

Components:

- New testbed suite `fs-plugin`:
  - Tenant `alpha` with `project_root = /tmp/alpha-root`.
  - `fs_read("/tmp/alpha-root/foo.txt")` → success.
  - `fs_read("/etc/passwd")` → `fs_outside_root`.
  - `fs_glob("**/*.txt")` honors roots.
  - Symlink that escapes root → rejected by default.
- New testbed suite `workspace-fs-tenant-divergence`:
  - Two tenants with different `project_root` values.
  - Same fs tool name in both → each sees its own root.
  - State (file written by alpha) invisible to beta even
    when paths overlap (impossible by design — but assert).
- New testbed suite `exec-plugin`:
  - `exec_run("pwd")` from default tenant returns
    `cwd_default`.
  - `exec_run("ls /tmp")` works regardless of fs.roots
    (Q3.4c, exec independence).
  - Timeout fires; process killed.
  - Background spawn → list → kill round-trip.
  - Deny-list rejects matching command.
- New testbed suite `workspace-no-project-root`:
  - Tenant created without `[workspace] project_root`.
  - fs and exec plugins start; `${project_root}` falls back
    to `$TABULA_HOME` per M5-04 default.
  - No crash, no surprise behavior.
- Regression coverage: replay previously-tested scenarios
  from the deleted `files`/`shell` testbeds and replay them
  against fs/exec to prove no functional regression.
- Update baseline smoke (M2-08 successor) to invoke fs and exec instead
  of the old skills.

## Acceptance criteria

- [x] Four new testbed suites pass on macOS and Linux CI.
- [x] Regression suite shows fs/exec preserve previously-tested
      behavior.
- [x] `runtime-smoke` updated.
- [x] Generated testbed template files synced.
- [x] Suites use installed bundle path (per AGENTS.md).

## Implementation notes

- `fs-plugin` covers all seven fs tools, outside-root denial, symlink escape
  rejection, and project-root config changes while the plugin worker is warm.
- `workspace-fs-tenant-divergence` creates `alpha` and `beta`, assigns distinct
  workspace roots, verifies same relative names resolve per tenant, and verifies
  alpha files are invisible/outside-root from beta.
- `exec-plugin` covers cwd default, `/tmp` access independent of fs roots,
  timeout, background spawn/list/kill, deny-list behavior, and standalone exec
  operation when `fs` is absent.
- `workspace-no-project-root` verifies default tenant fallback resolves
  `${project_root}` to `$TABULA_HOME` for both fs and exec.
- Baseline smoke now invokes `fs_write`, `fs_read`, and `exec_run` so the core
  runtime smoke path exercises the replacement workspace primitives.
- Generated testbed template and canonical `tabula-distrib/testbed` files are
  kept in sync for fs/exec/code/baseline coverage.

## Validation evidence

- `PYTHONPATH=tools/tabula-testbed/src python3 -m unittest tools.tabula-testbed.src.tabula_testbed_runner.runner_test`
- `PYTHONPATH=_lib/python/src:. python3 -m unittest workspace.exec.tests.test_exec_plugin workspace.fs.tests.test_fs_plugin _lib.python.tests.test_contract` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --suite fs-plugin --suite workspace-fs-tenant-divergence --suite workspace-no-project-root --suite exec-plugin --suite code`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --suite fs-plugin --suite workspace-fs-tenant-divergence --suite workspace-no-project-root --suite exec-plugin --suite code --bootstrap-check --home /tmp/tabula-m506-workspace`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --testbed-dir /Users/mak/src/tabula-distrib/testbed --suite baseline --suite fs-plugin --suite workspace-fs-tenant-divergence --suite workspace-no-project-root --suite exec-plugin --suite code --bootstrap-check --home /tmp/tabula-m506-distrib`

## Blocked by

- M5-05 (legacy bundles gone — testbed must use new bundles
  exclusively, no co-existing skill/plugin shadowing)

## Notes

- Per `tabula/AGENTS.md`: install/fan-out bugs require testbed
  coverage. The fs.roots and template resolver paths are
  classic install-fan-out concerns (config layering across
  tenants).
- After this slice merges, M5 is closed. Onward to M6 (remote
  backends).
