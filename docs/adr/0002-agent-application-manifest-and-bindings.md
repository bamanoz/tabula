# ADR 0002 — Agent application manifests, distro-owned semantics, app bindings

Date: 2026-05-09
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Tabula has a distro model. A distro such as `claw`, `code`, or `guardian`
is a reusable agent package: it owns boot policy, templates, bundled
skills/plugins/clients, prompt policy, product defaults, and distro-specific
runtime behavior.

The missing piece is project/user-level application: a repository or user config
should be able to say "apply this distro here, with these values" so that
another developer can reproduce the same agent setup. This must not make the
kernel aware of repositories, workspaces, projects, chats, organizations,
prompt files, or concrete distro policy.

During design we rejected a platform-level model with fields such as:

```toml
scope = "workspace"
scope = "global"

[instructions]
files = ["AGENTS.md"]

[features]
...
```

Those names are misleading because they encode one distro's domain model into
the platform:

- One distro may have one workspace.
- Another may have two workspaces.
- Another may have no workspace at all.
- A gateway distro may bind to a chat, incident room, organization, or service.
- A hermetic distro may refuse project prompt files entirely.
- `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `USER.md`, and first-run prompt behavior
  are `claw` conventions, not Tabula platform concepts.

The current codebase already shows why this boundary matters:

- `tabula serve` reads one global `config/kernel.toml`.
- `$TABULA_HOME/distrib/active`, `$TABULA_HOME/plugins`,
  `$TABULA_HOME/templates`, `$TABULA_HOME/skills`, and `$TABULA_HOME/apps` are
  global active-distro surfaces.
- Tenants already isolate config/state/cache/logs/sessions and are used by
  runtime workers through `tenant_id`, `TABULA_TENANT_ID`, and
  `TABULA_TENANT_DIR`.
- Driver/gateway clients currently join without a tenant id, so they land in the
  `default` tenant unless explicitly changed.
- Shared `tabula_drivers.prompt_builder` currently contains `claw` prompt-file
  policy. That is a boundary violation: prompt assembly semantics belong to the
  distro or a distro-owned component.

## Decision

Introduce an installer-owned **agent application manifest** model.

An application manifest is a runnable definition. It applies a distro as a named
application instance and also describes the non-sensitive launch topology needed
to start or reuse a kernel and runtime. It does not contain platform-level
workspace/global scope, generic instructions, features, or system prompt text.

### 1. Manifest shape

The committed/user-authored manifest shape is intentionally small:

```toml
[application]
id = "claw-tabula"
name = "Claw for Tabula development"

[distro]
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"

[kernel]
mode = "managed"
id = "claw-tabula"
url = "ws://127.0.0.1:8089/ws"

[[runtimes]]
id = "local"
mode = "managed"
tenants = ["claw-tabula"]

[runtimes.exec]
backend = "bare"

[[bindings.directory]]
root = "${project_root}"
app = "claw-tabula"
kernel = "claw-tabula"

[values.workspace]
path = "${project_root}"
```

The user-authored `[distro]` table intentionally uses only `source`; distro id
and name come from the resolved distro's own `distro.toml` and are recorded in
the lock. `kernel`, `runtimes`, and `bindings` are launch topology. `values` are passed to
the distro contract/materializer. The installer may expand a small set of
mechanical variables such as `${project_root}`, `${application_id}`, and
`${tabula_home}`, but it must not interpret values as a workspace, prompt,
feature set, or tool policy unless the distro contract says how to materialize
them.

### 2. Distro owns application semantics

The distro decides:

- whether it supports named application instances;
- whether instances are singleton or multi-instance;
- which bindings are allowed or preferred;
- whether it has one workspace, many workspaces, no workspace, chats, orgs,
  profiles, services, or some other semantic model;
- what `values` mean and how they are validated;
- whether project files can affect the prompt;
- which files, if any, are read into the prompt;
- final system prompt assembly.

The platform does not define prompt-file semantics. There is no generic
`[instructions]` or `[system_prompt]` section.

### 3. Installer owns apply, lock, materialization, and bindings

The `tabula` kernel binary must not own the apply flow. Use a separate installer
tool:

```bash
tabula-install distro install <source>
tabula-install app apply ./tabula.app.toml
tabula-install app lock ./tabula.app.toml
tabula-install app audit ./tabula.app.toml
tabula-install app bind <app-id> --directory <path>
tabula-install app bind <app-id> --default
```

It is acceptable to implement these commands under `tabula-distro` first if the
installer package has not yet been renamed, but the responsibility remains
outside the kernel.

The installer owns:

- source resolution and lockfiles;
- app id validation;
- app materialization under `TABULA_HOME`;
- managed kernel/runtime startup and external readiness checks;
- invoking distro-owned application materializers;
- binding registry writes;
- safety checks and audit output.

### 4. Bindings replace platform scopes

Do not use `scope = "workspace"`, `scope = "global"`, or `--global` as the
primary model.

Bindings are technical selection rules, not agent domain semantics. They select
both app id and kernel id:

- `directory`: choose this app when launched from a directory tree;
- `default`: fallback app when no more specific binding matches;
- `manual`: app exists but is selected only explicitly.

Example binding registry:

```toml
default = "personal-claw"

[[directory]]
root = "/Users/mak/src/tabula"
app = "claw-tabula"
kernel = "claw-tabula"
```

Resolution order:

1. Explicit CLI app id.
2. `TABULA_APP_ID`.
3. Nearest parent directory binding.
4. Default binding.
5. No app selected: fail with an actionable error.

`default` means user-wide fallback selection. It is not a platform-level global
agent scope.

### 5. Tenants are the application instance substrate

Use the existing tenant isolation model as the substrate for application
instances:

```text
$TABULA_HOME/tenants/<app-id>/
  tenant.toml
  app.toml
  app.lock.json
  values.toml
  config/
  state/
  cache/
  logs/
  distrib/
  _lib
  clients
  templates
  plugins
  skills
```

Runtime sessions must join with the selected application id as `tenant_id`.
Kernel/runtime remain tenant-aware, not workspace-aware or distro-semantic-aware.

Managed and external runtimes attach dynamically and advertise tenants and
capabilities. The kernel tracks live runtime attachments; it does not need to
know future runtime topology ahead of time.

### 6. App-scoped runtime surfaces are required for concurrent different distros

The existing global active distro layout can remain during transition, but it is
not sufficient for running different distros in different projects at the same
time.

The target runtime model is:

```text
session -> tenant/app id -> app-scoped distro generation -> app-scoped plugin catalog and boot metadata
```

This requires tenant/app-aware plugin catalogs, init tool filtering, boot
metadata, reload, and runtime manifest loading. Until then, early app support may
only safely handle multiple instances within current global-surface constraints.

### 7. Prompt assembly is distro-owned

Shared SDK/driver code must not own concrete prompt files or final prompt
semantics. It may provide mechanical provider/session/tool/context plumbing and
a prompt-builder extension point.

For `claw`, the prompt policy must move into a `claw`-owned component that owns:

- `SYSTEM.md`, `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md` composition;
- `IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md` project-file policy;
- first-run identity setup;
- external instruction-only skill presentation;
- provider-specific prompt variants if needed.

The application manifest only supplies `claw` values such as workspace path or
prompt options; it does not become the prompt owner.

## Consequences

Positive:

- Kernel remains generic and distro-agnostic.
- Distro authors can define arbitrary application semantics without asking the
  platform for new scopes.
- A single manifest model supports repo-local agents, personal/default agents,
  multi-workspace agents, gateway agents, and future domain-specific agents.
- Existing tenant isolation becomes useful as the app-instance isolation layer.
- Prompt policy ownership becomes explicit and testable.

Negative / costs:

- Full concurrent different-distro support requires app-scoped runtime surfaces,
  not just a manifest parser.
- Launchers and clients must resolve app bindings and pass tenant ids.
- Tool catalogs and boot metadata must become tenant/app-aware.
- Shared driver code needs a prompt-builder extension point and migration of
  `claw` prompt logic out of shared libraries.
- Existing global active-distro scripts/docs need a staged transition.

## Implementation Notes

The historical implementation issue backlog has been removed after completion.
The installer-to-distro materializer interface is specified in
`docs/APP_MATERIALIZER_CONTRACT.md`.

## Non-Decisions

- Exact filename for manifests may be `tabula.app.toml` initially; installer may
  later support `.tabula/app.toml` or explicit paths.
- Exact on-disk binding registry format may be TOML or JSON. The semantic model
  is the decision, not the serialization.
- The installer binary is `tabula-install`; `tabula-distro` remains the package
  name/internal implementation surface.
- App-scoped runtime surfaces may reuse existing tenant directories or introduce
  helper symlinks. The required property is app isolation, not a specific
  symlink layout.
