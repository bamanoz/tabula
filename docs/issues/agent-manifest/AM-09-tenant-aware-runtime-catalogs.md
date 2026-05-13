# AM-09 — Tenant-aware runtime catalogs and app boot metadata

Status: done
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/kernel, area/tenancy, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Finish the core To Be architecture for multiple app tenants in one kernel.

Current limitation:

- `app run` can point the runtime at one selected tenant's plugin/skill surface.
- The runtime catalog is still effectively global for that runtime process.
- Kernel init tools and boot metadata are not selected per session tenant.
- Different app tenants with duplicate plugin ids cannot safely share one global
  runtime catalog.

Target behavior:

```text
session tenant_id -> app-scoped boot metadata -> app-scoped runtime catalog -> runtime serving that tenant
```

Required changes:

- Runtime manifest store supports catalog namespaces by tenant/app id.
- Runtime capability advertisements include tenant/app scope.
- Duplicate plugin ids are allowed across different tenant catalogs.
- Kernel routes `init.tools` by the joining session's tenant id.
- Kernel routes tool calls by `session.tenant_id + tool/target`.
- Kernel stores app boot metadata per tenant/app, not one global boot meta.
- `ReloadPlugins` can reload one tenant/app catalog without evicting unrelated
  app workers.
- Status output shows capabilities grouped by tenant/app.

## Acceptance criteria

- [x] One kernel can serve two app tenants at the same time.
- [x] The two tenants can expose different tool catalogs.
- [x] The same plugin id can exist in both tenants without duplicate-id errors.
- [x] A client joined to tenant A receives only tenant A tools in init.
- [x] A client joined to tenant B receives only tenant B tools in init.
- [x] Tool invocation for tenant A cannot reach tenant B's plugin workers.
- [x] Boot metadata such as `prompt_builder` and workspace meta is selected by
      tenant/app.
- [x] Reloading tenant A does not evict tenant B workers unless explicitly
      requested.

## Blocked by

- AM-01 through AM-07.

## Notes

- Do not introduce workspace/project semantics into kernel types. Use tenant/app
  ids and catalogs.
- This is the key missing piece for concurrent different distros in one kernel.

## Progress

- Kernel runtime dispatch is tenant-scoped: runtime tools are keyed by
  tenant/app id and tool name, and init tool lists are filtered by the joining
  session's tenant.
- Runtime tool calls now invoke the runtime that owns the selected catalog entry,
  rather than always invoking the tenant default runtime.
- Runtime attachment tracks `tenants_served`; tenant runtime allowlists and
  default runtime bindings are loaded from tenant config.
- Tenant-local init metadata is loaded from tenant config and overrides global
  boot metadata for fields such as `prompt_builder` and workspace meta.
- Runtime manifest stores can now hold tenant/app-scoped indexes; duplicate
  plugin ids are valid across different tenant catalogs, capabilities carry
  tenant scope, and worker pool lookup uses `(tenant_id, target_id)`.
- Runtime reload requests carry tenant/app scope, tenant-scoped reload triggers
  are written by the installer, and runtime daemon reloads only the requested
  tenant catalog/workers when scope is present.
