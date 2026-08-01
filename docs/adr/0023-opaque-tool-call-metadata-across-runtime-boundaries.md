# ADR 0023: Opaque Tool-Call Metadata Across Runtime Boundaries

- Status: Accepted
- Date: 2026-08-01
- Supersedes: none
- Superseded by: none

## Context

Kernel WebSocket tool calls already carry optional `meta` separately from tool
arguments. Permission hooks use this channel for normalized caller context such
as agent actor and turn correlation. Runtime-backed tools previously received
only tenant, session, turn correlation, target, tool, and arguments. Metadata
was therefore available to authorization hooks but disappeared before the
plugin worker executed the call.

Plugins that record approval or audit identity must not accept identity fields
from tool arguments. They need the caller context selected by the trusted
client/driver and inspected by policy hooks, while the kernel must remain a
generic router with no actor or product semantics.

## Decision

Runtime API `invoke` and worker protocol `call` frames carry optional opaque
`meta` JSON copied from the finalized kernel tool call. Kernel, runtime
connection, and runtime host forward it without interpreting or reshaping its
contents.

`tabula_plugin_sdk` exposes metadata entries in the tool handler context for
keys not already owned by protocol context. Metadata cannot replace `call_id`,
tenant, session, target, tool, or other fields already present on the worker
call. Tool arguments remain separate and never populate handler context.

Consumers that use metadata as authority must reject a missing required value
and ignore equivalent caller-supplied tool arguments. Authenticity follows the
existing authenticated client/driver and kernel-token trust boundary: a client
that can submit arbitrary authenticated tool-call metadata is already inside
that boundary. Stronger multi-principal deployments require distinct client
credentials or an upstream identity binding; this ADR does not invent identity
semantics in the kernel.

## Consequences

- Permission hooks and executing plugins observe the same finalized call
  metadata.
- Plugins can bind approvals and audit records to driver-selected actor context
  without adding identity parameters to tool schemas.
- Runtime and worker protocols gain one optional backward-compatible field but
  no product-specific semantics.
- Protocol-owned context wins over colliding metadata keys.
- Tests must cover metadata preservation through kernel dispatch, Runtime API,
  worker serialization, plugin context normalization, and installed execution.
