# Prove the reduced kernel contract end to end

**Type:** AFK  
**Status:** proposed

## What to build

Run the deletion test for kernel refactoring v2. Remove all superseded surfaces and prove the installed system retains authoritative coordination guarantees without legacy authority or product semantics in core.

Tests must execute installed kernel, runtime, driver, gateways, bundles, and distro materialization rather than inspect source or catalogs only.

## Required scenarios

- same session ID in multiple tenants with runtime-originated plugin events;
- assignment and permit redelivery after replay/outbox retention expiry;
- kernel restart with queued, assigned, prepared, permitted, cancelling, and recovery-required turns;
- duplicate commands after later commits using repository-only deduplication;
- driver crash before and after permit;
- stale generation and late tool-result rejection;
- prompt preparation and custom output kinds through their new external owners;
- runtime worker crash/shutdown without kernel process supervision;
- install/reload with distro-materialized startup dependencies;
- cursor-expired snapshot recovery and cancellation/completion races.

## Acceptance criteria

- [ ] All scenarios pass through installed components in canonical testbeds.
- [ ] Generated testbed templates are synchronized.
- [ ] Core focused and baseline tests pass with `-race -count=1`; build, vet, lint, and formatting checks pass.
- [ ] No active source, test, guide, or mutable architecture doc references removed surfaces.
- [ ] Search-based boundary checks find no concrete provider, prompt-builder, workspace, tool-name, user-role, or distro-kind policy in kernel.
- [ ] Legacy session JSON, process supervisor, `AppliedCommands`, outbox payload rediscovery, and `PolicyEngine` are absent.
- [ ] Final architecture documentation states the minimal authoritative kernel contract and ownership of every extracted concern.

## Blocked by

Issues 01 through 16.
