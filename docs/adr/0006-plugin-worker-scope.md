# ADR 0006: Plugin Worker Scope

Status: Accepted
Date: 2026-06-29

## Context

Tabula workers were keyed by kernel, tenant, and target. That is correct for
tenant-local tools such as filesystem, exec, and tenant hooks, but it forces
gateway-style plugins to simulate singleton behavior with PID files while the
runtime still starts one wrapper per tenant.

External gateways and shared sidecars need one warm worker per runtime while
still serving tenant-scoped calls and events.

## Decision

Plugin manifests support `worker.scope`:

- `tenant` is the default and keeps the existing one-worker-per-tenant-target
  lifecycle.
- `runtime` creates one warm worker per runtime target, shared by all tenants
  served by that runtime.

`worker.scope = "runtime"` is valid only with `worker.mode = "warm"`. Worker call
and event frames include `tenant_id` so runtime-scoped workers can route each
request without relying on init-time tenant identity.

Runtime attach primes runtime-scoped warm targets so explicit sidecars such as
web gateways are available without a first tool call. Tenant-scoped warm targets
remain lazy and start only when invoked, hit by a hook, or explicitly reloaded.

## Consequences

Tenant-scoped plugins remain the safe default. Runtime-scoped plugins must treat
tenant identity as per-request data and keep tenant state/config isolated in
their own implementation.

Singleton guards such as PID files may still be used defensively by gateway
plugins, but runtime lifecycle no longer relies on spawning duplicate wrappers
and letting the plugin reject the duplicates.
