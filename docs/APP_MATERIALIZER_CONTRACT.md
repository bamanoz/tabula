# App Materializer Contract

Status: accepted

## Purpose

`tabula-install app install`, `tabula-install app apply`, and
`tabula-install app run` invoke an optional distro-owned application
materializer after the installer has resolved the app manifest, written app
metadata, and refreshed the app tenant's runtime surface.

The materializer is where a distro maps opaque `[values]` into tenant-local
config, prompt/boot metadata, plugin config, client config, and state defaults.
The generic installer must not interpret distro-specific names such as
`values.workspace`, `values.prompt`, `values.tools`, or `values.mempalace`.

## Distro Declaration

A distro opts in through `distro.toml`:

```toml
[application_contract]
materializer = "python3 materialize.py"
```

The command is parsed with shell-style quoting and executed with the distro root
as the working directory. If no materializer is declared, app install/apply/run
still materializes generic app metadata and bindings.

## Installer Responsibilities

Before invoking the materializer, the installer owns these mechanical steps:

- parse and validate `tabula.app.toml`;
- expand installer-owned variables such as `${project_root}`,
  `${application_id}`, `${tabula_home}`, and `${local.*}`;
- resolve and write the app lockfile;
- check distro-declared `runtime_requirements.executables` against `PATH`;
- create `TABULA_HOME/tenants/<app-id>/tenant.toml`;
- write `app.toml`, `app.lock.json`, and `values.toml` in the app tenant;
- clear any previous `TABULA_TENANT_DIR/config/` materializer output so app
  re-apply/run cannot inherit stale distro-specific config;
- install or refresh the distro runtime surface for the app tenant;
- apply app binding registry entries;
- for `app install` and `app run`, write managed runtime topology config;
- for `app run`, start/reuse the requested kernel/runtime.

## Materializer Responsibilities

The materializer owns distro semantics:

- validate the distro's `[values]` schema;
- write tenant-local config under `TABULA_TENANT_DIR/config/`;
- write plugin defaults under
  `TABULA_TENANT_DIR/config/plugins/<plugin-id>/defaults.toml`;
- write client config under
  `TABULA_TENANT_DIR/config/apps/<client-id>/config.toml`;
- write distro-owned boot metadata under the app tenant;
- configure prompt policy and project files for the distro;
- configure tenant-local or explicitly shared plugin state such as memory.

The installer owns the final plugin runtime surface. After the materializer runs,
it deep-merges each tenant plugin `defaults.toml` with the user-owned global
`config/plugins/<plugin-id>/config.toml` and writes the effective result to
`TABULA_TENANT_DIR/config/plugins/<plugin-id>/config.toml`. Plugin code reads
that effective tenant file when it exists.

Materializers must be idempotent. Re-running `app install`, `app apply`, or
`app run` with the same manifest should converge on the same tenant config.

## Environment

The installer passes these environment variables:

```text
TABULA_HOME              runtime/config/state root
TABULA_APP_ID            application id from [application]
TABULA_TENANT_ID         same value as TABULA_APP_ID
TABULA_TENANT_DIR        TABULA_HOME/tenants/<app-id>
TABULA_APP_MANIFEST      resolved manifest path
TABULA_APP_LOCK          resolved app lock path
TABULA_APP_LOCK_JSON     TABULA_TENANT_DIR/app.lock.json
TABULA_APP_VALUES        TABULA_TENANT_DIR/values.toml
TABULA_APP_PHASE         apply | run | audit
TABULA_APP_DRY_RUN       1 when app run --dry-run is active, otherwise 0
```

`app install` uses `TABULA_APP_PHASE=apply` because it materializes durable
tenant config without launching a session.

The materializer should read values from `TABULA_APP_VALUES`. It may read the
manifest or lock when it needs topology or source metadata, but `[values]` remain
the distro-owned configuration contract.

## Failure Behavior

A non-zero materializer exit code fails the installer command. The installer
includes captured stderr, or stdout if stderr is empty, in the error message.

Materializers should print actionable diagnostics and must not log secrets or
raw secret values.

## Audit And Dry Run

`TABULA_APP_PHASE=audit` is reserved for future materializer audit checks. The
current installer audit command does not invoke materializers.

`TABULA_APP_DRY_RUN=1` means the installer is planning an `app run` without
starting kernel/runtime processes. Materializers should still be safe and
idempotent; if a distro needs a pure preview mode it should avoid irreversible
side effects when this flag is set.

## Boundaries

- Do not add `app install`, `app apply`, or `app run` behavior to the `tabula`
  kernel binary.
- Do not add workspace, project, prompt, memory, or tool policy semantics to the
  kernel.
- Do not introduce generic platform sections such as `[instructions]`,
  `[features]`, or `system_prompt`.
- Do not store raw secrets in manifests, app locks, or materializer diagnostics.
