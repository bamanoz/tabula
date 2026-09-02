# Migrate gateways and client SDKs to the v4 client API

**Type:** AFK  
**Status:** completed

## What to build

Migrate gateway-web, gateway-api, and their SDK/test helpers to the v4 client
command/query/subscription API. The only currently supported first-party
gateway surfaces are web and API.

Gateways remain replaceable bundle components, but kernel treats them as ordinary authenticated client actors. They do not receive a session lease, start drivers, probe readiness, rejoin to wake execution, or maintain a second input queue.

## Acceptance criteria

- [x] Gateways submit durable inputs with stable idempotency IDs.
- [x] Gateways render authoritative accepted, queued, running, terminal, and recovery-required projections.
- [x] Reconnect uses snapshot plus cursor replay.
- [x] Browser optimistic state rolls back on rejection and always clears on terminal/recovery outcomes.
- [x] Web, API, and test client semantics are identical.
- [x] Existing gateway-specific driver wake/retry logic is removed.
- [x] Gateway bundle manifests and startup dependencies do not encode kernel driver semantics.
- [x] Focused Python/frontend tests and installed gateway reconnect tests pass.

## Blocked by

Issues 06, 10, and 11.
