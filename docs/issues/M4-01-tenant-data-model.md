# M4-01 — Tenant data model + on-disk layout

Status: open
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5)

## What to build

The foundational data structure: what a tenant **is** in code,
how tenants live on disk, and how the kernel boots a list of
them. No CLI, no per-tenant overlays, no runtime-side
enforcement yet — those are M4-02..05.

Components:

- `internal/kernel/tenant/tenant.go`:
  - `type Tenant struct { ID string; CreatedAt time.Time;
    DisplayName string; ... }`.
  - ID rules: `^[a-z0-9][a-z0-9-]{0,62}$`. Reserved: `default`,
    `system`, `runtime`, `kernel`, `admin`. Reserve a few more
    to be safe.
  - Validation function returns structured errors.
- `internal/kernel/tenant/store.go`:
  - `Store` interface: `List() []Tenant`, `Get(id) (Tenant, ok)`,
    `Create(t) error`, `Delete(id) error`.
  - Default impl: filesystem-backed. Each tenant lives at
    `$TABULA_HOME/tenants/<id>/`.
  - Tenant root contains `tenant.toml` with metadata.
- On-disk layout (under each `tenants/<id>/`):
  ```
  tenant.toml                        # metadata
  config/
    tenant.toml                      # tenant-scoped overlay
    plugins/<plugin-id>/config.toml  # per-tenant plugin config
  state/                             # tenant-scoped runtime state
    plugins/<plugin-id>/...
    skills/<skill-name>/...
    sessions/...
  cache/                             # tenant-scoped cache
  logs/                              # tenant-scoped logs
  skills/                            # symlinks (M4-04 fans out)
  plugins/                           # symlinks (M4-04 fans out)
  clients/                           # symlinks (M4-04 fans out)
  templates/                         # symlinks (M4-04 fans out)
  _lib/                              # shared, may symlink to global
  ```
- `$TABULA_HOME` top-level layout (after this slice):
  ```
  bin/                  # tabula, tabula-runtime
  config/               # global config (existing)
  generations/          # installer generations (existing)
  run/                  # sockets, pidfiles (existing)
  state/                # global runtime state
  tenants/              # NEW: per-tenant subtrees
    <id>/...
  ```
- Migration shim (allowed per AGENTS.md "concrete migration
  requirement"):
  - On `tabula serve` boot, if `$TABULA_HOME` has the legacy
    flat layout (`skills/`, `plugins/` at root, no `tenants/`
    dir), automatically promote it: create
    `tenants/default/`, move per-tenant directories into it,
    leave `_lib`, `config`, `bin`, `generations`, `run` at
    root.
  - Migration runs once per install, marker file
    `tenants/.migrated_v1` to prevent re-running.
  - Document the shim's removal milestone (M5? M6?). Mark it
    with a TODO + scheduled-removal milestone in the source.
- Boot sequence: kernel reads `tenants/` directory; if empty
  after migration, auto-create a `default` tenant.

Out of scope:
- CLI (`tabula tenant create`) → M4-02.
- Routing tools at session-init by tenant → M4-03.
- Installer fan-out into per-tenant trees → M4-04.
- Runtime-side enforcement → M4-05.

## Acceptance criteria

- [ ] `Tenant` and `Store` types exist with full godoc.
- [ ] ID validation rejects illegal characters and reserved
      names with structured errors.
- [ ] Filesystem store creates the documented layout with
      correct permissions: `tenants/` dir `0700`, per-tenant
      dirs `0700`, `tenant.toml` `0600`.
- [ ] Boot on a fresh `$TABULA_HOME`: creates
      `tenants/default/`.
- [ ] Boot on a legacy `$TABULA_HOME` (synthesized in test):
      migrates layout, creates `default` tenant, marker file
      written.
- [ ] Boot on already-migrated `$TABULA_HOME`: no-op,
      tenants enumerated.
- [ ] Race-clean.
- [ ] Tests cover create / list / delete / migrate flows.

## Blocked by

- Nothing in M4. Depends on the broader runtime program only
  insofar as kernel runtime API plumbing exists (M2-07).

## Notes

- Per ADR §5, kernel multi-tenancy is greenfield in M4. Until
  this slice lands, kernel is implicitly single-tenant.
- Migration shim is a deliberate exception to "no legacy"
  per `tabula/AGENTS.md`: on-disk layout migration is a
  concrete migration requirement.
- The `tenants/` directory is separate from
  `state/sessions/` — tenant != session. Sessions live inside
  a tenant.
- Quotas, ACLs, and per-tenant resource limits are NOT in
  scope. Tenant is an isolation namespace, not a billing /
  authz model. Document this in the package godoc.
