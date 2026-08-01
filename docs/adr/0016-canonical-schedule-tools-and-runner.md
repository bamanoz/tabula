# ADR 0016: Canonical Schedule Tools And Runner

- Status: Accepted
- Date: 2026-07-30
- Supersedes: ADR 0015 compatibility decision for existing cron tools
- Superseded by: none

## Context

ADR 0015 introduced `ScheduleService` while preserving the previous `cron_*`
tools and OS crontab delivery path. That created two schedule models and two
runners:

- canonical SDK fields such as `trigger`, `payload`, `timezone`, and `retry`;
- compatibility fields such as `cron`, `task`, `session`, and `once`;
- a leased warm-plugin runner with retry/recovery;
- an OS crontab `fire` path without equivalent leases, retries, one-shot `at`
  triggers, or stable occurrence lifecycle.

Keeping both paths would make schedule semantics depend on host capabilities and
force every future scheduling feature through compatibility projections.

## Decision

The `cron` component keeps its component ID and ownership, but exposes only the
canonical scheduling surface:

- `schedule_put`
- `schedule_list`
- `schedule_pause`
- `schedule_resume`
- `schedule_remove`

Persisted state uses `schedules.json` with a `schedules` collection. Records use only `trigger`, `payload`, `timezone`, `retry`, pause, revision, occurrence, and delivery-state fields. Agent-facing scheduled messages
use `payload.message` and `payload.session`; SDK callers may use arbitrary
payloads.

The warm plugin worker is the only scheduled-delivery runner. OS crontab sync,
CLI fire commands, `cron_*` tools, and compatibility record fields are removed.
One-shot delivery is represented by an `at` trigger, not a recurring trigger
with an `once` flag.

This is a clean break. Existing legacy cron state is not translated by the new
service.

## Consequences

- Tool, SDK, persistence, retry, recovery, timezone, and deduplication semantics
  share one model.
- Every delivery uses a persisted occurrence lease and stable `delivery_id`.
- Scheduling works consistently on hosts with or without OS crontab.
- Existing callers of `cron_*`, CLI `fire`/`sync`, or legacy `jobs.json` must
  move to the canonical schedule contract.
- Kernel behavior remains unchanged.
