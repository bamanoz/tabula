# ADR 0009 - Project-scoped agent tenants

Date: 2026-07-22
Status: Accepted
Supersedes: ADR 0002, ADR 0004
Superseded by: nothing

## Context

ADR 0002 introduced an agent application manifest that combines a distro source,
an application id, kernel and runtime topology, bindings, and distro-owned
values. ADR 0004 added install-time workspace and default binding targets.

That model makes an internal runtime namespace part of source-controlled project
configuration: `application.id` is also the tenant id. It also lets a project
manifest select host-local kernel and runtime topology. These choices expose
platform machinery in the beginner path and couple repository configuration to
one local installation.

The current installer also exposes tenant surfaces through the globally active
distro generation. This prevents one `TABULA_HOME` from safely serving tenants
backed by different distros at the same time.

Tabula needs a one-line installation path without making the kernel aware of
distros, projects, workspaces, frontends, or product policy. It also needs a
plain-file customization path for advanced users.

## Decision

### Domain model

Use these concepts:

- **Distro**: a reusable agent product. It owns bundled surfaces, product
  defaults, runtime requirements, a values contract, materialization semantics,
  and its default frontend.
- **Agent profile**: the logical product configuration shared conceptually
  across projects. A profile is distro plus values; it is not a kernel object.
- **Project**: a user-facing workspace context.
- **Tenant**: a project-scoped runtime instance of an agent profile. It is the
  kernel/runtime isolation boundary for config, sessions, plugin state, worker
  environments, and runtime routing.
- **Binding**: host-local selection from a directory or user default to a tenant.
- **Frontend**: a distro-installed client component such as CLI, Web, or ACP.

For workspace-backed agents, one project has one backing tenant and one tenant
belongs to one project. One logical agent profile can therefore serve many
projects through many isolated tenants:

```text
agent profile
|- project A -> tenant A
|- project B -> tenant B
`- project C -> tenant C
```

This 1:1 project-to-tenant rule preserves the current tenant-scoped workspace,
filesystem permission, prompt, plugin config, state, and worker isolation.
Supporting multiple projects inside one tenant would require a separate
session-scoped execution-context design and is outside this decision.

### Installation and distro generations

A tenant is created locally when a distro is installed for a project. Tenant ids
are local installation identities and are not committed to a repository.

Each tenant pins an exact installed distro generation. Runtime surfaces resolve
from that generation, not from a global `distrib/active` selection. One
`TABULA_HOME` can therefore serve tenants backed by different distros.

The user supplies a distro as a full Git source URI or local path. Core does not
provide distro aliases or a product catalog. Installation preserves the source
URI and records its resolved revision and generation in the tenant install
lock. Updates are explicit.

### Bindings

Bindings are host-local and map directory roots or a user-wide default directly
to tenant ids. They do not contain app ids, kernel ids, or distro semantics.

A directory can select only one tenant by default. Installing another tenant for
an already bound directory fails unless the user explicitly requests binding
replacement. Replacing a binding does not delete the previously selected tenant.
Advanced callers can select a tenant explicitly.

### Optional project declaration

A source-controlled `tabula.agent.toml` is optional. It may contain only:

- a distro source;
- distro-owned values.

It does not contain tenant identity, bindings, secrets, kernel endpoints,
runtime ids, runtime tenant lists, or execution topology. Different clones of
the same declaration create different local tenants.

### Component ownership

- `tabula` remains the generic kernel and operator binary.
- `tabula-runtime` remains the execution host and worker supervisor.
- `tabula-install` owns source resolution, generations, tenant materialization,
  install locks, bindings, runtime config compilation, and installed surfaces.
- Distro materializers map opaque distro values into tenant-local config and
  state defaults. They do not write host kernel/runtime topology.
- `tabula-agent` is a separate generic user command. It installs or selects a
  tenant, resolves the distro-declared frontend from installed metadata, ensures
  the local service is ready, and launches that frontend. It does not know
  concrete distro or frontend ids.
- The kernel knows tenant ids, sessions, runtimes, hooks, clients, and tools. It
  does not read distro, project, binding, manifest, materializer, or frontend
  metadata.

### Local lifecycle

The operating-system user service owns the kernel process. The kernel owns its
managed local `tabula-runtime` child. The canonical local service command is:

```text
tabula serve --runtime-mode managed
```

The former runner command is removed. Foreground and service operation use the
same local stack topology.

### Replacement policy

The agent application model is removed atomically after its replacement paths
are complete. No deprecated CLI aliases, manifest parser, environment-variable
aliases, or parallel app/tenant binding models remain. Existing installations
are not migrated by a compatibility layer.

Mutable user documentation must continue to describe shipped behavior while the
implementation is delivered in vertical slices. It switches to this model only
when the corresponding behavior lands.

## Consequences

### Positive

- Beginner installation does not require a manifest, tenant id, kernel topology,
  or runtime topology.
- Project configuration is clone-safe and source-control friendly.
- Tenant isolation remains aligned with workspace, tools, prompts, sessions,
  plugin state, and workers.
- Multiple distros can run concurrently under one kernel/runtime installation.
- Kernel boundaries remain generic and product-neutral.
- Advanced users retain explicit sources, values, tenant selection, local paths,
  and inspectable lock files.

### Negative

- A logical agent profile is materialized once per project, creating more tenant
  directories and potentially more tenant-scoped workers.
- Generation management and garbage collection must account for references from
  every tenant lock.
- Removing the existing app model requires coordinated changes across Tabula,
  distros, bundles, testbeds, and installed documentation.
- Sharing project-specific runtime state across projects is not implicit. A
  distro must model deliberately shared services, credentials, or memory outside
  project-scoped tenant state.

## Rejected alternatives

### Keep `application.id` as tenant id in the project manifest

Rejected because local runtime identity would remain committed and clones would
collide or unintentionally share sessions and state.

### Make one tenant serve multiple projects

Rejected for this design because workspace roots, filesystem permissions,
prompt inputs, plugin config, state, and worker environments are tenant-scoped.
Doing this safely requires a new execution-context boundary.

### Make `tabula` the product launcher

Rejected because it would mix kernel/operator responsibilities with distro,
binding, service, and frontend policy.

### Use distro-owned launcher commands

Rejected as the default because arbitrary source-installed distros would need to
claim global command names and users would lose one stable launch command.
Distros still own the selected frontend implementation.

### Preserve one globally active distro

Rejected because changing that global selection changes runtime surfaces for
unrelated tenants and prevents concurrent products.

### Require a project manifest

Rejected because it blocks zero-config installation. The manifest remains an
optional reproducibility and customization surface.
