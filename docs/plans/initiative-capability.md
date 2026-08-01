# Initiative Capability

Status: Approved
Date: 2026-07-31
Issue: `issues/11-initiative-capability.md`
Architecture: `docs/adr/0012-modular-agent-capabilities.md`
Task and scheduling primitives: `issues/04-task-scheduling-primitives.md`
Subagent SDK: `issues/03-subagent-orchestration-sdk.md`
Bundle ADR: `../tabula-bundles/docs/adr/0009-budgeted-autonomous-initiative.md`

## Source Study

Ouroboros background consciousness combines a durable agenda, periodic selection, queue leases, budget reserve, cooldowns, repeated-objective and consecutive-failure breakers, owner stop state, restart recovery, and durable notification. Those mechanics are useful, but campaign policy and provider execution do not belong in the Tabula kernel or generic task/scheduling primitives.

Tabula already provides tenant-local leased tasks, leased recurring schedules, typed subagent orchestration, and durable delivery. Initiative should compose those public contracts while owning authority, budget, breaker, and run policy.

## Decision

Add independent `initiative` bundle with one warm plugin and public Python SDK. Installation leaves every controller disabled. Explicit tools create agenda items, enable, pause, resume, disable, run one manual tick, and inspect control/run state.

Each controller uses dedicated bundle-owned `TaskService` and `ScheduleService` state directories. This avoids claiming unrelated todo or cron records while retaining generic lease and restart semantics. A deterministic recurring schedule creates ticks. One tick acquires a controller lease, checks authority and limits, claims one agenda task, and starts one typed subagent job. Run IDs and job IDs derive from controller, schedule delivery, task, and attempt.

## Authority And Limits

A controller is `disabled`, `enabled`, or `paused`. Resume returns paused state to enabled; disable pauses automatic work while retaining state. `initiative_run` is an explicit one-tick authorization while disabled; paused controllers reject manual and automatic work. Configuration requires explicit session, interval, and finite policy, while enabling activates only the durable schedule.

Every tick checks before spawning:

- controller state and schedule delivery lease;
- one controller execution lease and configured concurrency;
- maximum runs and cost units per UTC day;
- task time limit and declared task cost;
- global cooldown and per-task retry timing;
- consecutive failure and consecutive no-op thresholds.

No-op means a valid tick found no claimable task. Failure means subagent execution or finalization failed. Reaching a breaker pauses controller and persists reason. Successful work resets both streaks.

## State And Recovery

State lives under `$TABULA_TENANT_DIR/state/plugins/initiative/`:

- `controllers/<id>.json`: authority, policy, counters, lease, and breaker state.
- `runs/<run-id>.json`: immutable identity plus monotonic run state.
- `tasks/<controller>/...`: dedicated `TaskService` agenda store.
- `schedules/<controller>/...`: dedicated `ScheduleService` tick store.
- `notifications.jsonl`: append-only owner delivery records.

Expired controller, task, and schedule leases recover on startup. A running record first queries its deterministic subagent job. Terminal jobs finalize without respawn. Non-terminal jobs remain recoverable. Per-run execution locks serialize duplicate deliveries, daily cost reservation records run IDs idempotently, and terminal retries re-enqueue the same durable notification ID. Schedule completion happens only after run finalization, so duplicate ticks resolve to the same run identity.

## Notifications And Optional Integrations

Each terminal run and automatic pause creates one deterministic owner notification delivered through the durable plugin delivery queue. Activity, reflection, and evolution remain proposal references in task metadata or run output. Initiative does not import or mutate those capabilities.

## Non-Goals

- Kernel-owned autonomy or policy.
- Unbounded background execution.
- Direct provider/model invocation.
- Claiming user todo or cron records.
- Hidden activity, reflection, memory, continuity, or evolution mutation.
