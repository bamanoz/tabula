# Tenant Materializer Contract

Status: accepted

## Purpose

`tabula-install tenant install` creates one project-scoped tenant without an app
manifest. `tabula-install tenant materialize` re-runs the installed tenant
materializer with a replacement values file for bundle/gateway-managed updates:

```sh
tabula-install tenant install <distro-source> \
  --id <tenant-id> \
  --root <project-directory> \
  [--values <values.toml>]

tabula-install tenant materialize <tenant-id> \
  --root <project-directory> \
  --values <values.toml>
```

`<distro-source>` is a full `git+...@ref#path=...` URI, `local:` URI, or local
directory. Generic installer owns source resolution and runtime mechanics.
Distro-owned materializer maps opaque values and project context into
tenant-local product configuration.

## Distro Declaration

A runnable distro declares tenant metadata in `distro.toml`:

```toml
[tenant_contract]
materializer = "python3 tenant/apply.py"
values_schema = "values.schema.json"
values_defaults = "values.defaults.toml"
```

`values_schema` and `values_defaults` are optional files owned and interpreted by
distro.

Installer executes command from stable installed distro directory
`$TABULA_HOME/distrib/<name>/`. It never resolves tenant materializer through
original source checkout.

Missing declaration is valid. Generic tenant metadata, lock, component links,
and runtime config are still materialized.

## Installer Responsibilities

Before invoking materializer, installer:

- validates tenant ID using kernel tenant grammar;
- requires existing project directory;
- rejects existing tenant ID instead of overwriting it;
- resolves distro source and exact Git revision when applicable;
- checks distro runtime executable requirements;
- transactionally installs distro at `$TABULA_HOME/distrib/<name>/`;
- writes `TABULA_HOME/tenants/<id>/install.lock.json` with version 2, resolved
  distro lock, and stable `distrib/<name>` path;
- links tenant `plugins/`, `skills/`, `apps/`, `templates/`, and `packages/`
  directly to installed distro tree;
- writes `tenant.toml` and normalized tenant-local `values.toml`;
- clears stale tenant `config/` before invoking materializer;
- compiles tenant plugin defaults with user-owned global plugin config;
- registers tenant-specific plugin and skill directories in `runtime.toml`;
- signals runtime reload after successful completion.

Failed new tenant install removes incomplete tenant directory. Failed distro
replacement restores prior installed tree and runtime surfaces before returning
an error. `tenant materialize` requires authoritative version 2 install lock,
executes materializer from its stable distro path, replaces normalized tenant
values, recompiles plugin/runtime config, and signals reload. Callers updating
one values section must preserve all other distro-owned values in replacement
file.

## Materializer Responsibilities

Materializer may:

- validate distro-owned values;
- write deterministic product defaults under `TABULA_TENANT_DIR/config/`;
- write plugin defaults under
  `TABULA_TENANT_DIR/config/plugins/<plugin-id>/defaults.toml`;
- write tenant prompt metadata and tenant-scoped state defaults;
- derive filesystem and execution policy from `TABULA_TENANT_PROJECT_ROOT`.

Materializer must be idempotent and must not write global kernel or runtime
configuration. It must not create bindings, select another tenant, launch
processes, resolve sources, or mutate another tenant.

## Environment

Installer passes only tenant-oriented contract variables:

```text
TABULA_HOME                  runtime/config/state root
TABULA_TENANT_ID             local tenant ID
TABULA_TENANT_DIR            TABULA_HOME/tenants/<id>
TABULA_TENANT_DISTRO_DIR     stable installed distro directory
TABULA_TENANT_PROJECT_ROOT   resolved project directory
TABULA_TENANT_VALUES         normalized tenant values.toml
TABULA_TENANT_INSTALL_LOCK   authoritative tenant install.lock.json
```

Python materializers can import packages exported by installed distro through
its `packages/python/src` surface.

## Host-Local Binding

Successful `tenant install --root PROJECT` writes directory binding to
`$TABULA_HOME/bindings.toml` after materialization and runtime config succeed.
Binding contains only `root` and `tenant`. Rewriting same binding is idempotent;
replacing another tenant requires `--replace-binding`.

## Agent Starting

`tabula-agent` resolves tenant from longest cwd directory binding, or from
default binding only when no directory match exists. `--tenant <id>` selects an
installed tenant explicitly. Missing binding returns one error with both
`tabula-agent install --distro <source> --bind <path>` and `--tenant <id>`
remedies.

It ensures kernel plus selected tenant runtime are ready, then exits. Gateway,
frontend, and other client lifecycle belongs to installed components and their
plugins; `tabula-agent` does not require or execute an `apps/<id>` component.

## Lock And Runtime Isolation

`install.lock.json` is authoritative for tenant distro ownership. Version 2
contains resolved distro lock data and stable path `distrib/<name>`. Runtime
component discovery uses tenant-specific links and `[[tenant]]` entries in
`config/runtime.toml`. Root-level `plugins/`, `skills/`, and other runtime links
are compatibility surfaces and do not select a separate tenant version.

Installer transaction scratch under `$TABULA_HOME/run/install/<name>/` is
private crash-recovery state and is removed after commit or rollback.

## Boundaries

- Project binding registry and tenant selection belong to launcher/client
  policy, not this operation or kernel.
- Distro values are opaque to installer.
- Project, prompt, provider, memory, gateway, and tool policy remain
  distro/materializer concerns.
- Secrets must not be written to values, install locks, or diagnostics.
- Kernel code must not interpret distro, project, profile, or binding metadata.
- Candidate retention, health, known-good selection, rollback policy, and
  release recovery belong to external supervisors, not installer.
