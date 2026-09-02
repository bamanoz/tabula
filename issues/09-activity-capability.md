# Deliver activity capability end to end

**Type:** AFK  
**Status:** completed

## What to build

Create independent `activity` bundle providing a curated, cross-session narrative of work, decisions, artifacts, and outcomes. Keep it separate from raw hook logs, kernel committed events, and auxiliary session-record semantics.

## Required discovery and design

- Inspect Ouroboros task/event/artifact/outcome projections, supervisor events, gateway task state, and persistence/recovery behavior.
- Compare with Tabula committed session events, auxiliary session records, hook logger, artifacts, projects, and gateway consumers.
- Design a product-level event envelope, projections, correlation model, and retention rules without changing kernel event semantics.

## Acceptance criteria

- [x] Bundle records and queries structured work, decision, artifact, and outcome events across sessions.
- [x] Events carry tenant/project/session/actor/correlation metadata and artifact references.
- [x] Explicit SDK emitters exist for other optional bundles.
- [x] Hook-derived events are configurable and aggregated instead of logging every tool call as activity.
- [x] Current work and recent outcome projections recover after plugin restart.
- [x] Bundle works without continuity, reflection, initiative, or evolution.
- [x] Installed testbed records correlated work across two sessions and reads the resulting timeline/artifact.
- [x] Kernel committed events, auxiliary session records, and raw hook logger retain their existing ownership.

- [x] Canonical testbed and generated testbed template are updated; the suite executes installed activity tools across sessions and verifies persisted projections.

## Blocked by

- [01 Define modular capability architecture](01-modular-capability-architecture.md)
- [05 Expose durable artifact storage API](05-artifact-storage-api.md)
