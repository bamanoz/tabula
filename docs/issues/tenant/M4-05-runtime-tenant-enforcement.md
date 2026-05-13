# M4-05 — Runtime-side tenant whitelist + enforcement

Status: done
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/tenancy, area/security, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5, §6)

## What to build

The runtime-side half of the three-layer tenant enforcement.
`tabula-runtime` learns about the whitelist of (kernel_id,
tenant_id) pairs it is willing to serve and rejects Invokes
that don't match.

Per ADR §6, three layers: kernel router (M4-03), runtime pool
(this slice), worker check (next bullet). Defense in depth.

Components:

- `runtime.toml` schema extension:
  ```toml
  [[kernel]]
  id    = "local"
  url   = "unix:///$TABULA_HOME/run/runtime.sock"
  token_file = "${TABULA_HOME}/run/runtime-token"
  tenants = ["*"]                   # default: any tenant
  # or
  tenants = ["myproject", "demo"]   # explicit allow-list
  ```
- Whitelist semantics: `["*"]` = any tenant (M2 / single-host
  case). Explicit list = only those tenants accepted; others
  → `tenant_forbidden` (new error code in wire types if not
  already present).
- Runtime pool enforcement (M2-03):
  - On Invoke, validate `(kernel_id, tenant_id)` against the
    config for that kernel connection.
  - Reject before spawning any worker.
- Per-tenant resource accounting:
  - `pool.cold_workers_per_tenant_max` (M3-06) already exists.
    Now allow per-tenant overrides:
    ```toml
    [pool.tenants.myproject]
    cold_workers_max = 32
    warm_workers_max = 16
    ```
  - Defaults apply if not overridden.
- Worker env propagation (M2-03 already sets
  `TABULA_TENANT_ID`):
  - Add `TABULA_TENANT_DIR` pointing to
    `$TABULA_HOME/tenants/<id>/` so SDK `*_state_dir()`
    helpers (M4-03) work without the runtime resolving paths
    twice.
- Worker-side tenant check (cheap defense-in-depth):
  - The shared `tabula_plugin_sdk` Python lib (M2-04 SDK)
    gains `current_tenant_id() -> str` reading
    `$TABULA_TENANT_ID`. Skill SDK (M3-05) same.
  - If the env is missing or empty when a worker starts: the
    SDK refuses to call any tool handler and exits with
    `internal_error`. This is the worker layer's belt-and-
    suspenders (ADR §6).
  - This change ships in tabula-bundles (cross-repo); track in
    a follow-up bundle PR blocked by this issue.
- Logging:
  - Every Invoke logs `(kernel_id, tenant_id, target,
    tool, call_id, outcome)` at info level.
  - Tenant whitelist rejections logged at warn level with
    full context.

## Acceptance criteria

- [x] `runtime.toml` accepts `tenants = ["*"]` (default) and
      explicit list.
- [x] Invoke for whitelisted tenant succeeds.
- [x] Invoke for non-whitelisted tenant rejected with
      `tenant_forbidden` before worker spawn (verified in
      test).
- [x] `cold_workers_max` per-tenant override honored.
- [x] `TABULA_TENANT_DIR` set correctly in worker env.
- [x] All three enforcement layers exercised in integration
      tests:
      1. Kernel router rejects unknown tenant (M4-03).
      2. Runtime pool rejects non-whitelisted tenant (this).
      3. Worker SDK refuses on missing env (cross-repo bundle
         test).
- [x] No cross-tenant data leak in any test scenario covered by
      this runtime-side slice.
- [x] Race-clean.

## Implementation notes

- Runtime config now supports `[[kernel]].tenants` with default
  `[*]` semantics and `[pool.tenants.<id>].cold_workers_max`
  overrides.
- Runtime pool rejects non-allowlisted tenants before worker spawn
  and logs tenant whitelist rejections at warn level.
- Runtime pool logs Invoke outcomes with `(kernel_id, tenant_id,
  target, tool, call_id, outcome)` at info level without logging
  payload data.
- Warm plugin workers and cold bash skill workers receive
  `TABULA_TENANT_DIR=$TABULA_HOME/tenants/<id>` when the runtime has
  `TABULA_HOME` available.
- Worker SDK missing-env enforcement landed in the cross-repo bundle
  follow-up (`M4-05b`).

## Validation evidence

- `go test ./internal/runtime/host/config ./internal/runtime/host/pool ./internal/runtime/host/policy/bare ./internal/runtime/host/harness/bash ./cmd/tabula-runtime ./cmd/tabula`
- `go test ./internal/config ./internal/kernel ./internal/tenant ./cmd/tabula ./cmd/tabula-runtime/...`
- `go test -race ./internal/config ./internal/kernel ./internal/tenant ./cmd/tabula ./cmd/tabula-runtime/...`
- `PYTHONPATH=tools/tabula-distro/src python3 -m unittest tools.tabula-distro.tests.test_install`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --suite skill-cold-execution --bootstrap-check --home /tmp/tabula-m4-smoke`
- `PYTHONPATH=_lib/python/src python3 -m unittest _lib.python.tests.test_contract _lib.python.tests.test_skill_sdk` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite skill-cold-execution --suite skill-failure-modes --bootstrap-check --home /tmp/tabula-m405b-smoke2`

## Blocked by

- M4-03 (kernel sends tenant_id correctly)
- M3-06 (cold worker pool exists to gain per-tenant limits)

## Notes

- Per ADR §5, in M4 a single runtime serves all tenants of a
  kernel. Running a separate runtime per tenant is allowed
  (the `tenants` whitelist makes it possible to dedicate one
  runtime to one tenant) but not the default.
- This slice does not introduce per-tenant rate limiting,
  quotas, or fairness scheduling. Just admission control. Any
  fairness work is a separate program of work.
- Auth: M4 still uses M2's bearer-token model. Token grants
  access to all tenants the runtime is configured to serve;
  per-tenant tokens are an M6 concern when mTLS lands.
