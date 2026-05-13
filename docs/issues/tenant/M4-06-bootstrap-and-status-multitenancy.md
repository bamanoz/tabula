# M4-06 — Bootstrap script + tabula status update for tenants

Status: done
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/cli, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5, §9)

## What to build

Update `bootstrap.sh` (M2-07) and `tabula status` (M2-06) for
real multi-tenant world.

Components:

- `tabula status --json` shape extension:
  ```json
  {
    "kernel": {...},
    "runtimes": [
      {
        "id": "local",
        "attached": true,
        "tenants_served": ["default", "myproject"],
        "capabilities_by_tenant": {
          "default": ["fs", "exec", ...],
          "myproject": ["fs", "exec", "gateway-telegram"]
        }
      }
    ],
    "tenants": [
      {
        "id": "myproject",
        "display_name": "My Project",
        "created_at": "...",
        "active_sessions": 2
      }
    ]
  }
  ```
- Shape evolution rule (documented in M2-06): old fields stay,
  new fields additive. Old shell consumers (M2-07 bootstrap)
  must keep working with old `jq` selectors.
- `bootstrap.sh` flow:
  1. Read `tabula.project.toml` → `project.name` (becomes
     tenant id) and `kernel.mode`.
  2. `tabula tenant create "$project_name" --display-name
     "$display_name" --exists-ok` (M4-02).
  3. Local mode: `tabula serve &`, poll `tabula status --json`
     until `kernel.running == true && len(runtimes) > 0 &&
     "$project_name" in tenants_served`.
  4. Print `ready: tenant=$project_name`.
- New script `scripts/tenant-init.sh` for explicit tenant
  creation outside of project bootstrap. Same primitives,
  thinner workflow.
- `tabula tenant list --json` consumed by various README
  examples — update docs accordingly.
- Shell helpers (`scripts/lib.sh` if a convention exists)
  for "wait until tenant ready" polling primitive, reusable
  by future scripts.

## Acceptance criteria

- [x] `tabula status --json` returns documented shape.
- [x] `bootstrap.sh` end-to-end on fresh `$TABULA_HOME`:
      - Creates project tenant.
      - Starts kernel + runtime.
      - Waits for tenant to appear in
        `runtimes[].tenants_served`.
      - Exits 0 within 15s.
- [x] Re-running `bootstrap.sh` is idempotent (existing tenant
      reused, kernel either re-attached or already running).
- [x] `tabula status` human output gracefully shows multiple
      tenants without overwhelming display (truncate, summary
      line, etc. — pick a sensible UX).
- [x] Backward compat for old shell consumers: removing or
      renaming any pre-existing JSON field is a CI-failing
      change (add a snapshot test for the M2-06 shape and
      assert it's still a subset of the new shape).
- [x] Documentation updated where examples reference the old
      shape.

## Implementation notes

- Runtime `hello` now carries `tenants_served`; the kernel runtime
  snapshot preserves old `capabilities` while adding
  `tenants_served` and `capabilities_by_tenant`.
- `tabula status --json` expands wildcard runtime tenant service
  against known tenant records and includes tenant display name plus
  active session counts.
- Human `tabula status` includes a compact runtime tenant summary.
- `scripts/bootstrap.sh` now creates the project/default tenant with
  `--exists-ok`, waits for `runtimes[].tenants_served` to include
  that tenant, and prints `ready: tenant=<id>`.
- Added `scripts/tenant-init.sh` for explicit tenant creation outside
  project bootstrap.

## Validation evidence

- `go test ./cmd/tabula ./internal/kernel ./internal/runtime/wire ./internal/runtime/host/dialer`
- `go test ./internal/kernel ./internal/runtime/wire ./internal/runtime/conn ./cmd/tabula ./cmd/tabula-runtime/...`
- `go test -race ./internal/kernel ./internal/runtime/wire ./internal/runtime/conn ./cmd/tabula ./cmd/tabula-runtime/...`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --bootstrap-check --home /tmp/tabula-m406-smoke`
- Installed status evidence: `/private/tmp/tabula-m406-smoke/logs/bootstrap-status-last.json` includes `runtimes[0].tenants_served=["default"]`, `capabilities_by_tenant.default`, and tenant `active_sessions`.

## Blocked by

- M4-02 (tenant CLI)
- M4-04 (installer fan-out so created tenant is functional)
- M4-05 (runtime enforcement so `tenants_served` is real,
  not aspirational)

## Notes

- M2-06's `tabula status` was intentionally designed for
  shape evolution. This slice exercises that.
- `bootstrap.sh` is doing more work now (tenant create) but
  remains a shell script per ADR §9 — orchestration belongs
  in shell, not Go.
- Distros (e.g. tabula-distrib/claw) may ship their own
  bootstrap script that wraps this one. Keep this script
  generic and Tabula-kernel-only.
