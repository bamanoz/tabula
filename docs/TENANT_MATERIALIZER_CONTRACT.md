# Tenant Materializer Contract

Status: accepted

## Purpose

`tabula-install tenant install` creates one project-scoped tenant without an app
manifest. `tabula-install tenant materialize` re-runs the pinned tenant
materializer with a replacement values file for bundle/gateway-managed value
updates:

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
directory. The generic installer owns source resolution and runtime mechanics.
The distro-owned materializer maps opaque values and project context into
tenant-local product configuration.

## Distro Declaration

A runnable distro declares tenant metadata in `distro.toml`:

```toml
[tenant_contract]
materializer = "python3 tenant/apply.py"
values_schema = "values.schema.json"
values_defaults = "values.defaults.toml"
```

`values_schema` and `values_defaults` are optional generation-local files owned
and interpreted by the distro.

The installer stores `distro.toml` in the immutable generation and executes the
command from that exact generation directory. It never resolves a tenant
materializer through `distrib/active` or the original source checkout.

A missing declaration is valid. Generic tenant metadata, lock, component links,
and runtime config are still materialized.

## Installer Responsibilities

Before invoking the materializer, the installer:

- validates the tenant ID using the kernel tenant grammar;
- requires an existing project directory;
- rejects an existing tenant ID instead of overwriting it;
- resolves the distro source and exact Git revision when applicable;
- checks distro runtime executable requirements;
- creates or reuses an immutable distro generation;
- writes `TABULA_HOME/tenants/<id>/install.lock.json` with source, resolved
  revision, and generation identity;
- links tenant `plugins/`, `skills/`, `apps/`, `templates/`, and `packages/`
  directly to the pinned generation;
- writes `tenant.toml` and a normalized tenant-local `values.toml`;
- clears stale tenant `config/` before invoking the materializer;
- compiles tenant plugin defaults with user-owned global plugin config;
- registers tenant-specific plugin and skill directories in `runtime.toml`;
- signals runtime reload after successful completion.

A failed new tenant install removes the incomplete tenant directory. An already
installed immutable distro generation may remain available for reuse.
`tenant materialize` requires an existing authoritative install lock, executes
only that pinned generation, replaces normalized tenant values, recompiles
plugin/runtime config, and signals reload. Callers updating one values section
must preserve all other distro-owned values in the replacement file.

## Materializer Responsibilities

The materializer may:

- validate distro-owned values;
- write deterministic product defaults under `TABULA_TENANT_DIR/config/`;
- write plugin defaults under
  `TABULA_TENANT_DIR/config/plugins/<plugin-id>/defaults.toml`;
- write tenant prompt metadata and tenant-scoped state defaults;
- derive filesystem and execution policy from `TABULA_TENANT_PROJECT_ROOT`.

The materializer must be idempotent and must not write global kernel or runtime
configuration. It must not create bindings, select another tenant, launch
processes, resolve sources, or mutate another tenant.

## Environment

The installer passes only tenant-oriented contract variables:

```text
TABULA_HOME                  runtime/config/state root
TABULA_TENANT_ID             local tenant ID
TABULA_TENANT_DIR            TABULA_HOME/tenants/<id>
TABULA_TENANT_DISTRO_DIR     exact immutable distro generation
TABULA_TENANT_PROJECT_ROOT   resolved project directory
TABULA_TENANT_VALUES         normalized tenant values.toml
TABULA_TENANT_INSTALL_LOCK   authoritative tenant install.lock.json
```

Python materializers can import packages exported by their pinned generation
through its `packages/python/src` surface.

## Host-Local Binding

Successful `tenant install --root PROJECT` writes a directory binding to
`$TABULA_HOME/bindings.toml` after materialization and runtime config succeed.
The binding contains only `root` and `tenant`. Rewriting the same binding is
idempotent; replacing another tenant requires `--replace-binding`.

## Agent Starting

`tabula-agent` resolves tenant from longest cwd directory binding, or from the
default binding only when no directory match exists. `--tenant <id>` selects an
installed tenant explicitly. Missing binding returns one error with both
`tabula-agent install --distro <source> --bind <path>` and `--tenant <id>`
remedies.

It ensures kernel plus selected tenant runtime are ready, then exits. Gateway,
frontend, and other client lifecycle belongs to installed components and their
plugins; `tabula-agent` does not require or execute an `apps/<id>` component.

## Lock And Runtime Isolation

`install.lock.json` is authoritative for tenant generation ownership. Runtime
component discovery uses tenant-specific links and `[[tenant]]` entries in
`config/runtime.toml`. Root-level `distrib/active`, `plugins/`, and `skills/`
remain transitional compatibility surfaces and do not select versions for an
installed tenant.

Generation pruning preserves every generation referenced by a tenant install
lock.

## Boundaries

- Project binding registry and tenant selection belong to launcher/client
  policy, not this operation or the kernel.
- Distro values are opaque to the installer.
- Project, prompt, provider, memory, gateway, and tool policy remain
  distro/materializer concerns.
- Secrets must not be written to values, install locks, or diagnostics.
- Kernel code must not interpret distro, project, profile, or binding metadata.
