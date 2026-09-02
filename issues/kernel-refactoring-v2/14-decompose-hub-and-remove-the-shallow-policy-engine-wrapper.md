# Decompose Hub and remove the shallow PolicyEngine wrapper

**Type:** AFK  
**Status:** proposed

## What to build

Reduce `Hub` to connection/runtime composition and route coordination after the preceding ownership extractions. Move durable execution, tool coordination, exchanges, live membership, catalog routing, and lifecycle schedulers behind explicit modules.

Delete or rename `PolicyEngine`. Authentication, capability checks, hook dispatch, and bundle-owned policy must have precise owners; a policy-free kernel should not expose a shallow object called the single security policy engine.

## Evidence

`internal/kernel/kernel.go` stores client/session/process registries, hooks, policy, tools, runtimes, all durable agent services, exchanges, lifecycle maps, catalog refresh state, init metadata, and numerous mutexes. `internal/kernel/policy.go` mostly delegates checks and hook dispatch back into Hub.

## Acceptance criteria

- [ ] `Hub` constructor wires explicit modules instead of allocating their internal state maps directly.
- [ ] Durable agent services can be tested without constructing a full Hub.
- [ ] Tool and exchange coordinators have independent lifecycle and cleanup contracts.
- [ ] Authentication, transport capability checks, and bundle hook decisions are named separately.
- [ ] `PolicyEngine` is removed; no replacement god-object is introduced.
- [ ] Public behavior, protocol errors, and runtime attachment semantics remain stable.
- [ ] Package dependency direction is documented and has no cycle through Hub callbacks.

## Blocked by

Issues 05, 06, 11, and 13.
