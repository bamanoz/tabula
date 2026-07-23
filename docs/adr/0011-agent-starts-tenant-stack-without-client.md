# ADR 0011 - Agent starts tenant stack without a client

Date: 2026-07-23
Status: Accepted
Supersedes: ADR 0009 frontend-launch behavior
Superseded by: nothing

## Context

ADR 0009 made `tabula-agent` resolve and execute a distro-declared default
frontend after selecting a tenant. That couples generic tenant startup to an
optional client component. A distro may provide no CLI gateway, may expose only
a Web or API client, or may let plugins supervise gateways independently.
Requiring `tenant_contract.default_frontend` therefore prevents otherwise valid
distros from starting and gives `tabula-agent` ownership of component-specific
client lifecycle.

## Decision

`tabula-agent` owns tenant selection and managed local stack readiness only.

- Without a subcommand, it resolves a tenant from `--tenant` or host-local cwd
  binding, ensures kernel and selected tenant runtime readiness, reports the
  ready tenant, and exits.
- `tabula-agent install` and `tabula-agent apply` retain their existing optional
  service-start behavior.
- `[tenant_contract]` contains materializer and values metadata only. It has no
  `default_frontend` field.
- `tabula-agent` does not inspect or execute tenant-local `apps/<id>/app.toml`.
- Gateway, frontend, and other client lifecycle belongs to installed components,
  plugins, service managers, or explicit client commands.
- Component `apps/<id>/app.toml` remains valid installed component launch
  metadata; this decision removes only its use by `tabula-agent`.

## Consequences

- Distros without gateways or frontends can use the standard agent install and
  startup path.
- Broken or absent CLI gateways cannot prevent kernel/runtime startup.
- `tabula-agent` remains generic and has no concrete component knowledge.
- Starting a stack no longer creates an interactive client session. Users reach
  clients through whichever surfaces their distro installs and starts.
