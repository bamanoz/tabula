# Add runtime-owned driver supervision and authenticated registration

**Type:** AFK  
**Status:** completed

## What to build

Make driver a first-class replaceable bundle component hosted by runtime. Kernel expresses desired driver state for a session; runtime starts, monitors, and stops the configured driver worker; the worker registers through an authenticated execution channel.

This issue establishes process and identity lifecycle only. It does not dispatch turns.

## Acceptance criteria

- [x] Bundle/component metadata distinguishes driver implementations from generic plugins and gateways.
- [x] Kernel can issue `driver.ensure` for a session and pinned AgentSpec revision.
- [x] Runtime starts one session-scoped driver worker and reports start, ready, exit, and initialization failure.
- [x] Driver identity cannot be self-asserted by an arbitrary WebSocket client.
- [x] Runtime restart reconstructs desired driver workers from authoritative kernel state.
- [x] Driver process failure never closes the session or depends on a gateway reconnect.
- [x] Provider, prompt, model, and driver implementation policy remain outside kernel.
- [x] Runtime API and worker protocol changes are documented and versioned as required.
- [x] Focused race tests and installed runtime/driver startup and restart tests pass.

## Blocked by

Issues 01 and 03.
