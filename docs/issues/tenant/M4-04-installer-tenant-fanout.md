# M4-04 — Per-tenant installer fan-out

Status: done
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5, §10)

## What to build

Update `tabula-distro` installer to fan installed bundles into
each tenant's surface. After this slice every tenant has its
own `skills/`, `plugins/`, `clients/`, `templates/`,
populated by the active generation.

Per `tabula/AGENTS.md` "Installer And Artifacts": installer is
the source of truth for runtime layout.

Components:

- `tabula/tools/tabula-distro/src/tabula_distro/install.py`:
  - Existing `_refresh_runtime_surface` symlinks active
    generation into `$TABULA_HOME/skills/` etc.
  - New: enumerate `$TABULA_HOME/tenants/*/`. For each tenant:
    - Recreate `tenants/<id>/skills/` symlink to the active
      generation's `skills/`.
    - Same for `plugins/`, `clients/`, `templates/`.
    - `_lib` is shared; symlink to global `$TABULA_HOME/_lib/`.
  - Atomic touch `$TABULA_HOME/run/reload.touch` after
    refresh (existing behavior — keep).
- New tenant created by `tabula tenant create` (M4-02):
  - On creation, fan out the active generation's surface into
    the new tenant. (Tenant create calls into installer's
    `refresh_tenant(id)` helper, exposed for this purpose.)
- Per-tenant config templates: when creating a tenant, copy
  `config/global.toml.example` etc. into
  `tenants/<id>/config/` as starting templates if they don't
  exist. Existing files are not overwritten.
- Generation switch / update / rollback:
  - Each operation re-runs fan-out for every tenant.
  - Tenants added between the operation start and end are
    handled on next refresh.
- `tabula-distro` CLI:
  - No new subcommands. Existing `install`, `update`,
    `rollback`, `list`, `lock`, `gc` operate over all
    tenants implicitly.
  - Optional `--tenant <id>` filter on `install` to refresh
    just one tenant (useful for tenant-scoped reinstall).
- Documentation update in `tabula/docs/` (whichever doc
  describes the installer): note per-tenant fan-out behavior.

### Subtle decision

Q: should bundle code be physically duplicated per tenant, or
shared via symlinks?

Decision: **symlinks** to the global generation surface. Same
binary, same on-disk skill/plugin code. Only state, config,
and cache are tenant-scoped.

Rationale:
- Disk usage stays linear in tenants × number of bundles only
  for state, not for code.
- Updating a generation = single global swap; tenants pick up
  immediately on reload.
- Per-tenant patching is explicitly NOT a goal in M4. Use
  per-tenant config overlays (M4-03) to vary behavior.

## Acceptance criteria

- [x] `tabula-distro install` fans the active generation into
      every existing tenant.
- [x] `tabula tenant create foo` produces a tenant with its
      `skills/`, `plugins/`, etc. symlinks already populated.
- [x] `tabula-distro update` swaps generation; all tenants
      see the new surface after reload.
- [x] `--tenant <id>` filter on install refreshes just that
      tenant.
- [x] No physical duplication of bundle code (verify via test:
      tenant-A's `skills/timer/SKILL.md` and tenant-B's
      `skills/timer/SKILL.md` are the same inode or both
      symlink to the same target).
- [x] Atomic reload trigger fires after fan-out (existing
      behavior preserved).
- [x] Race-safe: parallel fan-out of two tenants doesn't
      corrupt either.
- [x] Testbed coverage: install → create-tenant →
      install-again → invoke skill in both tenants → both
      work independently with separate state.

## Implementation notes

- `tabula-distro install()` now accepts optional `tenant=` and the CLI
  exposes `tabula-distro install --tenant <id>` to refresh only one
  tenant surface while preserving the normal generation install/update
  flow.
- Runtime surface refresh fans root and tenant surfaces through the same
  `_link_runtime()` helper for `skills/`, `plugins/`, `clients/`,
  `templates/`, and `_lib/`.
- `tabula tenant create` now does two follow-up steps after creating the
  tenant record:
  - fan out the active runtime surface into `tenants/<id>/...`
  - seed `tenants/<id>/config/**/*.example` from root `config/**/*.example`
    without overwriting existing tenant files
- Installer refresh now safely replaces tenant destination symlinks with
  real directories before relinking, which fixed the tenant `_lib`
  corruption bug caught by M4-07 fanout coverage.
- `docs/ARCHITECTURE.md` now documents tenant fan-out under the active
  distro layout.

## Validation evidence

- `PYTHONPATH=tools/tabula-distro/src python3 -m unittest tools.tabula-distro.tests.test_install`
- `go test ./cmd/tabula`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-installer-fanout --bootstrap-check --home /tmp/tabula-m404-fanout`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --suite tenant-installer-fanout --suite tenant-config-overlay --bootstrap-check --home /tmp/tabula-m407-wildcard-triple`

## Blocked by

- M4-01 (tenant directories exist to fan into)
- M4-03 (per-tenant config layout settled)

## Notes

- This change is the "tenant got real" moment for the
  installer. After this, `bootstrap.sh` (M4-06) can wire
  tenant create + install + serve into a coherent flow.
- Skill / plugin code being shared across tenants is safe
  because workers run in distinct OS processes per tenant
  (M2-03 isolation). The shared code is read-only at runtime.
