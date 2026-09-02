# Retention Survey: Tabula and Local Agent Implementations

Status: research snapshot only. This document records observed behavior and does
not propose a Tabula retention design.

Research date: 2026-08-09

## Scope and Method

This survey covers persisted sessions and transcripts, event streams,
projections, outboxes, tool output, audit records, logs, caches, authentication
state, archives, disk budgets, cleanup triggers, context compaction, and
recovery/resume consequences.

Evidence came only from locally cloned source repositories. No web sources were
used. Statements that cleanup was not found mean no matching automatic cleanup
was found in the inspected source surfaces; they are not claims about code
outside these repository snapshots.

| Repository | Local revision | Branch |
| --- | --- | --- |
| Claude Code | `4b9d30f7953273e567a18eb819f4eddd45fcc877` | `main` |
| Codex | `65f8bf68533332628b7fc213eade2a91d18d36ee` | `main` |
| OpenCode | `0317531906d3f3bb01cf33c16319870cfde9170c` | `dev` |
| OpenClaw | `c4f1661f70738c65cb05716d7a438e58090706de` | `main` |
| Pi | `2efa728d2ee90ef597626e96b1e28ef2b279f07c` | `main` |
| Tabula | `856834ee439017fd6f22b2b5081a5f85158de720` | `main` |

## Executive Findings

- Context compaction and durable deletion are separate mechanisms in every
  inspected system. A compacted prompt does not imply that the original
  transcript or event history was deleted.
- Claude Code and OpenClaw have the broadest automatic lifecycle cleanup.
- Codex bounds selected stores and compresses cold rollouts, while preserving
  those rollouts unless explicitly archived or deleted.
- OpenCode and Pi primarily use compaction to control future model context. Their
  original durable session history remains unless explicitly removed.
- Tabula currently bounds replay records, retained attempt outputs, tool-result
  spool files, and enabled file logs. Several durable stores remain unbounded.
- Tabula archive and delete are logical session state transitions, not physical
  purge operations.
- Tabula's global `state/kernel/sessions.db` combines artifacts with different
  retention needs: projections, replay events, outbox messages, command
  deduplication, and append-only session records.

## Comparative Summary

| System | Automatic durable retention | Context compaction | Archive/delete | Recovery and resume impact |
| --- | --- | --- | --- | --- |
| Claude Code | Default 30-day age cleanup across transcripts and related artifacts | Separate auto/manual compaction and tool-result clearing | Age cleanup physically removes old artifacts; persistence can be disabled | Removed transcripts cannot be resumed; compacted sessions remain resumable while durable data exists |
| Codex | Optional byte cap for message history; 10-day runtime-log retention; cold rollout compression after 7 days | Token-triggered context compaction | Archive moves rollout; delete removes it | Compressed rollouts remain transparently readable; explicit delete removes resume source |
| OpenCode | No general age or disk cap found for SQLite session rows; snapshot Git GC prunes unreachable data after 7 days | Auto-compaction enabled; durable tool-output pruning disabled by default | Archive preserves session; hard remove cascades session data and durable events | Resume starts from latest completed compaction while older durable rows remain |
| OpenClaw | Default 30 days, 500 entries, 10 GiB disk budget, cleanup target at 80% | Separate transcript/context controls | Stale/count overflow, orphans, optional archives, and disk pressure are cleaned automatically | Most complete bounded-storage behavior among inspected systems; evicted sessions lose resume history |
| Pi | Append-only session JSONL; no automatic session age/count/disk retention found | Auto-compaction enabled; default reserve 16,384 tokens and keep recent 20,000 tokens | Manual deletion through trash, falling back to permanent unlink | Compaction entries change reconstructed context; original JSONL remains until manual deletion |

## Claude Code

### Persisted artifacts

Session transcripts and subagent transcripts are JSONL-backed. Related local
state includes tool results, plans, file history, session environment state,
debug logs, image caches, paste storage, and agent worktrees.

`src/utils/sessionStorage.ts` contains session persistence behavior and a 50 MiB
raw-read guard. This guard protects loading; it is not a retention policy.

### Retention policy

`src/utils/cleanup.ts` defines `DEFAULT_CLEANUP_PERIOD_DAYS = 30` and computes an
age cutoff from `cleanupPeriodDays`.

The cleanup covers:

- transcripts and subagent session data;
- stored tool results;
- plans;
- file history;
- session environment state;
- message, MCP, and debug logs;
- image caches;
- pasted content;
- stale agent worktrees.

`cleanupPeriodDays=0` disables session persistence. Bundled configuration help
describes this as disabling persistence entirely.

`src/utils/backgroundHousekeeping.ts` starts delayed housekeeping after process
startup and schedules recurring daily work for long-running supported sessions.
Cleanup functions also use markers and locks to avoid excessive or concurrent
runs.

### Compaction

`src/services/compact/` implements context compaction and tool-result clearing.
This controls model context independently from age-based file deletion.

### Recovery consequence

A compacted session can still retain its durable transcript. Once age cleanup
physically removes the transcript and related artifacts, normal session resume
no longer has that source history.

### Primary evidence

- `src/utils/cleanup.ts`
- `src/utils/backgroundHousekeeping.ts`
- `src/utils/sessionStorage.ts`
- `src/services/compact/`
- `src/skills/bundled/updateConfig.ts`

## Codex

### Message history

`codex-rs/message-history/src/lib.rs` stores user message history in
`~/.codex/history.jsonl` when history persistence is enabled.

Configuration supports optional `max_bytes`. When the file exceeds its hard
cap, trimming targets 80% of the configured maximum. This is a byte-budgeted
history store, separate from full rollout transcripts.

### Rollouts

Rollout files are durable conversation/event history. In
`codex-rs/rollout/src/compression.rs`, rollouts at least 7 days old become
eligible for best-effort Zstandard compression.

Compression changes representation from plain JSONL to `.jsonl.zst`; it does
not delete history. Readers transparently resolve either representation, and an
append path can materialize a compressed rollout back to plain JSONL.

### Archive and delete

- `codex-rs/thread-store/src/local/archive_thread.rs` implements archive movement.
- `codex-rs/thread-store/src/local/delete_thread.rs` implements hard deletion.

These explicit lifecycle actions are distinct from cold compression.

### Runtime logs

`codex-rs/state/src/runtime/logs.rs` defines 10-day SQLite log retention. Startup
maintenance deletes older rows and runs `PRAGMA wal_checkpoint(PASSIVE)`.
Additional reader-visible byte budgets keep approximately 10 MiB of log content
per partition, with row and byte capping behavior in the runtime log store.

### Compaction

`codex-rs/core/src/session/turn.rs` triggers model-context compaction based on
token pressure. Compaction does not replace rollout retention.

### Recovery consequence

Cold compression preserves resume capability because rollout readers support
both representations. Archive changes discovery/location. Hard delete removes
the local rollout needed for normal resume.

### Primary evidence

- `codex-rs/message-history/src/lib.rs`
- `codex-rs/rollout/src/compression.rs`
- `codex-rs/thread-store/src/local/archive_thread.rs`
- `codex-rs/thread-store/src/local/delete_thread.rs`
- `codex-rs/state/src/runtime/logs.rs`
- `codex-rs/core/src/session/turn.rs`

## OpenCode

### Durable session model

OpenCode stores sessions, messages, parts, and durable events in SQLite.
Foreign-key cascades connect session deletion to subordinate rows.

Relevant schema and store surfaces include:

- `packages/core/src/session/sql.ts`;
- `packages/core/src/session/event.sql.ts`;
- `packages/opencode/src/session/session.ts`.

No general age, count, or disk-budget retention was found for these SQLite
session rows.

### Archive and delete

`packages/opencode/src/session/session.ts` distinguishes archive from hard
remove. Archive retains the session. Hard remove deletes the session graph and
its durable event data.

### Compaction and resume

`packages/core/src/session/history.ts` reconstructs context from the latest
completed compaction. Older durable messages and parts remain present.

`packages/opencode/src/session/compaction.ts` implements context compaction and
optional pruning of old tool outputs. Auto-compaction is enabled unless disabled
by configuration or environment flags. Durable tool-output pruning is disabled
by default unless explicitly enabled.

Current compaction constants include a 20,000-token prune minimum, 40,000-token
protected region, and bounded preserved-recent budget. These are context
construction controls, not general session deletion.

### Snapshot cleanup

`packages/opencode/src/snapshot/index.ts` maintains a Git-backed snapshot store.
It runs `git gc --prune=7.days`, first after a one-minute delay and then hourly.
This bounds unreachable snapshot objects independently from session SQLite rows.

### Logs

`packages/core/src/observability/logging.ts` writes an append-style
`opencode.log`. No matching automatic cleanup was found in the inspected logging
surface.

### Recovery consequence

Compaction allows context reconstruction without replaying every old message,
but does not physically remove those rows. Hard session removal is the operation
that eliminates normal local resume data.

### Primary evidence

- `packages/core/src/session/sql.ts`
- `packages/core/src/session/event.sql.ts`
- `packages/core/src/session/history.ts`
- `packages/opencode/src/session/session.ts`
- `packages/opencode/src/session/compaction.ts`
- `packages/opencode/src/snapshot/index.ts`
- `packages/core/src/observability/logging.ts`

## OpenClaw

### Session maintenance defaults

`src/config/types.base.ts` exposes `session.maintenance` configuration.
`src/config/sessions/store-maintenance.ts` resolves these defaults:

- mode: `enforce`;
- stale session age: 30 days;
- maximum session entries: 500;
- model-run retention: 24 hours;
- per-agent session disk budget: 10 GiB;
- cleanup target: 80% of the disk budget.

Invalid explicit disk-budget configuration disables destructive budget cleanup
rather than silently falling back to a destructive default. Invalid archive
retention also stays on the keep side.

### Cleanup actions

Maintenance code handles:

- stale session-entry pruning;
- count-based capping;
- short-lived model-run pruning;
- orphan transcript and runtime artifact cleanup;
- optional age retention for reset/deleted archives;
- oldest-first disk-budget enforcement;
- extraction/reclamation of historical SQLite-backed sessions.

Maintenance runs around session-store persistence rather than relying only on a
separate operator command.

### Archive behavior

Archived transcript files such as reset and deleted archives are retained by
default until disk pressure requires eviction. Setting an archive-retention
duration opts into wall-clock deletion.

### Additional bounded stores

The inspected source also contains:

- trajectory artifact cleanup and size guards under `src/trajectory/`;
- file-backed transcript limits, including a 2,000-utterance cap;
- serialized session-store cache limits of 64 entries and 64 MiB;
- raw and visible SQLite transcript read limits;
- default log rotation at 100 MiB per file with rotated-file retention and age
  pruning in `src/logging/logger.ts`.

### Recovery consequence

Age, count, and disk-budget maintenance can physically remove old recovery
history. Warn mode reports the same pressure without enforcement. Enforce mode
makes durable storage bounded even if users never manually delete sessions.

### Primary evidence

- `src/config/types.base.ts`
- `src/config/sessions/store-maintenance.ts`
- `src/config/sessions/store-maintenance-files.ts`
- `src/config/sessions/disk-budget.ts`
- `src/config/sessions/cleanup-service.ts`
- `src/config/sessions/store-cache.ts`
- `src/trajectory/cleanup.ts`
- `src/transcripts/store.ts`
- `src/transcripts/config.ts`
- `src/logging/logger.ts`

## Pi

### Session persistence

`packages/coding-agent/src/core/session-manager.ts` implements append-only
per-session JSONL. The default root comes from `getSessionsDir()` and resolves to
`~/.pi/agent/sessions`; sessions are grouped below an encoded working-directory
path.

The original JSONL remains the durable record after compaction. No automatic
session age, count, or disk-budget cleanup was found in the inspected coding
agent source.

### Compaction

`packages/coding-agent/src/core/settings-manager.ts` documents defaults:

- compaction enabled: true;
- reserve tokens: 16,384;
- keep recent tokens: 20,000.

`packages/coding-agent/src/core/compaction/compaction.ts` produces compaction
entries containing a summary and `firstKeptEntryId`. Context reconstruction uses
these entries without rewriting or deleting the earlier JSONL entries.

### Manual deletion

`packages/coding-agent/src/modes/interactive/components/session-selector.ts`
tries the external `trash` command first and falls back to filesystem `unlink`.
The active session is protected by selector behavior.

### Logs

`packages/coding-agent/src/config.ts` defines a debug log path under the agent
directory. Explicit interactive export overwrites that log. No periodic debug-log
retention policy was found.

### Recovery consequence

Compaction changes which history is sent to the model but keeps the full session
file available for tree navigation and reconstruction. Manual deletion removes
that recovery source.

### Primary evidence

- `packages/coding-agent/src/core/session-manager.ts`
- `packages/coding-agent/src/core/compaction/compaction.ts`
- `packages/coding-agent/src/core/agent-session.ts`
- `packages/coding-agent/src/core/settings-manager.ts`
- `packages/coding-agent/src/modes/interactive/components/session-selector.ts`
- `packages/coding-agent/src/config.ts`

## Tabula Current State

### Storage layout

Kernel-owned durable session state lives at:

```text
$TABULA_HOME/state/kernel/sessions.db
```

`internal/cli/tabula/serve.go` opens both the agent session repository and the
session-record store against this same SQLite file.

This file therefore combines multiple retention classes:

- current aggregate projections;
- replay events;
- outbox messages;
- command idempotency records;
- append-only session records used for audit and lifecycle data.

Both SQLite store implementations enable WAL, full synchronous writes, foreign
keys, and a five-second busy timeout.

### Aggregate projection

The `sessions` table stores a serialized `agent.State`. That state contains:

- every retained input in `Inputs`;
- every retained turn and attempt in `Turns`;
- pending queue state;
- current driver state;
- every applied command ID and digest in `AppliedCommands`.

Attempt outputs are bounded, but inputs, turns, attempts, and applied-command
metadata have no general age, count, or byte retention.

Consequently, the current projection can grow throughout a long session even
when old replay rows are deleted.

Primary evidence:

- `internal/agent/types.go`;
- `internal/agent/apply.go`;
- `internal/agent/retention.go`;
- `internal/agent/sqlite_repository.go`.

### Output retention

`internal/agent/retention.go` defines:

| Scope | Count limit | Payload-byte limit |
| --- | ---: | ---: |
| One attempt | 256 outputs | 1 MiB |
| One session | 1,024 outputs | 4 MiB |

When limits are crossed, oldest outputs are removed from the projection.
Retention affects output payloads, not the surrounding turn and attempt
structures.

### Replay and outbox retention

`MaxRetainedReplayRecordsPerSession` is 4,096.

The repository uses one cursor across events and outbox messages. After each
commit it computes a retained cursor floor and deletes `events` and `outbox`
rows at or below that floor. The effective bound is therefore a window of 4,096
combined cursor positions, not 4,096 events plus 4,096 outbox rows.

A reader requesting a cursor below the floor receives `CursorExpiredError` and
must recover from the current projection/snapshot boundary.

### Command deduplication

The `commands` table stores command ID, digest, and result metadata for every
committed command. No command-row pruning was found.

The serialized projection also stores every command ID and digest in
`AppliedCommands`. This creates two durable, independently growing command
idempotency surfaces.

### Session records

`internal/sessionrecord/store.go` and `internal/sessionrecord/sqlite.go` expose
only `Append` and `Read`.

Current limits:

- maximum payload: 1 MiB per record;
- read page: 1 through 256 records;
- kind length: 128 characters;
- producer length: 256 characters.

No delete, prune, age limit, count limit, byte budget, or archive API exists for
`session_records`. Records therefore grow append-only in the shared database.

### Archive and delete semantics

For the aggregate repository:

- archive sets `State.Archived = true`;
- unarchive clears it;
- delete changes session status to `closed`;
- later commits against a closed session are rejected.

Neither archive nor delete removes the projection, command rows, session
records, or all replay history. The delete event itself can eventually age out
of the replay window while the closed projection remains durable.

The older kernel live-session snapshot path has separate behavior:

```text
$TABULA_HOME/tenants/<tenant>/state/sessions/<session>.json
```

These JSON snapshots persist live lifecycle state and are rehydrated after
restart. Snapshots marked deleted are skipped during hydration. A snapshot is
removed when the final client leaves the session. This store is not the durable
agent transcript/event repository.

Primary evidence:

- `internal/agent/apply.go`;
- `internal/agent/decide.go`;
- `internal/agent/sqlite_repository.go`;
- `internal/kernel/session_lifecycle.go`;
- `internal/kernel/session_store.go`;
- `internal/kernel/join_flow.go`;
- `internal/kernel/kernel.go`.

### SQLite space reclamation

No application-level use of these operations was found in Tabula source:

- `VACUUM`;
- incremental vacuum;
- `PRAGMA wal_checkpoint`;
- `PRAGMA optimize`.

Replay-row deletion is therefore logical retention. It does not guarantee that
the main SQLite file immediately shrinks, and no explicit application policy
was found for reclaiming free pages or checkpointing the shared WAL.

### Tool-result spool retention

Large runtime tool results are temporarily spooled below:

```text
$TABULA_HOME/run/tool-results
```

`internal/kernel/tool_result.go` defines:

- inline result threshold: 12,000 bytes;
- spool preview: 4,096 bytes;
- stream idle timeout: 30 seconds;
- stale age cutoff: 24 hours.

Normal call cleanup closes and removes its spool file. Kernel startup removes
all matching JSON spool files. Creation of a new spool also removes files older
than 24 hours.

The temporary file is therefore bounded by lifecycle cleanup, while any output
copied into the aggregate projection is governed by attempt/session output
limits.

### Logs

`internal/logging/logging.go` uses Lumberjack rotation.

File logging is silent by default. When enabled, defaults are:

- 10 MiB maximum active file size before rotation;
- 7-day maximum age for old files;
- 3 backups;
- compression enabled by kernel service configuration.

`TABULA_*` environment controls exposed through `ApplyEnv` cover log levels,
formats, output, and file path. Size, age, backup count, and compression are not
exposed by that environment overlay.

Primary evidence:

- `internal/logging/logging.go`;
- `internal/cli/tabula/service_config.go`.

### Runtime authentication state

Runtime token metadata is stored in:

```text
$TABULA_HOME/state/runtime-tokens.json
```

Issuing a token replaces the record for the same runtime ID, preventing repeated
issuance for one ID from growing the map. Expired and revoked records remain
stored. No age cleanup or total unique-runtime cap was found.

Primary evidence: `internal/runtime/auth/token.go`.

### Tenant data and caches

Tenant deletion removes the complete tenant subtree, including tenant-local
state, cache, logs, and live-session JSON snapshots.

The global `state/kernel/sessions.db` is outside that subtree. No integration was
found that purges rows for a deleted tenant from the global aggregate or
session-record tables.

No generic kernel cleanup was found for active tenant cache and log directories.
Those directories may contain component-owned data whose retention belongs to
the responsible plugin, runtime, gateway, or distro rather than to generic
kernel policy.

Primary evidence:

- `internal/tenant/store.go`;
- `internal/layout/layout.go`;
- `internal/cli/tabula/serve.go`.

### Installer and release artifacts

Distro installation uses stable replacement rather than retained generations.
Private transaction state lives under:

```text
$TABULA_HOME/run/install/<distro>/
```

It contains `staging`, `previous`, and `transaction.json`. Successful commit
removes staging, previous, and transaction state. Recovery or rollback uses the
single previous tree only while a replacement is in progress.

Host-service releases are a separate installer/service lifecycle. They use
immutable content-addressed releases with `current` and `previous` references.
No periodic pruning of inactive immutable releases was found; ordinary service
removal deletes executable releases, while purge additionally removes
service-owned state. This is not kernel session retention.

Primary evidence:

- `tools/tabula-distro/src/tabula_distro/install.py`;
- `tools/tabula-distro/src/tabula_distro/host_service.py`.

## Tabula Retention Classification

### Currently bounded or lifecycle-cleaned

- Replay event/outbox cursor window: 4,096 positions per session.
- Retained output payloads: bounded per attempt and per session.
- Tool-result spool files: per-call removal, startup cleanup, 24-hour stale
  cleanup, and 30-second idle timeout.
- File logs when enabled: rotation, age, backup count, and compression.
- Distro installer transaction scratch: removed after successful replacement.
- Tenant-local state as a whole: removed only when the tenant itself is deleted.

### Currently unbounded in inspected source

- Number and total size of aggregate session projections.
- Inputs, turns, attempts, and applied-command metadata inside a long-lived
  projection.
- Command deduplication rows.
- `AppliedCommands` inside serialized state.
- Append-only session records and their total bytes.
- Number of archived and logically deleted aggregate sessions.
- Runtime token metadata across unique runtime IDs.
- Tenant/component cache and log artifacts without owner-specific cleanup.
- Inactive immutable host-service releases until explicit service removal
  (installer/service-owned, not kernel-owned).
- Physical size of `sessions.db` after logical row deletion.

## Recovery and Resume Implications for Tabula

- Replay consumers can recover only within the retained 4,096-position window.
  Older cursors must use the current projection boundary.
- Output retention can remove old output payloads while preserving the turn and
  attempt topology.
- Archive preserves durable state and supports later unarchive.
- Delete prevents future aggregate commits but preserves the closed projection
  and associated durable records.
- Session-record audit history currently survives archive, delete, and tenant
  subtree removal.
- SQLite logical deletion can reduce live row count without reducing on-disk file
  size.
- Legacy live-session JSON snapshots are restart aids, not authoritative durable
  transcript history, and disappear when the final client leaves.

## Boundary Observation

The Tabula kernel is a persistence boundary, router, message bus, and lifecycle
coordinator. Generic correctness invariants for its own durable stores belong in
kernel/core code. Product choices such as desired retention duration, user-facing
archive policy, cache semantics, and component-specific cleanup remain distro,
plugin, gateway, client, or operator policy unless a future architecture decision
establishes a generic persistence contract.

This observation records repository boundaries only; it is not an implementation
proposal.
