# AM-07 — App-scoped runtime surface and catalogs

Status: proposed
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/kernel, area/tenancy, area/installer

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Support multiple application instances, potentially from different distros,
without sharing one global active runtime surface.

Current blockers:

- `TABULA_BOOT` points to one global boot script.
- `$TABULA_HOME/boot.py` points at `$TABULA_HOME/distrib/active/boot.py`.
- `$TABULA_HOME/plugins`, `skills`, `templates`, `clients`, `_lib` are global
  fan-out surfaces.
- `$TABULA_HOME/config/runtime.toml` contains one global `plugin_dirs` set.
- Kernel tool registry and init metadata are global, while sessions are
  tenant-bound.

Target behavior:

```text
session -> tenant/app id -> app-scoped distro generation -> app-scoped plugin catalog and boot metadata
```

Components:

- Installer materializes app-scoped distro surfaces under
  `$TABULA_HOME/tenants/<app-id>/`.
- Runtime manifest store loads plugin/skill dirs per tenant/app or otherwise
  namespaces target catalogs by tenant.
- Managed runtimes can start from installer-generated runtime config that points
  each served app tenant at its app-scoped plugin/skill dirs.
- Kernel init tool catalog is filtered by session tenant/app.
- Kernel boot metadata is tenant/app-aware.
- Reload trigger can refresh one app or all apps without assuming one global
  active distro.
- Warm worker pool key already includes tenant id; verify target id collisions
  across apps are isolated.
- Runtime `Hello.TenantsServed` remains the enforcement surface for which app
  tenants a runtime can serve.

## Acceptance criteria

- [ ] Two app tenants can have different plugin config and state at the same
      time.
- [ ] Two app tenants can expose different plugin catalogs without tool leakage
      in `init.tools`.
- [ ] Two app tenants can come from different distro generations without sharing
      global `$TABULA_HOME/plugins`.
- [ ] Runtime reload can refresh one app's plugin surface without evicting
      unrelated app workers unless necessary.
- [ ] `tabula status --json` shows app/tenant catalog information without
      breaking existing tenant fields.
- [ ] Testbed proves two apps in two project directories route to different
      tool surfaces and state roots.

## Blocked by

- AM-01 for app install metadata.
- AM-04 for clients joining with app tenant ids.

## Notes

- Do not expose distro semantics in kernel types. Use tenant/app ids and target
  catalogs, not workspace/project concepts.
- This is the slice that makes different distros concurrently viable. Earlier
  slices can support multiple instances only within current global surface
  constraints.
