# Durable Task and Scheduling Primitives

## Discovery

### Ouroboros patterns worth preserving

- `supervisor/queue.py:192-235`: queue mutation is serialized and each task gets
  stable identity, attempt, order, and queued timestamp.
- `supervisor/queue.py:272-305`: schedules use idempotent upsert/removal and
  atomic persisted replacement.
- `supervisor/queue.py:424-568`: due schedules persist task state before enqueue,
  prevent concurrent duplicate work, and advance next-run state after dispatch.
- `supervisor/queue.py:598-699`: restart snapshot captures enough ownership and
  task state for diagnosis/recovery.
- `supervisor/queue.py:713-870`: recovery rejects terminal resurrection and
  fails closed when persisted ownership state is malformed.
- `supervisor/task_reaper.py:125-181`: retry retains original correlation and is
  refused when ownership/admission cannot be proven safe.
- `supervisor/events.py:2693-2710` and `supervisor/state.py:107`: durable owner
  stop is useful for autonomous policy, but belongs to initiative/evolution,
  not generic scheduling.

### Tabula contracts to keep authoritative

- `productivity/todo/scripts/run.py`: `todo_read`/`todo_write` are
  session-scoped replacement-snapshot tools with stable item IDs.
- `productivity/cron/run.py`: canonical `schedule_*` tools and the warm runner use
  the same `trigger`/`payload` schedule model and `<schedule>` deliveries.
- `tabula_plugin_sdk.paths.plugin_state_dir`: state is tenant-local when
  `TABULA_TENANT_DIR` is present.
- `tabula_plugin_sdk.filelock`: cross-platform process locking.
- `tabula_plugin_sdk.delivery`: durable at-least-once session delivery with
  caller-provided delivery IDs.
- Warm plugin `on_start`/`on_shutdown`: scheduler lifecycle remains plugin-owned.

## Public API

`tabula_tasks_sdk.TaskService`:

- `list(scope)` / `get(scope, task_id)`
- `replace(scope, items, expected_revision=None)`
- `add(scope, task, expected_revision=None)`
- `update(scope, task_id, changes, expected_revision=None,
  expected_task_revision=None)`
- `claim(scope, owner, task_id=None, lease_seconds=60)`
- `release(scope, task_id, lease_token)`
- `complete(scope, task_id, lease_token, result=None)`
- `fail(scope, task_id, lease_token, error, retry_at=None)`
- `recover(scope)`

`tabula_scheduling_sdk.ScheduleService`:

- `put(schedule)` / `get(id)` / `list()` / `remove(id)`
- `pause(id)` / `resume(id)`
- `claim_due(owner, lease_seconds=60, limit=1)`
- `complete(id, lease_token)` / `fail(id, lease_token, error)`
- `recover()`

## Storage and recovery

- JSON stores use atomic replace under a sibling `.lock` file. Schedule state is canonical `schedules.json` with a `schedules` collection; no legacy job projection is persisted.
- Store and record revisions increase on every successful mutation.
- Expired task claims return to `pending`; expired schedule claims retain the
  same occurrence and `delivery_id` for retry.
- Schedule delivery is at-least-once. Stable IDs make duplicate handling
  explicit and testable.

## Ownership boundaries

- `todo` owns `tabula_tasks_sdk`; `cron` owns `tabula_scheduling_sdk`, persisted schedules, the single warm scheduler loop, and canonical `schedule_*` tools.
- `tabula_plugin_sdk` owns generic paths, locks, and durable kernel delivery.
- Controllers own payload meaning, budgets, campaign IDs, stop policy, and
  decisions to create or claim work.
- Kernel remains unaware of tasks, schedules, leases, retries, and controllers.
