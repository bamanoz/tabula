# M5-04 — Per-tenant workspace config (`fs.roots`, `exec.cwd_default`, `${project_root}`)

Status: done
Phase: M5
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, area/tenancy, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5)

## What to build

The kernel/runtime side of moving "where is the project root"
from a global env var (`Hub.ProjectRoot` from
`TABULA_PROJECT_ROOT`) to per-tenant config templates resolved
at plugin-config-load time.

Components:

- Per-tenant `tenant.toml` schema gains a `[workspace]`
  section:
  ```toml
  [workspace]
  project_root = "/Users/me/src/myproject"
  ```
- Config template resolver (lives in
  `tabula_plugin_sdk.config`, M2-04 SDK):
  - `${project_root}` resolves from the per-tenant
    `[workspace] project_root`.
  - `${tenant_id}` resolves from `$TABULA_TENANT_ID`.
  - `${tabula_home}` resolves from `$TABULA_HOME`.
  - Unknown `${...}` → load-time error with structured
    message; do NOT silently leave the literal string.
- Where the resolver runs:
  - Plugin SDK's `load_plugin_config()` substitutes templates
    after merging global + tenant config (M4-03 layering).
  - Worker process resolves at startup (cold harnesses) or
    on each Reload (warm plugins).
  - Kernel does NOT pre-resolve templates before sending —
    the resolver lives at the consumer (worker), so each
    consumer sees its own tenant context. This avoids
    embedding tenant context into wire types.
- Add `tabula tenant set <id> --workspace-root <path>` (or
  similar) subcommand to `tabula tenant` CLI (M4-02), writing
  `[workspace] project_root` into the tenant.toml. Or — keep
  the CLI minimal and require users to edit tenant.toml
  directly. Decide in PR review; recommend the CLI for
  ergonomics, given bootstrap.sh will use it.
- Bootstrap.sh update (`tabula/scripts/bootstrap.sh`, M4-06):
  - Reads `project.root` from `tabula.project.toml` (existing
    project metadata file) — falls back to PWD if not set.
  - Calls `tabula tenant set "$tenant_id" --workspace-root
    "$project_root"`.
- Delete `Hub.ProjectRoot` field from kernel:
  - `internal/kernel/kernel.go`: remove field, remove
    constructor param.
  - `cmd/tabula/main.go:266`: remove env-var read.
  - `internal/kernel/join_flow.go:82`: stop emitting
    `init.meta.project_root`. Document the breaking change.
- Update `internal/kernel/project_root_test.go`: delete or
  rewrite as a `tenant.workspace.project_root` config test.

### Q: do we need `init.meta.project_root` for any client?

Audit before deleting: does the TUI / any client consume
`init.meta.project_root` today? If yes, the deletion needs
either:
1. A migration path (clients read tenant config directly via
   a new kernel op), or
2. Replacing it with `init.meta.tenant.workspace.project_root`
   in the same change.

Pick whichever is smaller. Default recommendation: clients
that need to know project root ask the kernel via a new
`Tenant.Get` op exposed through the existing init handshake's
`tenant_context` field. Add the field if missing.

## Acceptance criteria

- [x] `[workspace] project_root` recognised in tenant.toml.
- [x] Plugin SDK template resolver expands `${project_root}`,
      `${tenant_id}`, `${tabula_home}`.
- [x] Unknown template variable → structured load error.
- [x] `Hub.ProjectRoot` field deleted; no compile-time
      references.
- [x] `TABULA_PROJECT_ROOT` env var no longer read by kernel.
- [x] `init.meta.project_root` either removed or migrated
      (audit decision recorded in PR).
- [x] `tabula tenant set --workspace-root` works (or
      documented manual `tenant.toml` edit path).
- [x] Bootstrap.sh sets workspace_root for the project tenant.
- [x] Single-tenant `default` keeps working — its
      `tenant.toml` gets `project_root = "${TABULA_HOME}"` as
      a sensible default if unset.
- [x] Tests cover template resolution, missing-variable
      errors, per-tenant divergence (two tenants with
      different roots).

## Implementation notes

- Added `tabula tenant set <id> --workspace-root <path>` writing
  `[workspace] project_root` to
  `$TABULA_HOME/tenants/<id>/config/tenant.toml`.
- `bootstrap.sh` now sets the project/default tenant workspace root from
  `tabula.project.toml` `project.root`, falling back to the current
  working directory.
- Python plugin config loading now resolves workspace templates from the
  tenant-local `tenant.toml`.
- `fs-plugin` installed coverage uses `roots = ["${project_root}"]` and
  changes the workspace root while the warm plugin is running.
- Removed the kernel-owned workspace root surface. `Hub.ProjectRoot`, the
  kernel `TABULA_PROJECT_ROOT` read, and `init.meta.project_root` are gone.
  Audit found no driver/client reads of `init.meta.project_root`; remaining
  `TABULA_PROJECT_ROOT` reads are legacy workspace bundles tracked by M5-03
  and M5-05, not kernel clients.
- `PrepareBootLayout` and `tabula tenant create default` now materialize
  `[workspace] project_root = "${TABULA_HOME}"` for the default tenant when
  unset, preserving any explicit value.
- The Python SDK expands `${TABULA_HOME}` and `${tabula_home}` inside the stored
  `[workspace] project_root` value before resolving `${project_root}`.

## Validation evidence

- `go test ./cmd/tabula`
- `go test ./internal/kernel ./internal/tenant ./cmd/tabula`
- `PYTHONPATH=_lib/python/src:. python3 -m unittest _lib.python.tests.test_contract workspace.fs.tests.test_fs_plugin` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --bootstrap-check --home /tmp/tabula-m504-fs-reload`

## Blocked by

- M4-03 (per-tenant config overlay machinery)
- M5-01, M5-02 (fs and exec plugins consume `${project_root}`)

## Notes

- `Hub.ProjectRoot` is currently the ONLY workspace-related
  field in the kernel (per discoveries). After this slice the
  kernel is workspace-agnostic — it just routes Invokes;
  workspace lives in plugin config layered with tenant config.
- Default tenant's `project_root` falling back to
  `$TABULA_HOME` is a deliberate non-decision: most
  global/system tools don't care; project-aware tools (fs,
  exec) get a sane root. If review prefers a fail-loud
  default ("must set workspace_root explicitly"), flip it.
- Distros (tabula-distrib/claw) own the policy of which
  variables to expose; this slice only adds the resolver
  primitives.
