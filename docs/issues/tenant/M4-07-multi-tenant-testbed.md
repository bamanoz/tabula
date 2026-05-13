# M4-07 — Multi-tenant testbed coverage

Status: done
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

- [x] Four new testbed suites pass locally and are ready for macOS and
      Linux CI: `tenant-isolation`, `tenant-config-overlay`,
      `tenant-runtime-whitelist`, and `tenant-installer-fanout`.
- [x] Existing testbed suites pass with explicit tenant_id.
- [x] No cross-tenant data leak in covered tenant-isolation suite.
- [x] Generated testbed template files synced with canonical
      sources.
- [x] `runtime-smoke` (M2-08) extended with one cross-tenant
      assertion.

## Implementation notes

- Added installed testbed suite `tenant-isolation`.
- Added installed testbed suite `tenant-installer-fanout`.
- Added installed testbed suite `tenant-runtime-whitelist`.
- Added installed testbed suite `tenant-config-overlay`.
- Added `testbed_tenant_note`, a deterministic Python cold-worker
  fixture that writes tenant-scoped state under
  `$TABULA_TENANT_DIR/state/skills/testbed-cold-python/`.
- The suite creates tenants `alpha` and `beta`, joins distinct
  tenant sessions, writes tenant-specific notes, verifies `beta`
  cannot read `alpha` state, then verifies physical state files are
  distinct under `tenants/alpha/...` and `tenants/beta/...`.
- The suite checks `tabula status --json` while both tenant sessions
  are active and verifies tenant records, `active_sessions`,
  `runtimes[].tenants_served`, and `capabilities_by_tenant` keys.
- `tenant-installer-fanout` creates tenant `gamma` after install,
  verifies the tenant receives skill and `_lib` runtime surfaces,
  invokes the installed skill in that tenant, re-applies the generated
  distro with `install --update`, and verifies tenant invocation still
  works after refresh.
- `tenant-runtime-whitelist` uses suite metadata to rewrite
  `config/runtime.toml` before kernel start, runs the managed local
  runtime with `tenants = ["alpha"]`, verifies tenant `alpha` can
  execute the installed test skill, and verifies tenant `beta` gets a
  clean `tenant_forbidden` kernel-visible error with no tenant state
  written.
- `tenant-config-overlay` adds a deterministic test fixture plugin that
  returns `load_plugin_config()` state. The suite writes
  `config/global.toml` plus
  `tenants/alpha/config/plugins/testbed-tenant-config/config.toml` and
  verifies `default -> from-global`, `alpha -> from-alpha`.
- Python `load_plugin_config()` now reads tenant-local plugin config
  from `TABULA_TENANT_DIR/config/plugins/<plugin>/config.toml` with
  precedence after root plugin config and before env/explicit values.
- `tabula serve` now preserves an existing explicit runtime tenant
  allowlist when it regenerates managed local runtime config; this was
  required so installed whitelist config survives kernel startup.
- Suite metadata now includes `runtime_tenants`; the runner rejects
  incompatible combinations like wildcard-tenant suites plus the
  whitelist suite in one run.
- The fanout suite exposed and fixed a real installer bug where
  refreshing a tenant whose `_lib` was a symlink to root `_lib` could
  follow the symlink and corrupt root `_lib` symlink targets.
- `M4-07` is complete. Whitelist suites intentionally run separately
  from wildcard-tenant suites because their runtime startup policy is
  mutually incompatible; suite metadata now makes that explicit.

## Validation evidence

- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --bootstrap-check --home /tmp/tabula-m407-tenant-isolation3`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --suite tenant-isolation --bootstrap-check --home /tmp/tabula-m407-baseline-tenant`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-installer-fanout`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-installer-fanout --bootstrap-check --home /tmp/tabula-m407-fanout4`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-runtime-whitelist`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-runtime-whitelist --bootstrap-check --home /tmp/tabula-m407-whitelist2`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-config-overlay`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-config-overlay --bootstrap-check --home /tmp/tabula-m407-config-overlay`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --suite tenant-installer-fanout`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --suite tenant-installer-fanout --bootstrap-check --home /tmp/tabula-m407-tenant-combined`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --suite tenant-installer-fanout --suite tenant-config-overlay`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite tenant-isolation --suite tenant-installer-fanout --suite tenant-config-overlay --bootstrap-check --home /tmp/tabula-m407-wildcard-triple`
- `PYTHONPATH=tools/tabula-distro/src python3 -m unittest tools.tabula-distro.tests.test_install`
- `go test ./cmd/tabula`
- `PYTHONPATH=_lib/python/src python3 -m unittest _lib.python.tests.test_contract` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m unittest tools.tabula-testbed.src.tabula_testbed_runner.runner_test`
- `PYTHONPATH=_lib/python/src python3 -m unittest _lib.python.tests.test_skill_sdk` in `../tabula-bundles`

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
