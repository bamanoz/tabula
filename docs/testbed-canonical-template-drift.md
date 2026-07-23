# Testbed canonical ↔ template drift

Status: open. Documented after the `exec_run = ask` hang workstream
(2026-05-22). Not addressed in that workstream because resolution requires
intent decisions from whoever owns the testbed runner contract.

## Rule

Both `tabula/AGENTS.md` and `tabula-distrib/AGENTS.md` require canonical
testbed files (in `tabula-distrib/testbed/`) and the generated testbed template
shipped with the runner (in
`tabula/tools/tabula-testbed/src/tabula_testbed_runner/testbed_template/`) to
be kept in sync.

## Current state

The packaged template is a **superset** of the canonical distro testbed and
has diverged in non-trivial ways.

### Suite-level drift

Template-only suites (not present in `tabula-distrib/testbed/testbed.toml`):

- `plugin-concurrency`
- `plugin-failure-modes`
- `plugin-fixture-execution`
- `runtime-mtls-loopback`
- `runtime-multi-backend`
- `runtime-ssh-loopback`
- `runtime-token-revoke`
- `runtime-wss-loopback`
- `tenant-config-overlay`
- `tenant-installer-fanout`
- `tenant-isolation`
- `tenant-runtime-whitelist`

Canonical and template agree on the rest (acp, approvals, ask-user, baseline,
code, compaction, cron, exec-plugin, fs-plugin, mcp, memory, sessions,
subagents, timer, todo, workspace-fs-tenant-divergence,
workspace-no-project-root).

### Test-file drift

Files only in template (under `tests/`):

- `test_plugin_fixture_execution.py`
- `test_runtime_remote_backends.py`
- `test_skill_concurrency.py`
- `test_skill_failure_modes.py`
- `test_tenant_config_overlay.py`
- `test_tenant_installer_fanout.py`
- `test_tenant_isolation.py`
- `test_tenant_runtime_whitelist.py`

Files only in canonical:

- `test_driver_config.py`

Files that exist in both but **differ**:

- `tests/smoke.py`
- `tests/test_compaction.py`
- `tests/test_cron.py`
- `tests/test_mcp.py`
- `tests/test_sessions.py`
- `tests/test_timer.py`
- `tests/test_workspace_fs_tenant_divergence.py`
- top-level `generate.py`
- top-level `testbed.toml`

### Example of semantic drift (not just whitespace)

`tests/test_compaction.py` provisions the driver differently:

- Canonical writes `$TABULA_HOME/config/global.toml` with
  `[clients.driver.providers.openai] api_key = "test-key"` etc. and runs
  driver with `--no-tenant-binding`.
- Template sets `TABULA_PLUGIN_DRIVER_OPENAI_API_KEY=test-key` and
  `TABULA_PLUGIN_DRIVER_OPENAI_MODEL=o3` via env, and runs driver without
  `--no-tenant-binding`.

Both surfaces exist in current code: `tabula-bundles/drivers/driver/run.py`
defines `--no-tenant-binding`, and
`tabula-bundles/drivers/driver/app.schema.toml` declares
`TABULA_PLUGIN_DRIVER_OPENAI_API_KEY`. The two test variants test *different*
provisioning paths.

## Why this PR/workstream did not resolve it

The `exec_run = ask` workstream is scoped to the kernel-level exchange-cancel
fix and the `code-immune` test that depended on it. The drift here long
predates that work (last shared sync was commit `7ab059c`/`452e6e1` on
2026-05-16, "sync ACP testbed template coverage"). Resolving it requires:

1. Deciding the **direction of truth** per file (canonical → template, or
   template → canonical, or merge).
2. Validating that template-only suites still work against current
   `tabula-bundles` (some fixtures may have moved or been deleted in
   `tabula-bundles/test-fixtures`).
3. A full testbed run after the merge.

That is a separate, focused PR.

## Suggested resolution shape

- Pick canonical (`tabula-distrib/testbed/`) as the source of truth for
  distro-level suites (matches the "distro policy lives in distro" rule).
- Move the runtime/runner/tenant/fixture-execution test suites either:
  - into `tabula-distrib/testbed/` if they are conceptually distro testbed
    coverage; or
  - into focused tests inside `tabula/tools/tabula-testbed` or under
    `tabula/internal/...` integration tests if they exercise runner /
    kernel infrastructure, not distro policy.
- After moving, delete the duplicated content from the loser side so the
  rule "Keep canonical testbed files and the generated testbed template in
  sync" can hold with `diff -rq` returning nothing meaningful (ignoring
  `__pycache__`).
- Decide whether `test_compaction.py` drift is a deliberate two-mode
  coverage or an accidental fork; collapse to one.

## Verification once merged

Minimum:

```
go test ./internal/kernel/... -count=1
tabula-testbed run --suite baseline    --testbed-dir <chosen-canonical>
tabula-testbed run --suite exec-plugin --testbed-dir <chosen-canonical>
diff -rq --exclude=__pycache__ \
  tabula-distrib/testbed \
  tabula/tools/tabula-testbed/src/tabula_testbed_runner/testbed_template
```

The last command should return nothing except for cases where the template
intentionally carries `__init__.py` packaging shims absent in canonical.
