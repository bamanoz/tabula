# Agent Applications

Agent applications are runnable definitions for applying a Tabula distro. A
committed `tabula.app.toml` should contain everything needed to install the
distro, create the app tenant, start or reuse the selected kernel and runtime,
and bind launchers to the app, except for secrets and machine-local values.

## Concepts

- **Distro**: reusable agent package such as `claw`, `code`, or `guardian`.
- **Application**: named runnable instance of a distro. `application.id` is the
  tenant id used on client joins and runtime catalog isolation.
- **Kernel**: WebSocket server the app connects to.
- **Runtime**: separate process that hosts plugin workers.
- **Execution backend**: where the runtime executes workers/commands. Managed
  local runtime execution currently supports `bare`.
- **Binding**: how launchers choose app and kernel from a directory or default.

The kernel does not define workspace, project, chat, org, or prompt-file
semantics. Those belong to the distro and its `[values]` contract.

## Minimal Claw App

```toml
[application]
id = "claw-tabula"
name = "Claw for Tabula"

[distro]
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"

[values.workspace]
path = "${project_root}"

[values.prompt]
project_files = ["AGENTS.md"]
create_missing_project_files = false
```

`[distro]` intentionally has only `source`. The distro id/name are resolved from
the distro's own `distro.toml` and recorded in the app lock.

If omitted, the installer defaults to a managed local kernel at
`ws://127.0.0.1:8089/ws`, one managed bare runtime for `application.id`, and a
directory binding from `${project_root}` to the app.

Install it for the current workspace without launching the kernel/runtime:

```bash
tabula-install app install --workspace .
```

Install it as the fallback app outside more specific workspace bindings:

```bash
tabula-install app install --global
```

`app install` resolves and installs the distro from the manifest, creates the
tenant metadata, runs the distro materializer, compiles tenant plugin config,
writes runtime config, and records the requested binding target. `--global` and
`--workspace` are installer UX aliases for default and directory bindings; they
are not platform-level agent scopes.

Start or reuse the configured kernel/runtime:

```bash
tabula-install app run
```

On a fresh machine, the release installer can install Tabula and then forward to
`tabula-install` in the same command:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- app run
```

For a private Tabula repository, fetch the installer with an authenticated raw
request and pass the same token to the installer so it can download release
assets:

```bash
token=$(gh auth token)
curl -fsSL \
  -H "Authorization: Bearer $token" \
  https://raw.githubusercontent.com/bamanoz/tabula/v0.9.3/scripts/install.sh \
  -o /tmp/tabula-install.sh
GITHUB_TOKEN="$token" bash /tmp/tabula-install.sh app run
```

`tabula-install` is the final installer entrypoint. The older `tabula-distro`
entrypoint is the same installer package but should not be used in docs for new
app workflows.

Dry-run the plan:

```bash
tabula-install app run --dry-run
```

By default, app commands look for `./tabula.app.toml` and then
`./.tabula/app.toml`. Pass a manifest path only for non-standard locations.

## Local Overrides

Use `.tabula/local.toml` for machine-specific values. Do not commit this file.

```toml
[ssh]
host = "makbook.local"
user = "mak"

[mempalace]
personal_path = "/Users/mak/.tabula/shared-mempalace/personal"
```

Reference local values from the manifest:

```toml
[values.mempalace]
mode = "shared"
path = "${local.mempalace.personal_path}"
```

Secrets should use environment variables, the local secret store, or local-only
config. Lockfiles must not contain raw credentials.

## Bindings

`app install --global`, `app install --workspace`, `app apply`, and `app run`
materialize bindings into `$TABULA_HOME/app-bindings.toml`.

```toml
[[bindings.directory]]
root = "${project_root}"
```

When `app` or `kernel` are omitted, they default to `application.id` and the
selected kernel id.

Use `app install --no-bind` to materialize tenant/runtime state without changing
selection. The app can still be selected explicitly by id.

Launchers resolve in this order:

1. explicit `--app` / `--kernel`
2. `TABULA_APP_ID` / `TABULA_KERNEL_ID`
3. nearest directory binding
4. default binding

If no app is selected, launchers fail with an actionable message rather than
silently joining the default tenant. The selected app id is sent as the protocol
`tenant_id` on join.

## Inspect

Use inspect to audit the materialized app surface:

```bash
tabula-install app inspect claw-tabula --json
```

Inspect reports the tenant directory, required app files, lock/source metadata,
bindings, runtime config surface, distro runtime executable requirements,
materializer output, and missing/stale issues.

## Memory Modes

Claw memory is tenant-local by default:

```toml
[values.mempalace]
mode = "project"
```

The materializer writes each memory plugin to:

```text
$TABULA_HOME/tenants/<app-id>/state/plugins/mempalace
```

Shared memory is explicit and distro/plugin-owned:

```toml
[values.mempalace]
mode = "shared"
path = "${local.mempalace.personal_path}"
```

Multiple apps can point at the same shared path when that is desired.

## Multiple Apps

Multiple applications can run against one kernel when each app has its own
tenant id and runtime catalog surface. The local runtime config contains one
`[[tenant]]` catalog per app, and reload coalesces rapid app updates by reloading
the full local tenant set. Duplicate plugin ids such as `fs`, `exec`, and
`mempalace-save` are scoped by tenant.

Installed coverage verifies two live Claw apps in one kernel with isolated
`fs_write`/`fs_read`, tenant-specific `exec` cwd, project memory for one app, and
explicit shared memory for the other.

## Current Limits

`bare` is the only managed runtime execution backend implemented today. `ssh`
and `docker` are reserved manifest shapes, but that backend work is paused and
tracked separately from the runnable app-manifest path.

The kernel/runtime substrate remains generic tenant-aware infrastructure. Distro
application contracts own `[values]`, prompt policy, materializer behavior,
workspace/project semantics, and product-specific memory modes.
