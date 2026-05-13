# Distro Application Manifest

Status: proposal
Date: 2026-05-09

## Problem

Tabula already has a distro model. A distro such as `claw`, `guardian`, or
`ouroboros` is a reusable agent package: it owns boot policy, templates,
bundled skills/plugins/clients, product defaults, and the installed runtime
surface under `TABULA_HOME`.

What is missing is a repository-local way to say: apply this distro to this
project with these values. Another developer should be able to clone a project
and run one installer command to get the same distro selection, source pins, and
project-specific config without making the kernel understand projects.

This should not become a second distro format. It is closer to:

- a Helm chart plus values file
- a Docker image plus compose/env/volume config
- a reusable module plus project-specific inputs

The distro remains the packaged product. The application manifest is the
project-local apply/config layer for that product.

## Goals

- A project can commit one declarative file that identifies the distro to apply.
- The installer resolves and pins the distro source using the same principles as
  `tabula-distro`.
- The installer writes project-specific runtime config under `TABULA_HOME`, not
  inside the repository as mutable runtime state.
- The kernel stays generic. It does not learn about projects, workspaces, MCP,
  concrete distros, or product policy.
- Distro instructions stay distro-owned: templates, prompt files, boot policy,
  and project-file conventions remain in the distro.
- Plugin/client config stays component-owned and uses the existing config files.
- Secrets stay local and optional. A manifest can refer to config keys that may
  be secret-bearing, but it does not make secrets a required Tabula concept.
- The applying command is a separate installer tool, not `tabula`.

## Non-Goals

- Do not add `tabula agent ...` commands to the kernel CLI.
- Do not make the project manifest an overlay that can arbitrarily redefine the
  distro's bundled components in the first version.
- Do not move distro templates or prompt instructions into the project manifest.
- Do not define universal top-level `instructions` or `features` sections unless
  a concrete cross-distro contract appears.
- Do not require every distro application to include filesystem, exec, MCP,
  memory, gateways, or workspace capabilities.
- Do not make `TABULA_HOME` mean the user's repository. It remains the runtime,
  config, and state root.
- Do not commit generated install layouts, runtime state, caches, logs, local
  overrides, or secret values.

## Existing Facts

`tabula-distro` already owns distro install/composition:

- source resolution for local and `git+...@ref#path=...` sources
- distro lock records
- generation staging and atomic promotion
- active distro selection under `$TABULA_HOME/distrib/active`
- fan-out of `_lib`, `clients`, `templates`, `plugins`, and `skills`
- tenant runtime-surface refresh via `tabula-distro install --tenant <id>`
- compatibility checks against installed kernel/protocol metadata
- hot reload trigger through `$TABULA_HOME/run/reload.touch`

This means the project application installer should reuse or extend that install
machinery instead of adding behavior to `tabula`.

Secrets are not a kernel-level manifest primitive. In the Python SDK today:

- plugin config uses defaults, `$TABULA_HOME/config/global.toml`,
  `$TABULA_HOME/config/plugins/<plugin-id>/config.toml`, environment variables,
  and explicit runtime or CLI values
- schema-based skill/client config can mark fields as `secret: true`
- secret fields may resolve through env, config references, files, or
  `$TABULA_HOME/secrets.json`
- `$TABULA_HOME/secrets.json` is a local store used when a distro/component opts
  into that pattern

So the application manifest should configure normal config values and may write
secret references such as `{ source = "store", id = "..." }`, but it must not
claim that every Tabula application has a required `secrets` block.

In `claw`, instructions are distro policy:

- templates live under the installed distro's `templates/`
- boot policy decides which project files exist and how they are included
- `claw` currently owns `IDENTITY.md`, `SOUL.md`, `USER.md`, and `AGENTS.md`
- workspace resolution is `TABULA_WORKSPACE`, then `[workspace].path` in
  `config/global.toml`, then the distro default

Therefore a generic project application manifest should not define
`[instructions]`. For `claw`, it can set config values that make `claw` point at
the project root, and then `claw` boot policy can create/read its own prompt
files from that location.

There is also an architecture boundary to fix before implementing this model:
the current main system-prompt assembly path is too shared. `claw/boot.py` no
longer emits the final system prompt; the prompt is assembled by the shared
driver library using installed templates and `claw`-style project files. That
means `IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md`, first-run behavior, and
template composition have effectively leaked into a reusable component. They
should instead live in a `claw`-owned bundle or distro-local component. Shared
driver/runtime code may expose hooks or helper interfaces, but it should not own
the concrete prompt-file set for one distro.

## Terminology

- **Distro**: A reusable agent package. It owns boot code, templates, product
  defaults, source declarations for bundles/components, and installed runtime
  layout.
- **Distro Application Manifest**: A repository-local file that tells an
  installer which distro to apply and which project-specific config values to
  materialize.
- **Application Lockfile**: A generated file pinning the resolved distro source
  and any installer-managed application inputs needed for reproducibility.
- **Application Instance**: The applied runtime/config realization for one
  project identity, usually represented as tenant/project-scoped config under
  `TABULA_HOME`.
- **Local Overrides**: Gitignored machine-specific files or environment values.
  They can provide provider choice, local paths, and secret values without
  affecting the committed lock.

## Tool Ownership

The applying command must be outside the `tabula` kernel binary.

Two plausible shapes:

1. Extend the existing `tabula-distro` package with application commands.
2. Create a sibling installer package and share resolver/install code with
   `tabula-distro`.

The first option is likely simpler if the concept is still tightly coupled to
distro installation. The command surface could be:

```bash
tabula-distro apply ./tabula.app.toml
tabula-distro apply --lock ./tabula.app.toml
tabula-distro apply --frozen ./tabula.app.toml
tabula-distro audit ./tabula.app.toml
```

If the name `tabula-distro` becomes too narrow, a later rename can create one
installer tool with two nouns:

```bash
tabula-install distro install <source>
tabula-install app apply ./tabula.app.toml
```

The design point is stable either way: install/apply belongs to an installer
binary, not to `tabula serve`, `tabula run`, or other kernel commands.

## Proposed Files

Default project files:

```text
tabula.app.toml
tabula.app.lock
.tabula/local.toml      # gitignored, optional
```

`tabula.app.toml` is the committed source of truth for applying a distro to the
repository.

`tabula.app.lock` is generated by the installer and committed when the project
wants reproducible setup.

`.tabula/local.toml` is developer-specific. It must not change source
resolution or lock contents.

There is intentionally no committed `.tabula/secrets.toml` in the base model.
Secret values should come from environment, local config, files, OS-specific
secret stores later, or `$TABULA_HOME/secrets.json` when the distro/component
uses that store.

## Manifest Shape

Example:

```toml
[application]
id = "tabula"
name = "Tabula development agent"

[distro]
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"
name = "claw"

[values.workspace]
path = "${project_root}"
external_skill_roots = [
  "${project_root}/skills",
  "${project_root}/.agents/skills",
]

[values.prompt]
project_files = ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]
create_missing_project_files = true
include_external_skills = true

[values.tools]
fs_roots = ["${project_root}", "${project_root}/.agents"]
exec_cwd = "${project_root}"

[values.provider]
name = "anthropic"
model = "claude-sonnet-4-6"

[[values.permissions.rules]]
tool = "exec_run"
command = "git push *--force*"
effect = "ask"
```

This example intentionally has no `[instructions]` and no `[features]`.

`[values]` is a distro-owned TOML tree. The installer preserves it and passes it
to the distro contract/materializer. For `claw`, the materializer may map values
into existing tenant-local config files such as:

```text
$TABULA_HOME/tenants/<application-id>/config/tenant.toml
$TABULA_HOME/tenants/<application-id>/config/plugins/<plugin-id>/config.toml
$TABULA_HOME/tenants/<application-id>/config/clients/<client-id>/config.toml
```

The installer must not interpret `values.workspace`, `values.prompt`, or
`values.tools` as platform-level concepts. Those names belong to `claw` in this
example.

Template variables should be small and installer-owned:

- `${project_root}`: absolute repository root being applied
- `${application_id}`: stable application id from the manifest
- `${tabula_home}`: active `TABULA_HOME`

Do not invent a large template language in the first version.

## What Belongs In The Manifest

The manifest should contain:

- application identity
- distro source and intended distro name
- distro-defined values
- optional local override include paths
- lock policy such as frozen/update behavior

The manifest should not contain:

- distro prompt templates
- generic `instructions.files`
- generic `features`
- executable tool definitions
- plugin implementation paths
- secret values
- runtime state

If a distro needs a project to choose among distro-defined modes, the distro
should expose that as config. For example:

```toml
[values.profile]
profile = "coding"
```

That is a distro-specific values contract, not a universal platform-level
`features` abstraction.

## Relationship To Distros

The application manifest applies a distro. It does not replace the distro.

```text
distro package + application values + local overrides -> applied instance
```

The distro contributes:

- boot behavior
- installed templates
- prompt composition policy
- bundled components
- default plugin/client/skill config contracts
- default provider, gateway, workspace, and tool-visibility policy when the
  distro has such policy

The application manifest contributes:

- which distro source to resolve
- which project identity to use
- which distro-defined values to pass to the application contract
- which local overrides are allowed

This keeps ownership clean. A `claw` application can set
`[values.workspace].path = "${project_root}"`, but the meaning of workspace
files, template creation, first-run behavior, and prompt assembly still belongs
to `claw`.

## Prompt Ownership Boundary

Prompt assembly is not an SDK concern.

Shared driver/runtime libraries can provide mechanical helpers such as provider
initialization, context injection, tool visibility, and a narrow extension point
for a prompt builder. They should not define which project files exist, which
templates are required, or how a concrete agent identity is composed. Those are
distro or distro-bundle decisions.

For `claw`, the prompt policy should move into a `claw`-owned bundle or
distro-local component that owns:

- `SYSTEM.md`, `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md` composition
- `IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md` project file policy
- first-run identity setup text
- external instruction-only skill presentation
- provider-specific prompt variants if `claw` needs them

The application manifest should only provide config that this `claw` prompt
component consumes, such as workspace/project root. It should not grow a generic
`instructions` section to paper over misplaced prompt logic.

## Binding Model

The platform should not define semantic application scopes such as workspace,
project, org, chat, profile, or global. Those belong to the distro.

The installer only owns technical bindings: how a launcher selects an app id.

Bindings:

- `manual`: app exists but is selected only explicitly.
- `directory`: app is selected when launching from a directory tree.
- `default`: fallback app when no explicit or directory binding matches.

Example registry:

```toml
default = "personal-claw"

[[directory]]
root = "/Users/mak/src/tabula"
app = "claw-tabula"
```

Resolution order:

1. Explicit CLI app id.
2. `TABULA_APP_ID`.
3. Nearest parent directory binding.
4. Default binding.
5. No app selected: fail with an actionable message.

`default` is user-wide fallback selection, not a platform-level global agent
scope. Avoid `--global`; use explicit binding commands such as:

```bash
tabula-install app bind personal-claw --default
```

## Resolution And Locking

`apply --lock` should:

- resolve the distro source
- reuse `tabula-distro` source parsing and cache behavior
- include the resolved distro lock information
- include application manifest version and application id
- include exact source refs, paths, and resolved shas
- exclude local override values and secret values

`apply --frozen` should:

- require `tabula.app.lock`
- avoid network resolution
- fail if the lock is missing or cannot satisfy the manifest

`apply` should:

- verify or create a lock depending on mode
- install or update the distro generation as needed
- materialize or update the app instance under `TABULA_HOME`
- invoke the distro-owned materializer when declared
- create or update app/tenant config under `TABULA_HOME`
- refresh the runtime surface for the relevant app instance
- trigger the existing reload touch file on success

## Trust And Security

Application manifests are executable-surface selectors because they choose a
distro source and may configure plugins. Treat them like `package.json`,
`devcontainer.json`, `flake.nix`, or a CI workflow file.

Security rules:

- The installer must show the resolved distro source before first apply.
- Default binding changes must be explicit.
- Lockfiles must not contain secret values.
- Config values that are secret-bearing should be references or local-only
  values, never committed raw credentials.
- Plugin tools remain plugin-owned. The manifest can configure plugins but must
  not define plugin tools.
- An audit command should print installed distro, source refs, plugins, clients,
  hooks, and config files that will be written.

## Open Questions

- Should the command stay under `tabula-distro`, or should there be one renamed
  installer binary that owns both distro install and app apply?
- Should the file be `tabula.app.toml`, `.tabula/app.toml`, or a distro-specific
  filename?
- Should app identity be explicit only, or can it be derived from the
  repository URL/path when omitted?
- Should the first implementation write tenant-scoped config or a new
  application namespace under `TABULA_HOME`?
- How should a running gateway select the applied app/tenant when the
  user starts a session inside a project directory?
- Should distros declare an application values schema, so the installer can
  validate values before invoking a materializer?

## Recommended First Slice

1. Keep `tabula` unchanged.
2. Add `tabula-distro app apply` or `tabula-install app apply` as the first
   installer-owned command, reusing the existing resolver, cache, install, lock,
   and reload machinery.
3. Support `tabula.app.toml` with `[application]`, `[distro]`, and `[values]`
   only.
4. Add binding registry and app resolution.
5. Use tenants as the application instance substrate.
6. Write config through distro-owned materializers into SDK-compatible
   tenant-local config paths.
7. Generate `tabula.app.lock` from the resolved distro lock plus application
   metadata.
8. Add `tabula-install app audit ./tabula.app.toml` before default binding
   changes.
9. Defer arbitrary bundle/plugin source additions, universal features, and any
   generic instructions model until a real distro needs them.
