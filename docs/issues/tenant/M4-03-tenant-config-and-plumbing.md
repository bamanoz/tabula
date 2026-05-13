# M4-03 — Per-tenant config overlay + plumbing tenant_id everywhere

Status: done
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5)

## What to build

Make every kernel surface tenant-aware. Per-tenant config
overlays, session→tenant binding, tenant_id propagation through
to every Runtime API Invoke (already mandatory in wire types
since M1-01, but until M4 only `default` is plumbed).

Components:

- Config layering (per ADR §5):
  - Global: `$TABULA_HOME/config/global.toml`.
  - Per-tenant overlay: `$TABULA_HOME/tenants/<id>/config/tenant.toml`.
  - Plugin config: `config/plugins/<plugin>/config.toml` global
    + per-tenant `tenants/<id>/config/plugins/<plugin>/config.toml`.
  - Merge order: global ← tenant ← (future: session). Last
    write wins. Document the precedence in the config package
    godoc.
- `internal/config/`: extend loader to take a tenant_id
  and return merged config. Existing single-tenant callers
  pass `"default"` until this slice's wave of caller updates
  completes.
- Session → tenant binding:
  - Session create takes `tenant_id` (mandatory; default to
    `"default"` if not specified, enforced by the entry point
    that creates sessions).
  - Stored in session record.
  - Every operation derived from a session inherits its
    tenant_id.
- `Hub.RouteToolCall` and any tool dispatch path propagates
  tenant_id all the way into the `Invoke` envelope.
- Init handshake:
  - Client connects with `(tenant_id, target_id, ...)` — the
    tenant_id is part of the join request.
  - Kernel rejects if tenant doesn't exist.
- State separation:
  - Sessions move from a flat directory to
    `tenants/<id>/state/sessions/`.
  - Plugin state via `tabula_plugin_sdk` `plugin_state_dir()`
    starts resolving to per-tenant path.
  - Skill state via `tabula_skill_sdk` `skill_state_dir()`
    similarly per-tenant.
- Updated SDK behavior (cross-repo):
  - `tabula-bundles/_lib/python/tabula_plugin_sdk/paths.py`:
    `plugin_state_dir()` resolves under `$TABULA_TENANT_DIR/
    state/plugins/<plugin>/` (new env var, set by runtime
    M4-05). Falls back to global path with WARNING if env
    missing.
  - Same for `tabula_skill_sdk`.
  - This is a behavioral change to existing SDKs. Per AGENTS.md
    no-legacy, the change replaces old behavior in place.

### Tenant resolution fallback

Q: what happens when an entry point creates a session without
specifying a tenant?

Decision: kernel default is `"default"`. Distros (e.g. tabula-
distrib/claw) may set `default_tenant` in distro config; entry
points may set per-call. The chain:

```
explicit param → distro default → "default"
```

If the resolved tenant doesn't exist, error out
(`tenant_unknown`); do NOT auto-create. Auto-create is the
installer's job (M4-04 bootstrap.sh).

## Acceptance criteria

- [x] Config loader takes tenant_id; merge order enforced via
      tests.
- [x] Session create requires (or defaults) tenant_id; rejects
      unknown tenants with `tenant_unknown` error.
- [x] Every `Invoke` envelope sent from kernel carries the
      session's tenant_id (verified via mock RuntimeConn
      assertions).
- [x] Sessions stored under `tenants/<id>/state/sessions/`.
- [x] Per-tenant plugin config is read; values from
      `config/plugins/foo/config.toml` correctly overlaid by
      `tenants/myproject/config/plugins/foo/config.toml`.
- [x] SDK `plugin_state_dir()` returns tenant-scoped path when
      `TABULA_TENANT_DIR` is set; warns and falls back when
      missing.
- [x] Existing single-tenant tests pass after migration to
      explicit `default` tenant_id passing.
- [x] Race-clean.

## Implementation notes

- `internal/config.Loader` now merges:
  - `config/global.toml`
  - `tenants/<id>/config/tenant.toml`
  - `config/plugins/<plugin>/config.toml`
  - `tenants/<id>/config/plugins/<plugin>/config.toml`
- Join/session plumbing is tenant-aware end-to-end:
  - join accepts `tenant_id`
  - unknown tenant rejects with `tenant_unknown`
  - sessions record `tenant_id`
  - runtime `Invoke` envelopes inherit the session tenant
- Session persistence is tenant-scoped under
  `tenants/<id>/state/sessions/`.
- Python SDK state path helpers now prefer `TABULA_TENANT_DIR` for
  plugin/skill state and emit `RuntimeWarning` fallback notices when
  tenant context is missing.
- Existing single-tenant testbed suites now pass explicit
  `tenant_id="default"`, and M4-07 multi-tenant suites exercise the
  same plumbing in installed layout.

## Validation evidence

- `go test ./internal/config ./internal/kernel`
- `go test -race ./internal/config ./internal/kernel`
- `PYTHONPATH=_lib/python/src python3 -m unittest _lib.python.tests.test_contract _lib.python.tests.test_skill_sdk` in `../tabula-bundles`
- Installed multi-tenant evidence from M4-07:
  - `tenant-isolation`
  - `tenant-runtime-whitelist`
  - `tenant-config-overlay`

## Blocked by

- M4-01 (data model)
- M4-02 (CLI to set up test tenants)

## Notes

- Plugins / skills do not need to know about tenants at the
  code level — `plugin_state_dir()` returns the right path,
  done. Author surface unchanged. ADR §5 commitment.
- Two tenants invoking the same plugin tool spawn two distinct
  worker processes (M2-03 already enforces per-(kernel,
  tenant, target) isolation). This slice ensures the tenant_id
  reaches the runtime correctly.
- Workspace decomposition (M5) builds on this: `fs.roots` and
  `exec.cwd_default` move into per-tenant `tenant.toml`
  config sections. M4 just makes the overlay mechanism work.
