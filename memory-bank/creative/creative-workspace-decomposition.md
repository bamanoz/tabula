# Creative Phase: Workspace Decomposition

## 1. PROBLEM DEFINITION
- What needs to be designed: the M5 rollout that deletes kernel/project-root workspace concepts and replaces legacy `files`, `shell`, and `code/workspace` surfaces with reusable `workspace/fs` and `workspace/exec` plugins using resolver-backed tenant config.
- Constraints:
  - Workspace is not a kernel abstraction; the kernel routes Invokes and does not own project roots after M5.
  - AMENDMENTS C7 corrects dependency direction: resolver scaffold precedes fs/exec consumption.
  - `${project_root}` is owned by per-tenant `[workspace] project_root`, not by `TABULA_PROJECT_ROOT` or `Hub.ProjectRoot`.
  - Shared executable code remains global at `$TABULA_HOME/skills` and `$TABULA_HOME/plugins`; per-tenant symlinks are introspection only.
  - No legacy aliases: delete `Hub.ProjectRoot`, `TABULA_PROJECT_ROOT`, `init.meta.project_root`, and legacy bundle directories in the M5 window.
- Success criteria:
  - BUILD has a mechanical rollout order that avoids the C7 cycle.
  - `workspace/fs` and `workspace/exec` responsibilities are independent and testable.
  - Cross-repo deletion/grep strategy is explicit enough for M5-05 and M5-06.
- Non-functional requirements:
  - Resolver behavior is deterministic and conformance-tested across Python SDK and Go harnesses.
  - Missing `${project_root}` fails safely where required rather than silently defaulting to global paths.
  - Testbed verifies installed layout and tenant divergence.

## 2. OPTIONS

### Option A: Resolver-first decomposition with atomic legacy deletion
- Description: Land resolver scaffold in M2-04, real `${tenant_id}` in M4-03/M4-03b, `${project_root}` in M5-04/M5-04b, then implement fs/exec consumers and delete legacy surfaces in one coordinated M5 gate.
- Architecture: Kernel owns tenant config and handshake tenant context; bundle SDK/harnesses own template expansion; fs/exec plugins consume resolved config.
- Advantages:
  - Aligns with AMENDMENTS C7.
  - Avoids circular dependency between project root config and fs/exec plugin config.
  - Keeps workspace out of kernel routing after cutover.
  - Enables strong cross-repo grep guards.
- Disadvantages:
  - Requires cross-repo coordination across tabula, tabula-bundles, and tabula-distrib.
  - M5-04/M5-04b must be treated as blockers for M5-01/M5-02 despite stale README text.
- Risk factors:
  - Partial release window could leave plugins expecting `${project_root}` before resolver support exists.

### Option B: Implement fs/exec first with temporary literal paths
- Description: Build plugins using literal config values first, then add `${project_root}` resolver after.
- Architecture: Plugin configs avoid templates until M5-04.
- Advantages:
  - Lets plugin implementation begin with fewer dependencies.
  - Simpler early unit tests.
- Disadvantages:
  - Encourages a temporary path surface that must be deleted.
  - Does not prove real tenant/project-root integration until late.
  - Risks shipping plugin configs that diverge from intended workspace persona.
- Risk factors:
  - Temporary config becomes legacy or distro policy leaks into kernel/bundles.

### Option C: Keep a kernel workspace compatibility layer
- Description: Preserve `Hub.ProjectRoot`/`TABULA_PROJECT_ROOT` or `init.meta.project_root` as aliases while plugins migrate.
- Architecture: Kernel continues to expose project root and may pre-resolve plugin config.
- Advantages:
  - Client/bundle migration can be staggered.
  - Fewer immediate breakages.
- Disadvantages:
  - Violates accepted workspace decomposition and no-legacy rules.
  - Keeps workspace as a kernel concept.
  - Makes tenant divergence harder to reason about.
- Risk factors:
  - Cross-tenant/project-root leaks and stale client assumptions.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 4 | 2 |
| Performance | 2 | 5 | 5 | 5 |
| Maintainability | 5 | 5 | 3 | 1 |
| Scalability | 4 | 5 | 3 | 1 |
| Security | 5 | 5 | 3 | 2 |
| **Weighted Total** | | **92** | 67 | 41 |

## 4. DECISION
**Selected: Option A — resolver-first decomposition with atomic legacy deletion.**

Justification: Option A follows the accepted C7 dependency correction and avoids inventing temporary compatibility layers. It gives BUILD a safe sequence: template variables exist before plugin configs consume them, then legacy workspace/file/shell surfaces can be deleted without aliases.

Trade-offs accepted:
- More careful merge-window packaging is required.
- M5 plugin work may need to branch from resolver support or land in a coordinated PR set.

## 5. IMPLEMENTATION GUIDELINES

### Corrected rollout order
1. `M2-04` (`tabula-bundles`): resolver scaffold in plugin SDK with `${tabula_home}` and stub/default `${tenant_id}`.
2. `M4-03` (`tabula`) and `M4-03b` (`tabula-bundles`): real tenant context and tenant-aware SDK paths; missing tenant env hard-refuses.
3. `M5-04` (`tabula`) and `M5-04b` (`tabula-bundles`): add `[workspace] project_root`, `${project_root}`, and shared resolver corpus.
4. `M5-01` (`tabula-bundles`): implement `workspace/fs` consuming resolver-backed `roots`.
5. `M5-02` (`tabula-bundles`): implement `workspace/exec` consuming resolver-backed `cwd_default` and env templates.
6. `M5-03`: migrate or delete workspace boundary hook after audit.
7. `M5-05`: atomic deletion of legacy bundles and stale kernel/client concepts.
8. `M5-06`: installed-layout workspace decomposition testbed and cross-repo grep guards.

If steps 3-5 are merged in one coordinated window, the PR set must still prove resolver/project-root support is available before fs/exec plugin config is exercised.

### `${project_root}` ownership
- Source of truth: `$TABULA_HOME/tenants/<id>/tenant.toml`:
  ```toml
  [workspace]
  project_root = "/Users/me/src/project"
  ```
- Write path: `tabula tenant set <id> --workspace-root <path>` from AMENDMENTS C6.
- Bootstrap: shell script reads project config/current directory and calls `tabula tenant set`; no Go bootstrap UX.
- Resolver:
  - Python SDK expands plugin config files at load/startup/reload.
  - Go harness expands skill `exec` template strings using the same fixture corpus.
  - Unknown `${...}` → structured load error.
  - Missing `[workspace] project_root` when `${project_root}` is referenced → structured unresolved-variable error.
- Kernel must not pre-resolve plugin config values for workers.

### Client init/project-root migration
- Delete `init.meta.project_root` entirely.
- Clients that need project root read it through the init handshake's `tenant_context.workspace.project_root` field per AMENDMENTS M18.
- `init.meta.tenant_context.workspace.project_root` is a replacement semantic field, not a legacy alias.
- M5-05 grep guard should reject literal `init.meta.project_root` except archive/changelog/removed-feature notes.

### `workspace/fs` responsibilities
- Plugin path: `tabula-bundles/workspace/fs/`.
- Owns filesystem tools: read/write/edit/list/stat/glob/grep/mkdir/rm as implemented by the issue scope.
- Config example:
  ```toml
  roots = ["${project_root}"]
  writable = true
  deny_globs = ["**/.git/**", "**/node_modules/**"]
  follow_symlinks = false
  ```
- Enforces roots lexically after path clean/normalization.
- `fs_outside_root` is plugin-internal, not a Runtime API wire error.
- Does not import or wrap the old `files` skill.

### `workspace/exec` responsibilities
- Plugin path: `tabula-bundles/workspace/exec/`.
- Owns shell/subprocess tools, including background process operations.
- Config example:
  ```toml
  cwd_default = "${project_root}"
  timeout_default_seconds = 60
  timeout_max_seconds = 600
  env_extra = { TABULA_TENANT_ID = "${tenant_id}" }
  ```
- `exec` is independent from `fs.roots`; it does not pretend to sandbox the OS process.
- Missing `cwd_default` means calls must pass `cwd`; do not silently default to `$TABULA_HOME`.
- `exec_denied` is plugin-internal, not a Runtime API wire error.

### Global code / per-tenant introspection split
- Runtime enumerates workers from global `$TABULA_HOME/skills` and `$TABULA_HOME/plugins`.
- `tenants/<id>/skills` and `tenants/<id>/plugins` are symlink trees for client introspection only.
- Per-tenant enablement and behavior are controlled by tenant config overlays and worker env, not by copying code per tenant.

### Legacy deletion strategy
Delete these in the M5 coordinated window:
- `Hub.ProjectRoot`.
- `TABULA_PROJECT_ROOT` read/propagation in kernel code/docs/tests.
- `init.meta.project_root`.
- `tabula-bundles/files/files/`.
- `tabula-bundles/base/shell/`.
- `tabula-bundles/code/workspace/`.
- Stale distro pins and prompts that reference deleted tools.

Cross-repo grep guard patterns:
- `files/files`
- `base/shell`
- `code/workspace`
- `Hub.ProjectRoot`
- `TABULA_PROJECT_ROOT`
- `init.meta.project_root`

Allowed locations only: archive, changelog, ADR/removed-feature notes, or Memory Bank historical records if excluded from production CI.

### BUILD handoff checklist
- Shared resolver corpus passes in Python SDK and Go harness tests.
- Two tenants with different `[workspace] project_root` values resolve different fs roots/cwd defaults.
- Missing `${project_root}` behavior fails safely for configs that require it.
- Testbed covers roots enforcement, exec independence from fs roots, tenant divergence, deleted legacy references, and installed-layout plugin execution.
- PR evidence for M5-05 lists `M5-01..04`, `M5-04b`, distro migration PRs, grep output, and testbed results.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 10
    extensibility: 8
    failure_isolation: 8
    constraint_fit: 10
    simplicity: 8
  ai_slop_flags: none
  verdict: PASS
  notes: Resolver-first ordering removes the documented dependency cycle while making the kernel workspace-agnostic and preserving no-legacy deletion discipline.
