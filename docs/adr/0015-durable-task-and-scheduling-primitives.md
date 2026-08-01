# ADR 0015: Durable Task and Scheduling Primitives

- Status: Accepted
- Date: 2026-07-30
- Supersedes: none
- Superseded by: ADR 0016

## Context

Independent controllers need durable work coordination without importing agent,
initiative, reflection, or evolution policy. Existing `todo` state is a
session-scoped replacement snapshot without cross-process mutation protection.
Existing `cron` state schedules user-facing messages, but does not expose a
reusable claim/lease API for plugin callers.

Ouroboros demonstrates useful failure patterns in
`supervisor/queue.py:192-235`, `supervisor/queue.py:484-568`,
`supervisor/queue.py:598-699`, `supervisor/queue.py:713-870`, and
`supervisor/task_reaper.py:125-181`: serialize shared mutations, persist before
dispatch, reject resurrection of terminal work, use stable retry identity, and
fail closed when ownership is uncertain. Its budgets, acceptance fences,
worker pool, model lanes, task trees, and evolution owner-stop state are product
policy and are not Tabula primitive contracts.

Tabula already owns tenant-local plugin state, cross-platform file locks, atomic
replacement writes, durable delivery queues, warm plugin startup, and session
message routing in `tabula_plugin_sdk`. Those contracts remain authoritative.

## Decision

The `productivity` bundle exports two public Python packages from existing
component owners:

- `todo` exports `tabula_tasks_sdk`. `TaskService` owns scoped durable task
  records and atomic add/update/claim/release/complete/fail/recover operations.
- `cron` exports `tabula_scheduling_sdk`. `ScheduleService` owns one-shot and
  recurring schedules with idempotent put, pause/resume/remove, due-work claims,
  retry, completion, and expired-lease recovery.

Both services persist under tenant-local plugin state and serialize each store
with a cross-process file lock. Stores and records carry monotonically
increasing revisions. Claims carry opaque lease tokens and expiry timestamps;
lease-bound transitions reject stale or mismatched tokens.

Task scopes are explicit `{kind, id}` values. Built-in kinds are `session`,
`tenant`, and `project`; any other non-empty kind is caller-defined. Scope is a
storage and coordination boundary only. It does not grant authority or imply
kernel routing behavior.

Schedule delivery is at-least-once. Each due occurrence receives a stable
`delivery_id`; retries and restart recovery retain that ID so downstream durable
delivery or consumers can deduplicate. The scheduler persists a claim before
calling delivery code and records completion afterward. A crash between those
steps may repeat delivery, but cannot silently lose the occurrence.

Existing `todo_read`/`todo_write` and `cron_*` tools remain agent-facing adapters
with their documented schemas. No task or schedule semantics are added to the
kernel.

## Consequences

- Plugins can coordinate durable work without invoking agent-facing tools.
- Concurrent controllers detect stale revisions instead of losing updates.
- Restart recovery returns expired claims to claimable state.
- Scheduling remains generic payload delivery; initiative policy belongs to a
  future controller bundle.
- Installed testbeds must execute an SDK consumer plugin and verify recovery and
  duplicate-delivery identity, not only import the package.
