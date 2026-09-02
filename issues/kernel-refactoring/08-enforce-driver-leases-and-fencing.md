# Enforce driver leases, generations, and fencing

**Type:** AFK  
**Status:** completed

## What to build

Add authoritative session driver ownership. A session has at most one active driver generation. Registration, heartbeat, release, takeover, expiry, and all driver-originated mutations are validated by kernel against the current lease.

## Acceptance criteria

- [x] Driver lease includes session, instance, generation, revision, and expiry.
- [x] Takeover monotonically increments generation.
- [x] Events from expired, released, disconnected, or older generations are rejected without changing state.
- [x] Heartbeat and lease expiry are deterministic under clock and reconnect races.
- [x] Two runtime instances cannot both mutate one session as active driver.
- [x] Driver registration is bound to authenticated runtime/worker identity and session assignment.
- [x] Disconnect produces an authoritative driver-lost transition and wakes durable recovery/queue logic without gateway intervention.
- [x] Focused race tests cover concurrent registration, takeover, stale terminal events, and expiry.

## Blocked by

Issues 04 and 07.
