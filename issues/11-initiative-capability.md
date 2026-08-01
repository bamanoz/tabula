# Deliver initiative capability end to end

**Type:** AFK  
**Triage:** ready-for-human

## What to build

Create independent `initiative` bundle for budgeted autonomous background work: agenda, scheduled ticks, task selection, subagent execution, cooldowns, pause/stop, restart recovery, and durable owner notification.

## Required discovery and design

- Inspect Ouroboros background consciousness, supervisor scheduling, queue state, budget reserve, repeated no-op/failure breakers, owner stop sentinel, and notifications.
- Compare with Tabula cron/wait/delivery, durable tasks, subagents, session activity, and tenant config.
- Design explicit authority and spend policy. Installation must not silently enable unbounded background work.

## Acceptance criteria

- [x] Bundle supports manual, enabled, paused, resumed, and disabled states.
- [x] Agenda and run history survive worker/kernel restart.
- [x] Each tick enforces configured concurrency, time/cost/run budget, cooldown, and stop conditions before spawning work.
- [x] Durable tasks are claimed safely and outcomes/notifications are persisted without duplicate execution.
- [x] Circuit breakers stop repeated failure and repeated no-op behavior.
- [x] Optional activity/reflection/evolution integrations do not become hard dependencies.
- [x] No provider-specific or local-model invocation tool is introduced.
- [x] Installed testbed enables one bounded initiative run, observes result delivery, then proves pause prevents another run.

- [x] Canonical testbed and generated testbed template are updated; the suite executes a bounded installed initiative run and verifies pause/restart safety.

## Blocked by

- [03 Expose reusable subagent orchestration SDK](03-subagent-orchestration-sdk.md)
- [04 Expose durable task and scheduling primitives](04-task-scheduling-primitives.md)
