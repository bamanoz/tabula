# ADR 0030 - Synchronous tenant tool catalog readiness

Date: 2026-08-09
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Runtime attachment initially publishes manifest-derived capability previews.
Warm plugin workers publish their authoritative schemas and hooks only after
worker `init_ack`.

Driver startup previously took the kernel's prompt tool snapshot before
`before_prompt_build` lazily initialized those workers. Catalog updates arrived
after the snapshot, so the first provider turn after runtime startup or reload
received manifest placeholders while the next turn received complete schemas.
Retries or sleeps in prompt construction could reduce the symptom but could not
establish ordering against asynchronous catalog frames, and plugins without a
prompt hook could remain uninitialized.

The kernel must remain generic. Plugin lifecycle and readiness belong to the
runtime, while the kernel owns driver execution ordering and the prompt catalog
snapshot consumed by that driver.

## Decision

Add a synchronous Runtime API operation:

- kernel sends `prepare_tenant` with `request_id` and `tenant_id`;
- runtime initializes every warm, tenant-visible, non-driver plugin worker in
  dependency order;
- runtime returns `prepare_tenant_ack` with the complete capability snapshot for
  those warm targets after each target reaches a terminal `ready` or `failed`
  state;
- kernel applies every returned capability through its ordinary catalog update
  path before `driver.ensure` may proceed.

Cold workers remain lazy. Session-scoped driver workers are excluded from tenant
preparation. Runtime-scoped warm workers remain shared but are included in each
relevant tenant snapshot.

A failed optional plugin is represented by a failed capability and does not
block unrelated ready plugins. Missing capability state, request cancellation,
transport failure, or failure to apply the returned catalog aborts driver
startup. No timing sleeps or prompt-builder retries are part of the contract.

Runtime reconnect reconciliation runs in background after the attachment is
registered. It preserves one preparation barrier per tenant and runtime before
restoring that tenant's durable session drivers, but historical tenants do not
block attachment readiness. Detach and replacement cancel the attachment's
reconciliation task.

## Consequences

Positive:

- first provider turn after startup or reload receives worker-authoritative tool
  schemas and hooks;
- catalog readiness has an explicit request/reply boundary instead of relying on
  asynchronous frame timing;
- runtime retains plugin lifecycle and dependency policy;
- kernel remains a generic catalog sink and driver lifecycle coordinator;
- failed optional plugins do not make healthy tools unavailable.

Negative:

- starting or reconciling a driver now waits for all warm tenant-visible plugin
  workers to reach terminal readiness;
- reconnect reconciliation is asynchronous, so failures are reported through
  diagnostics rather than failing runtime attachment;
- Runtime API peers must implement the new mandatory operation together.

## Verification

Implementation must prove:

- Runtime API codec round-trips and validates both preparation frames;
- connection correlation applies returned capabilities before preparation
  returns;
- pool preparation returns worker-authoritative schemas and hooks;
- one failed plugin does not block unrelated ready plugins;
- supervisor calls preparation before `driver.ensure` and fails closed when
  preparation fails;
- first and second provider turns after isolated startup and reload expose the
  same tool-name and schema digest.
