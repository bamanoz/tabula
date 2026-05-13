# AM-14 — Test distro values materializer

Status: done
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/testbed, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add a generic installed-layout tracer bullet using a generated test distro whose
application materializer maps opaque `[values]` into tenant-local plugin and
client config.

This proves the installer invokes the distro contract and that runtime plugins
consume the resulting tenant config without introducing `claw` policy into the
kernel or generic installer.

## Acceptance criteria

- [x] Test manifest includes opaque `[values]`.
- [x] Test distro materializer writes
      `tenants/<app>/config/plugins/<plugin-id>/config.toml`.
- [x] `tabula-install app run` invokes the materializer.
- [x] Live testbed executes installed plugins using materialized config.
- [x] Kernel remains unaware of workspace/project/claw semantics.

## Blocked by

- AM-13.

## Notes

- This issue should replace any test-only manual plugin config writes in the
  app-manifest testbed path.

## Progress

- Generated testbed distro now declares `[application_contract]` and installs a
  `materialize_app.py` fixture.
- The fixture maps opaque `[values.workspace]` into tenant-local `fs` and `exec`
  plugin config for test coverage.
- The app-manifest testbed no longer writes plugin config manually.
- App apply/run touches a tenant-scoped reload trigger after materializer output
  so live runtimes pick up tenant config changes.
