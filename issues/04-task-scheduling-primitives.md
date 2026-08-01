# Expose durable task and scheduling primitives

**Type:** AFK  
**Status:** completed

## What to build

Extend existing `todo` and `cron` owners with reusable durable task and scheduling APIs needed by independent background controllers. Keep all initiative/evolution policy outside these primitives.

## Required discovery and design

- Inspect Ouroboros supervisor queue, scheduling, leases, retries, owner stop state, budgets, and restart recovery.
- Compare with Tabula `todo`, `cron`, `wait`, durable delivery, plugin startup, and session scopes.
- Design generic APIs for atomic task mutation and durable scheduled delivery without importing Ouroboros campaign semantics.

## Acceptance criteria

- [x] Durable tasks support session, tenant, project, and caller-defined scopes.
- [x] Atomic add/update/claim/release/complete/fail operations use revisions or leases to prevent lost updates.
- [x] Scheduling SDK supports one-shot/recurring jobs, idempotent replacement, pause/resume, retry, and removal.
- [x] Existing `todo_read`/`todo_write` remain stable; legacy `cron_*` tools and OS crontab path are replaced by canonical `schedule_*` tools backed directly by `ScheduleService`.
- [x] Restart recovery and duplicate-delivery behavior are tested.
- [x] Installed fixture controller claims scheduled work and records completion end to end.
- [x] APIs contain no initiative, reflection, or evolution policy.

- [x] Canonical testbed and generated testbed template are updated; the suite executes installed scheduling/task components and covers restart or duplicate-delivery recovery.

## Blocked by

None - can start immediately.
