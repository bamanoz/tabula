# M4-07 — Multi-tenant testbed coverage

Status: open
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/testbed, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5)

## What to build

Testbed suites that exercise multi-tenant behavior under
installed layout. Per `tabula/AGENTS.md` "Tests" rule,
isolation-class bugs require testbed coverage.

Components:

- New testbed suite `tenant-isolation`:
  - Create two tenants (`alpha`, `beta`).
  - Start a session in each; invoke a stateful skill (e.g.
    `memory-save` then `memory-search`) in each.
  - Assert: writes from `alpha` not visible to `beta` and
    vice versa.
  - Assert: state directories are physically distinct
    (`tenants/alpha/state/skills/memory/...` vs
    `tenants/beta/state/skills/memory/...`).
- New testbed suite `tenant-config-overlay`:
  - Set a plugin config in global config to value A.
  - Override in `tenants/alpha/config/plugins/foo/config.toml`
    to value B.
  - Invoke the plugin from a session in each tenant.
  - Assert: tenant `default` sees A, tenant `alpha` sees B.
- New testbed suite `tenant-runtime-whitelist`:
  - Configure runtime with `tenants = ["alpha"]`.
  - Invoke as `alpha` → success.
  - Invoke as `beta` → `tenant_forbidden`.
  - Verify error reaches kernel and is surfaced cleanly to
    caller.
- New testbed suite `tenant-installer-fanout`:
  - Install a generation.
  - Create new tenant `gamma` after install.
  - Invoke a plugin tool in `gamma`'s session.
  - Assert: tool works (fan-out happened on tenant create).
  - Run `tabula-distro update` to a new generation.
  - Re-invoke in `gamma` → uses updated bundle.
- Update existing single-tenant testbed suites to explicitly
  pass `tenant_id = "default"` rather than relying on it
  being implicit. Catches sloppy plumbing.
- Run all M2/M3 testbed suites against a multi-tenant
  configuration (one default tenant + one extra) to confirm
  no regressions.
- Sync canonical testbed files with generated template files.

## Acceptance criteria

- [ ] Four new testbed suites pass on macOS and Linux CI.
- [ ] Existing testbed suites pass with explicit tenant_id.
- [ ] No cross-tenant data leak in any suite.
- [ ] Generated testbed template files synced with canonical
      sources.
- [ ] `runtime-smoke` (M2-08) extended with one cross-tenant
      assertion.

## Blocked by

- M4-01..06 (full M4 stack must land before testbed can
  exercise it)

## Notes

- Per `tabula-bundles/AGENTS.md` Testbed Coverage: tests must
  exercise installed behavior, not just import source files.
  These suites use the real installer fan-out from M4-04.
- This issue closes M4. After it merges, the kernel is
  multi-tenant in production — distros can ship multi-project
  setups.
