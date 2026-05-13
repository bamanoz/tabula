# Agent Manifest Implementation Track

Status: completed
Type: planning
Repo: tabula
Labels: needs-triage, area/installer, area/tenancy, area/runtime, area/distro-integration

## Parent

Plan: `docs/plans/PROJECT_AGENT_MANIFEST.md`
ADR: `docs/adr/0002-agent-application-manifest-and-bindings.md`

## Decision Summary

Build a runnable agent application manifest. A project can commit
`tabula.app.toml`; a user can run one installer command; the installer applies
the distro, creates the app tenant, starts or reuses the requested kernel and
runtime, binds the current directory, and launches the agent.

Core rules:

- Distro owns application semantics, prompt collection, prompt assembly, and the
  meaning of `values`.
- Application manifest contains runnable topology: application, distro, kernel,
  runtimes, bindings, and distro-defined values.
- Installer owns source resolution, locking, materialization, kernel/runtime
  startup orchestration, binding registry, and safety checks.
- Launcher resolves the active application by explicit app id, environment,
  directory binding, then default binding.
- Application id is the tenant id. Runtime sessions join with that `tenant_id`.
- Kernel/runtime use tenants as the isolation substrate, but remain unaware of
  distro domain semantics.
- There is no platform-level `[instructions]`, `[features]`, `system_prompt`,
  `scope = "workspace"`, or `scope = "global"`.
- Sensitive and user-specific values live in local overrides, env, or secret
  stores, not in the committed manifest.

## Proposed Commands

Target UX if a single installer binary is introduced:

```bash
tabula-install distro install <source>
tabula-install app run ./tabula.app.toml
tabula-install app apply ./tabula.app.toml
tabula-install app lock ./tabula.app.toml
tabula-install app audit ./tabula.app.toml
tabula-install app list
tabula-install app inspect <app-id>
tabula-install app bindings
tabula-install app bind <app-id> --directory <path>
tabula-install app bind <app-id> --default
tabula-install app unbind --directory <path>
```

Acceptable first implementation if renaming is deferred:

```bash
tabula-distro app apply ./tabula.app.toml
tabula-distro app lock ./tabula.app.toml
tabula-distro app audit ./tabula.app.toml
```

Avoid `--global` as the primary model. User-wide fallback behavior is a binding
selection (`--default`), not an application scope.

## Manifest Shape

```toml
[application]
id = "claw-tabula"
name = "Claw for Tabula"

[distro]
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"

[values.workspace]
path = "${project_root}"
```

If omitted, `kernel`, `runtimes`, and directory bindings default to one managed
local app runtime using `application.id`. `values` remain distro-owned.

## Target Layout

Use tenants as application instances:

```text
$TABULA_HOME/tenants/<app-id>/
  tenant.toml
  app.toml
  app.lock.json
  values.toml
  config/
  prompt/
  state/
  cache/
  logs/
  _lib
  clients
  templates
  plugins
  skills

$TABULA_HOME/app-bindings.toml
```

The existing global active distro layout remains for current behavior while the
application model is introduced. Do not silently replace it before app-scoped
runtime surfaces exist.

## Issues

- `AM-01-runnable-app-manifest-and-lock.md` — parse runnable manifests, generate
  app locks, and add installer command surface.
- `AM-02-app-binding-registry.md` — materialize directory/default/manual binding
  registry and deterministic resolution.
- `AM-03-kernel-runtime-launch-orchestration.md` — start/reuse managed kernels
  and runtimes from manifest topology; support external/embedded modes.
- `AM-04-launchers-pass-app-tenant.md` — launchers and drivers resolve app id
  and join with tenant/app id.
- `AM-05-claw-application-contract.md` — add `claw` distro application contract,
  values schema, examples, and materializer design.
- `AM-06-claw-prompt-ownership.md` — move `claw` prompt policy out of shared
  driver SDK/library into a `claw`-owned component.
- `AM-07-app-scoped-runtime-surface.md` — support app/tenant-scoped distro
  surfaces, plugin catalogs, boot metadata, and runtime reload.
- `AM-08-testbed-and-docs.md` — add installed-layout tests, multi-app testbed
  coverage, and user-facing docs.
- `AM-09-tenant-aware-runtime-catalogs.md` — finish concurrent multi-app
  catalogs and app-scoped boot metadata in one kernel.
- `AM-10-runtime-execution-backends.md` — paused; SSH/Docker execution backends
  are a separate runtime/sandboxing project.
- `AM-11-tabula-install-binary.md` — introduce the final installer binary and
  command surface.
- `AM-12-installed-app-testbed.md` — add canonical installed-layout testbed
  coverage for runnable app manifests.
- `AM-13-distro-application-contract.md` — lock down the installer-to-distro
  application materializer contract.
- `AM-14-test-distro-values-materializer.md` — prove opaque `[values]` flow into
  tenant-local plugin/client config through a generated test distro.
- `AM-15-claw-application-materializer.md` — implement the real `claw` app
  materializer in `tabula-distrib`.
- `AM-16-real-claw-app-testbed.md` — run a real Claw app manifest in installed
  testbed coverage.
- `AM-17-cold-managed-app-run.md` — prove `app run` can start from no running
  kernel/runtime and bring up managed topology.
- `AM-18-launcher-binding-resolution.md` — prove launcher binding resolution and
  tenant join behavior end to end.
- `AM-19-app-memory-modes.md` — prove tenant-local default memory and explicit
  shared memory modes.
- `AM-20-two-apps-one-kernel.md` — prove two apps with isolated catalogs/state in
  one kernel.
- `AM-21-app-boot-metadata-audit.md` — tighten inspect/audit coverage for
  app-scoped boot metadata and tenant layout.
- `AM-22-final-app-docs.md` — refresh docs to match final implemented behavior.

## Sequencing

1. Land runnable manifest/lock parser and audit output.
2. Materialize app id as tenant id and write tenant-local values/config.
3. Add binding registry.
4. Add kernel/runtime launch orchestration from manifest topology.
5. Teach clients to pass tenant/app id on join.
6. Add `claw` application contract/materializer and prompt ownership migration.
7. Add selected-app runtime surfaces.
8. Add docs and focused installed-layout coverage.
9. Add tenant-aware runtime catalogs and app-scoped boot metadata for concurrent
   apps.
10. Skip paused SSH/Docker execution backends for this track.
11. Introduce `tabula-install` as final installer command surface.
12. Add full installed testbed coverage for one app, then two apps and two
    projects.
13. Lock down the distro application materializer contract.
14. Prove `[values]` materialization through a generated test distro.
15. Implement and test the real `claw` application materializer.
16. Prove cold managed `app run`, launcher binding resolution, memory modes, and
    two-app isolation.
17. Refresh final docs and audit output.

## Open Risks

- `tabula serve` still has one process-level boot command, but app runtime
  surfaces are tenant-scoped through installer materialization, runtime config,
  reload, and tenant-specific client joins.
- Local runtime config supports multiple tenant/app catalog surfaces in one
  kernel; `claw-app-manifest` verifies two live Claw apps with duplicate plugin
  ids and isolated fs/exec/memory config.
- Kernel tool init and dispatch are tenant-aware for app catalogs. Runtime reload
  coalesces rapid app updates by reloading the full local tenant set.
- Prompt semantics remain distro-owned. Shared SDK code resolves app bindings and
  transport only; Claw owns its prompt/materializer policy.
- Existing distro install layout remains for global installs. App installs also
  persist tenant-local `tenant.toml`, `app.toml`, `app.lock.json`, `values.toml`,
  plugin/client config, and inspectable lock/source metadata.
- Current runnable manifest accepts SSH/Docker backend shapes, but managed
  runtime execution supports `bare` only. SSH/Docker backend work is paused and
  should be handled separately from this track.
