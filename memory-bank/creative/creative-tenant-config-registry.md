# Creative Phase: Tenant Config Registry

## 1. PROBLEM DEFINITION
- What needs to be designed: the M4 tenant model, default-tenant invariant, config layering, runtime registry schema, whitelist enforcement, and additive status JSON evolution.
- Constraints:
  - Tenant isolation has three enforcement points: kernel router, runtime worker pool, worker env/SDK check.
  - `default` is a well-known system-managed tenant ID, not hard-invalid.
  - Hard-reserved names for general tenant creation/lookup policy are `system`, `runtime`, `kernel`, and `admin` unless a later issue documents a new one.
  - Effective tenant layout follows AMENDMENTS M6, not stale issue text listing `clients/` or `templates/`.
  - Kernel runtime registry uses singular `[[runtime]]`; runtime-side dial-out config uses singular `[[kernel]]`; no plural aliases.
  - M4-08 is inserted before enforcement/status/backend work that depends on registry shape.
- Success criteria:
  - BUILD can implement tenant validators without rejecting the system-managed `default` tenant in runtime/status/env/resolver paths.
  - Config shapes for kernel global, tenant, and runtime-side files are unambiguous and alias-free.
  - Status JSON remains additive over M2 shape.
- Non-functional requirements:
  - Missing/empty tenant environment is a hard refusal with `internal_error`, not fallback.
  - Tenant store and secret-bearing files use restrictive permissions.
  - Config validation should fail early on unknown runtime references and malformed IDs.

## 2. OPTIONS

### Option A: Layered registry with source-aware tenant validation
- Description: Split validation into syntax, hard-reserved-name validation, and user-create policy. Treat `default` as system-managed-valid. Use kernel `global.toml` `[[runtime]]`, tenant `tenant.toml` `[tenant]`, and runtime `runtime.toml` `[[kernel]]` with no aliases.
- Architecture: Tenant store owns layout and metadata; registry owns runtime definitions; router combines tenant whitelist/default with live runtime connections; runtime config enforces reciprocal whitelist.
- Advantages:
  - Resolves the `default` contradiction without special-casing every callsite.
  - Cleanly separates config ownership by layer.
  - Allows M2 implicit local runtime to be formalized in M4-08.
  - Supports testable three-layer enforcement.
- Disadvantages:
  - Requires discipline to use the right validator at each callsite.
  - More explicit config-load tests are needed.
- Risk factors:
  - BUILD could collapse validators into one helper and reintroduce the contradiction.

### Option B: Reserve `default` everywhere and use another boot tenant
- Description: Treat `default` as invalid and migrate boot/fresh installs to a different well-known tenant ID.
- Architecture: One strict validator for all tenant IDs.
- Advantages:
  - Simple validator implementation.
  - Avoids source-aware creation rules.
- Disadvantages:
  - Conflicts with accepted plan and many issue examples relying on `tenants/default/`.
  - Requires broad issue/doc rewrites and risks accidental compatibility shims.
  - Breaks M2/M3 stub tenant assumptions.
- Risk factors:
  - High cross-repo drift and stale examples.

### Option C: Treat missing tenant/config as fallback to global defaults
- Description: Keep a permissive global fallback for missing tenant env/config until all bundles migrate.
- Architecture: SDK/config layers use global paths when tenant context is absent.
- Advantages:
  - Short-term migration friction is lower.
  - Some old tools keep running during partial rollout.
- Disadvantages:
  - Directly violates AMENDMENTS C1 and ADR defense-in-depth.
  - Creates hidden cross-tenant state leaks.
  - Adds a legacy shim without a concrete on-disk migration need.
- Risk factors:
  - SECURITY will fail tenant isolation checks.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 4 | 3 |
| Performance | 2 | 5 | 5 | 5 |
| Maintainability | 5 | 5 | 2 | 1 |
| Scalability | 4 | 5 | 3 | 1 |
| Security | 5 | 5 | 4 | 1 |
| **Weighted Total** | | **92** | 65 | 35 |

## 4. DECISION
**Selected: Option A — layered registry with source-aware tenant validation.**

Justification: Option A is the only design that satisfies the accepted M4 refinement: `default` remains a real, system-managed tenant while user-created reserved/system IDs stay blocked. It also preserves clean ownership of runtime registry vs tenant binding vs runtime allow-list config.

Trade-offs accepted:
- BUILD must implement multiple small validators/policies rather than a single catch-all `ValidateTenantID`.
- Tests must cover validator boundaries explicitly.

## 5. IMPLEMENTATION GUIDELINES

### Tenant ID invariants and validator boundaries
- Syntax validation for tenant/runtime/kernel IDs: `^[a-z0-9][a-z0-9-]{0,62}$`.
- Hard-reserved tenant IDs: `system`, `runtime`, `kernel`, `admin`.
- `default` is valid for:
  - Tenant store enumeration.
  - Runtime and wire validation.
  - Routing and status JSON.
  - Worker env (`TABULA_TENANT_ID=default`, `TABULA_TENANT_DIR=.../tenants/default`).
  - Template resolver `${tenant_id}`.
- Source-aware creation policy:
  - Fresh boot/migration/bootstrap may create or ensure `tenants/default/`.
  - `tabula tenant create default` is not a normal user-create flow; after boot it should fail as exists, and on an offline empty store should report system-managed initialization is owned by boot/bootstrap.
  - `tabula tenant set default ...` is allowed for the existing system-managed tenant.
  - `tabula tenant delete default` refuses unless `--force`; if forced and no tenants remain, next boot may recreate it.
- Suggested function split for BUILD:
  - `ValidateIDSyntax(id)` — regex only.
  - `ValidateRuntimeID(id)` / `ValidateKernelID(id)` — syntax plus their reserved names.
  - `ValidateTenantIDForRead(id)` — syntax plus hard-reserved rejection; permits `default`.
  - `ValidateTenantIDForUserCreate(id)` — read validation plus rejects/blocks `default` as system-managed.
  - `EnsureDefaultTenant()` — privileged boot/migration path.

### Effective tenant layout
Use the simplified layout under `$TABULA_HOME/tenants/<id>/`:
```text
tenant.toml
config/
state/
cache/
logs/
skills/      # symlinks, introspection only
plugins/     # symlinks, introspection only
```
- Do not create per-tenant `clients/` or `templates/` in this program.
- Runtime enumerates executable skill/plugin code from global `$TABULA_HOME/skills` and `$TABULA_HOME/plugins`.
- Per-tenant `skills/` and `plugins/` are client-side introspection symlinks only.
- Tenant isolation comes from config overlay, runtime whitelist, worker env, and per-tenant state/log/cache paths.

### Config registry shapes

Kernel global config: `$TABULA_HOME/config/global.toml`
```toml
[kernel]
id = "main" # default hostname if absent when created

[[runtime]]
id = "local"
backend = "local" # local | attach | ssh | wss
# backend-specific fields live under the same runtime entry or subtable.
```

Tenant config: `$TABULA_HOME/tenants/<id>/tenant.toml`
```toml
[tenant]
display_name = "My Project"
allowed_runtimes = ["local"] # or ["*"]
default_runtime = "local"

[workspace]
project_root = "/Users/me/src/myproject" # introduced/consumed in M5
```

Runtime-side config: `$TABULA_HOME/config/runtime.toml`
```toml
[[kernel]]
id = "main"
url = "unix://${TABULA_HOME}/run/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
tenants = ["*"] # or explicit tenant IDs
```

Rules:
- No `[[runtimes]]` or `[[kernels]]` aliases.
- No inline `token` field to mean token path; use `token_file`.
- M2-style single-runtime installs may synthesize `[[runtime]] id="local" backend="local"` in memory when no registry entries exist; do not introduce a plural/legacy config key.
- Duplicate runtime IDs fail config load.
- Tenant `default_runtime` not present in registry fails tenant load.
- Tenant `default_runtime` outside `allowed_runtimes` fails tenant load or route validation before Invoke.

### Whitelist enforcement
- Kernel router:
  - Resolve tenant.
  - Check `allowed_runtimes` and `default_runtime`.
  - Refuse unknown tenant with `tenant_unknown`.
  - Refuse disallowed runtime with `tenant_forbidden` before wire send.
  - Refuse absent live connection with `runtime_unavailable`.
- Runtime worker pool:
  - On Invoke, validate `(kernel_id, tenant_id)` against the runtime-side `[[kernel]].tenants` allow-list.
  - Reject `tenant_forbidden` before worker spawn.
  - Pool key includes `(kernel_id, tenant_id, target_id)`; no cross-tenant worker reuse.
- Worker SDK/env:
  - Every worker gets `TABULA_HOME`, `TABULA_KERNEL_ID`, `TABULA_TENANT_ID`, `TABULA_TENANT_DIR`, `TABULA_TARGET_ID`; skills additionally get `TABULA_SKILL_DIR`, `TABULA_TOOL_NAME`, `TABULA_CALL_ID`.
  - Missing/empty `TABULA_TENANT_ID`, `TABULA_TENANT_DIR`, or `TABULA_KERNEL_ID` is an `internal_error` refusal, not fallback.

### Status JSON evolution
- M2 status shape remains valid and additive.
- M4 adds fields but must keep flat `runtimes[].capabilities` union.
- Runtime status object should include:
  ```json
  {
    "id": "local",
    "attached": true,
    "capabilities": ["fs", "exec"],
    "tenants_served": ["default", "myproject"],
    "capabilities_by_tenant": {
      "default": ["fs"],
      "myproject": ["fs", "exec"]
    }
  }
  ```
- Snapshot/subset tests should fail if M2 fields are removed or renamed.

### Migration shim boundary
- The only allowed shim here is concrete on-disk migration from legacy `$TABULA_HOME` to `tenants/default/`.
- The shim should be one-shot, marker-based, narrowly documented, and scheduled for removal.
- No runtime/wire/config aliases are justified by this migration.

### BUILD handoff checklist
- Unit tests: `default` read/runtime/status/env/resolver valid; `system`, `runtime`, `kernel`, `admin` hard-reserved invalid.
- Boot tests: fresh and legacy migration ensure `tenants/default/` using simplified layout.
- CLI tests: `tenant create default` blocked as system-managed/exists; `tenant set default` allowed; `tenant delete default` force-guarded.
- Registry tests: singular `[[runtime]]` and `[[kernel]]`, duplicate IDs, unknown default runtime, wildcard and explicit allow-lists, reload behavior.
- Runtime/status tests: `default` appears in `tenants_served`, `capabilities_by_tenant`, worker env, and `${tenant_id}` resolver.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 8
    failure_isolation: 9
    constraint_fit: 10
    simplicity: 7
  ai_slop_flags: none
  verdict: PASS
  notes: The selected design resolves the default-tenant contradiction by naming validator boundaries and keeps config ownership split cleanly across kernel, tenant, and runtime layers.
