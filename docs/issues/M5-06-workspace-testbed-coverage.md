# M5-06 — Workspace decomposition testbed coverage

Status: open
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

- New testbed suite `workspace-fs-roots`:
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
- New testbed suite `workspace-exec`:
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
- Regression suite: pick three previously-failing scenarios
  from the deleted `files`/`shell` testbeds and replay them
  against fs/exec to prove no functional regression.
- Update `runtime-smoke` (M2-08) to invoke fs and exec instead
  of the old skills.

## Acceptance criteria

- [ ] Four new testbed suites pass on macOS and Linux CI.
- [ ] Regression suite shows fs/exec preserve previously-tested
      behavior.
- [ ] `runtime-smoke` updated.
- [ ] Generated testbed template files synced.
- [ ] Suites use installed bundle path (per AGENTS.md).

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
