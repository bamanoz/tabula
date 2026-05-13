# AM-12 — Installed app manifest testbed

Status: done
Type: AFK
Repo: tabula-distrib, tabula
Labels: needs-triage, area/testbed, area/installer, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add canonical installed-layout testbed coverage for runnable app manifests.

Focused unit tests already cover parser/materializer/bindings. This issue adds
installed behavior checks using generated `TABULA_HOME` and actual installed
tools/plugins.

Scenarios:

- Apply and run a repo-local `claw` `tabula.app.toml`.
- Assert tenant id equals `application.id`.
- Assert tenant surface exists and runtime config points at tenant plugin/skill
  dirs.
- Start kernel and managed runtime from `app run`.
- Launch `tabula-cli --expected-distro-id tabula.claw` from the bound directory and verify join uses the app
  tenant.
- Execute `fs` and `exec` tools through the installed runtime.
- Verify memory defaults to tenant-local state.
- Verify shared memory can be configured through `.tabula/local.toml`.
- After AM-09, run two apps in different directories against one kernel and
  verify tool catalogs and state isolation.

## Acceptance criteria

- [x] New testbed suite runs from a clean temporary `TABULA_HOME`.
- [x] Testbed executes installed `tabula-distro app run` or `tabula-install app
      run`.
- [x] Testbed executes installed `tabula-cli --expected-distro-id tabula.claw`.
- [x] Testbed executes actual installed plugins, not just catalog checks.
- [x] Generated testbed template and canonical testbed files remain in sync.

## Blocked by

- AM-01 through AM-08 for single-app coverage.
- AM-09 for concurrent multi-app coverage.
