# Amendments — review pass corrections

This file is **authoritative** and overrides any conflicting
text in individual issue files. It exists because a thorough
review pass after the M1-M6 cut found contradictions and
gaps. Rather than rewrite every issue, decisions are recorded
here and individual issues will be touched only where their
text actively misleads.

Scope: corrections to issues `M1-01` through `M6-05` and
README.md.

---

## C1 — `TABULA_TENANT_DIR` missing-env behavior

**Decision: hard refuse + exit `internal_error`.** Worker SDK
on missing/empty `TABULA_TENANT_DIR` does NOT fall back to
global path. ADR §6 defense-in-depth wins: a worker without
tenant context must not run.

- `M4-03` text "Falls back to global path with WARNING if env
  missing" is **superseded** — fall-back removed.
- `M4-05` text stands.

## C2 — `tabula status --json` shape evolution

**Decision: additive only, never remove.** `M4-06` is
amended:

```json
{
  "runtimes": [
    {
      "id": "local",
      "attached": true,
      "capabilities": ["fs", "exec", ...],         // KEEP — flat union
      "tenants_served": ["default", "myproject"],
      "capabilities_by_tenant": {                   // ADD
        "default": [...],
        "myproject": [...]
      }
    }
  ],
  ...
}
```

The flat `capabilities` field stays; M2-06 shell consumers
keep working. `capabilities_by_tenant` is the new
tenant-aware view. Snapshot test from M4-06 asserts both
fields present.

## C3 — `gateway-telegram` cross-repo migration

**Decision: tracked as a separate issue file.** See new
`M2-04b-gateway-telegram-sdk-migration.md` (repo:
`tabula-distrib`). M2-07's cutover is blocked by it.

## C4 — Kernel-side runtime registry config

**Decision: split into two layers.**

- **Kernel-side `$TABULA_HOME/config/global.toml`** holds the
  registry of known runtimes and their dial parameters:
  ```toml
  [[runtime]]                       # singular key, list-of-tables
  id      = "local"                 # required
  backend = "local"                 # local | attach | ssh | wss
  # backend-specific fields below; vary per backend
  ```
  `[[runtime]]` is the **kernel-side** config key.
- **Per-tenant `tenants/<id>/tenant.toml`** holds the binding
  whitelist:
  ```toml
  [tenant]
  allowed_runtimes = ["local"]      # or ["*"] = any runtime
  default_runtime  = "local"
  ```
- **Runtime-side `$TABULA_HOME/config/runtime.toml`** keeps
  `[[kernel]]` (which kernels do I dial). Different file,
  different layer; no conflict.

This resolves `[[runtime]]` vs `[[runtimes]]` vs `[[kernel]]`
drift. See new `M4-08-kernel-runtime-registry-config.md`
which lands the kernel-side `[[runtime]]` parsing and
per-tenant `allowed_runtimes` enforcement.

Issues affected:
- `M6-03` (SSH backend): config example moved into
  `[[runtime]]` form per above.
- `M6-01`/`M6-02`: same — kernel-side `[[runtime]]` for WSS.

## C5 — Bundles-repo SDK companions

**Decision: file three companion issues in
`tabula-bundles`** for the SDK changes that M4-03 / M4-05 /
M5-04 describe:

- `M4-03b-bundle-sdk-tenant-paths.md`
- `M4-05b-bundle-sdk-tenant-env-check.md`
- `M5-04b-bundle-sdk-template-resolver.md`

Each new issue is blocked by its `tabula`-side counterpart
and explicitly carries the SDK code change.

## C6 — `tabula tenant set` subcommand

**Decision: add to `M4-02`.** Subcommand surface
extended with:

```
tabula tenant set <id> [--workspace-root <path>]
                        [--display-name <name>]
                        [--default-runtime <runtime-id>]
                        [--allowed-runtimes <id1,id2,...>]
```

Updates `tenants/<id>/tenant.toml`. `--workspace-root` writes
`[workspace] project_root`. M5-04 and M4-06 reference this
subcommand without inventing it.

## C7 — Template resolver dependency direction

**Decision: split resolver landing across milestones.**

- **M2-04** (plugin SDK base): introduces the
  `tabula_plugin_sdk.config` template resolver scaffolding
  with `${tabula_home}` and `${tenant_id}` (no
  `${project_root}` yet). Add to M2-04 acceptance criteria.
- **M4-03** (per-tenant config plumbing): registers
  `${tenant_id}` properly once tenants are real (M2-04 has
  it as a stub returning literal "default").
- **M5-04** (workspace decomposition): adds
  `${project_root}` resolver entry, sourced from
  `[workspace] project_root` in tenant.toml.

This unblocks M5-01 / M5-02 (fs and exec plugins): when they
write configs using `${project_root}`, the resolver
infrastructure already exists; they only depend on the
specific variable being registered, which M5-04 owns.
M5-04's "blocked by M5-01/M5-02" was wrong — corrected: M5-04
blocks M5-01/M5-02 (or they all merge together; either way,
no cycle).

**Updated dependency direction:**

```
M2-04 (resolver scaffold + ${tabula_home}, ${tenant_id} stub)
  └─▶ M4-03 (tenant_id resolves to real tenant)
        └─▶ M5-04 (registers ${project_root})
              └─▶ M5-01, M5-02 (fs, exec plugins consume it)
                    └─▶ M5-05 (delete legacy)
```

## C8 — `cancelled` error code

**Decision: add `cancelled` to the wire error code set in
M1-01.** Distinct from `timeout`:

- `cancelled` — explicit `Cancel` op was sent for this
  call_id.
- `timeout` — per-call deadline expired without explicit
  cancel.

Both are retryable=false (the operation didn't fail; it was
intentionally aborted).

The full M1-01 error code roster is now:

```
unauthorized            (M2-05 auth fail)
runtime_unavailable     (transport-layer disconnect; retryable)
runtime_busy            (M3-06 cold pool overflow; retryable)
unknown_runtime         (M6-02 cert/registry mismatch)
tenant_unknown          (M4-03 unknown tenant in routing)
tenant_forbidden        (M4-05 whitelist rejection; renamed
                         from `tenant_denied`)
target_unknown          (target_id not in capabilities)
target_forbidden        (renamed from `target_not_authorized`)
tool_not_found          (target exists but no such tool)
timeout                 (per-call deadline expired)
cancelled               (explicit Cancel op fulfilled)
protocol_error          (malformed wire frame)
internal_error          (catch-all)
skill_exec_failed       (skill subprocess returned non-zero)
fs_outside_root         (plugin-internal, not wire)
exec_denied             (plugin-internal, not wire)
```

`tenant_denied` and `target_not_authorized` are NOT
introduced; later issues that name them are corrected to use
`tenant_forbidden` / `target_forbidden`.

---

## MEDIUM-level corrections (batched)

### M1 — Error code naming drift
Resolved by C8 + the consolidated roster above. Wherever an
issue mentions a code not on the roster, treat it as the
nearest-matching code on the roster. Plugin-internal codes
(`fs_outside_root`, `exec_denied`) live in tool result
envelopes, not wire-level errors — keep them in plugins.

### M2 — `target` field shape
**Wire form is the object `{kind, id}`** per plan §2.4 and
M1-01. The string form `"skill:<name>"` / `"plugin:<name>"`
in M3-01 is an **internal display / log identifier**, not
the wire envelope. M3-07's pseudo-code `Invoke{target:
skill.target_id, ...}` is shorthand; real construction is
`Invoke{Target: {Kind: "skill", ID: "timer"}, ...}`.

### M3 — `token` vs `token_file` field name
**Standardize on `token_file`** everywhere. M2-02's example
`token = "${TABULA_RUNTIME_TOKEN_FILE}"` is corrected to
`token_file = "${TABULA_HOME}/run/runtime-token"`.

### M4 — Config key drift (`[[runtime]]` / `[[runtimes]]` /
### `[[kernel]]`)
Resolved by C4. Convention:
- `[[runtime]]` (singular): kernel-side global.toml,
  registry of dialable runtimes.
- `[[kernel]]` (singular): runtime-side runtime.toml, list
  of kernels to dial.
- No `[[runtimes]]` or `[[kernels]]` (plural) form anywhere.

### M5 — Skill location global vs per-tenant
**Decision: skills (and plugins) are shared code; the
runtime reads them once from a global path; tenancy is
applied via config overlay and worker env, not via separate
binaries.**

- Runtime enumerates skills/plugins from
  `$TABULA_HOME/skills/` and `$TABULA_HOME/plugins/`
  (existing global symlinks fanned by the installer's
  active generation).
- `tenants/<id>/skills/` and `tenants/<id>/plugins/`
  symlinks created by M4-04 are **for client-side
  introspection** (e.g. a TUI listing what's available to a
  tenant). The runtime does NOT read them.
- Per-tenant config overlay (M4-03) determines which
  tools the tenant can actually invoke; the worker's tenant
  env determines state isolation.

This corrects M3-01's search path (kept as
`$TABULA_HOME/skills/`) and clarifies M4-04's purpose for
the per-tenant symlink trees.

### M6 — `clients/`, `templates/` per-tenant fan-out
**Decision: drop them from M4-01/M4-04** unless a future
issue motivates them. M4-01 layout simplified to:

```
tenants/<id>/
  tenant.toml
  config/
  state/
  cache/
  logs/
  skills/      # symlinks (introspection only)
  plugins/     # symlinks (introspection only)
```

`clients/` and `templates/` removed. If the TUI later wants
per-tenant template overrides, add them then.

### M10 — `runtime_id` format
**Decision: same rule as `tenant_id`**:
`^[a-z0-9][a-z0-9-]{0,62}$`, reserved names: `kernel`,
`system`, `admin`. Validated by M1-01 (wire types validate
both ids on read) and M4-08 (kernel-side registry rejects
malformed ids).

### M11 — `kernel_id` source
**Decision: kernel reads `$TABULA_HOME/config/global.toml`
`[kernel] id = "main"`, defaulting to hostname.**
M2-07 sets `Hub.KernelID` from this. M2-03 worker env
`TABULA_KERNEL_ID` propagates it. No separate generation
mechanism.

### M12 — Template resolver location
**Decision: two implementations, shared format.**

- Python SDK `tabula_plugin_sdk.config` (M5-04b) for
  plugin/skill **config files** (TOML at startup).
- Go harness layer (M3-02..04) for skill **`exec` template
  strings** in SKILL.md frontmatter at spawn time.

Both expand the same `${var}` syntax with the same set of
known variables. Conformance is enforced by a shared test
fixture corpus (Go tests parse the same input strings as
Python tests, assert identical output). Add to M5-04
acceptance criteria.

### M16 — `tabula serve --foreground` flag
**Decision: dropped.** Plan Q9d already mandates foreground
only. M6-04 service unit invokes `tabula serve` directly;
no flag needed. M6-04 text saying "honour absence of
`--detach`" is moot — `--detach` doesn't exist.

### M17 — `runtime_busy` introduction
Resolved by C8 — added to the M1-01 roster.

### M18 — M5-05 lint guard for `init.meta.project_root`
**Decision: M5-04 takes option (1) — clients query tenant
context via init handshake's `tenant_context` field.**
`init.meta.project_root` is removed entirely; clients that
need project root read it from
`init.meta.tenant_context.workspace.project_root`. M5-05
lint guard for the literal string `init.meta.project_root`
will then catch any stragglers correctly.

---

## How to use this file

When implementing any of the M1-M6 issues:

1. Read the issue file.
2. Read this AMENDMENTS.md.
3. If they conflict, this file wins. Note the amendment ID
   (e.g. C1, M5) in the PR description so review knows which
   correction applied.

Future amendments append to this file with new headings; do
not edit older entries except to mark them `(superseded by
[ID])`.
