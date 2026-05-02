# M4-08 — Kernel-side runtime registry config

Status: open
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, area/runtime, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
Amendment: AMENDMENTS.md C4

## What to build

Resolve the M2-M5 gap where the kernel needs to know about
multiple runtimes (for multi-backend, multi-tenant routing)
but no issue defined the configuration surface.

Per AMENDMENTS C4:

- Kernel-side global config holds the runtime registry.
- Per-tenant config holds the binding whitelist.

Components:

- `$TABULA_HOME/config/global.toml` schema extension:
  ```toml
  [kernel]
  id = "main"                     # default: hostname

  [[runtime]]                     # singular, list-of-tables
  id      = "local"
  backend = "local"               # local | attach | ssh | wss
  # backend-specific:
  #   local: (no fields; managed child)
  #   attach: socket = "..." or url = "..."
  #   ssh:    host = "user@host", command = "tabula-runtime stdio",
  #           token = "...", ssh_args = [...]
  #   wss:    url = "wss://...", token = "...", ca_file, cert_file, key_file
  ```
- `$TABULA_HOME/tenants/<id>/tenant.toml` schema extension:
  ```toml
  [tenant]
  allowed_runtimes = ["*"]          # default: any runtime
  default_runtime  = "local"        # required if more than one runtime

  [workspace]
  project_root = "..."              # M5-04 lands this section
  ```
- `internal/kernel/runtime/registry/`:
  - Reads `[[runtime]]` entries from global.toml at boot.
  - Validates ids (per AMENDMENTS M10 reuse tenant_id rule).
  - Stores in registry keyed by id.
  - On reload (`run/reload.touch`), rereads.
- `Hub.Pick(tenant_id) -> RuntimeConn`:
  - Resolves the tenant's `default_runtime`.
  - Validates it's in `allowed_runtimes` (or wildcard).
  - Returns the registry's connection.
  - Errors: `runtime_unavailable` if registry has no live
    connection; `tenant_forbidden` if default_runtime not in
    allowed_runtimes.
- Validation: kernel boot fails fast on:
  - Duplicate runtime ids.
  - Tenant referencing a runtime id not in registry.
  - Reserved id used (`kernel`, `system`, `admin`).

### M2 backward shape

In M2 the kernel runs with one implicit `local` runtime.
This slice formalizes it: M2 emits a synthetic
`[[runtime]] id = "local" backend = "local"` entry at boot
if `global.toml` has none. Tenants without
`allowed_runtimes` default to `["*"]`. Single-tenant default
ships with `default_runtime = "local"`.

## Acceptance criteria

- [ ] `[[runtime]]` parsed from global.toml; backend field
      validated.
- [ ] `[tenant] allowed_runtimes` and `default_runtime`
      parsed from tenant.toml.
- [ ] `Hub.Pick(tenant_id)` returns the right runtime.
- [ ] Duplicate runtime id at boot → fail fast.
- [ ] Tenant referencing unknown runtime id → fail at tenant
      load (not at routing time).
- [ ] M2-style single-runtime install (no `[[runtime]]` in
      global.toml) auto-synthesizes `local` and continues
      working.
- [ ] Reload picks up registry changes without restart.
- [ ] Race-clean.

## Blocked by

- M2-07 (kernel has a runtime to register at all)
- M4-01 (tenant data model)
- M4-03 (per-tenant config plumbing)

## Blocks

- M4-05 (runtime-side enforcement assumes the registry knows
  per-runtime tenant whitelists from this side)
- M6-01, M6-02, M6-03 (each adds a backend type to this
  registry)

## Notes

- This issue formalizes what M2's atomic cutover treated
  implicitly. Without it, M6-03's SSH backend has nowhere
  to live at the config layer.
- Naming: `[[runtime]]` (singular, list-of-tables form)
  intentionally matches `[[kernel]]` on the runtime side.
  Symmetric: each side configures the other in its own file
  with its own list.
