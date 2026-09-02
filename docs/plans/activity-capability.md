# Activity Capability

Status: Implemented
Date: 2026-07-31
Issue: `issues/09-activity-capability.md`
Architecture: `docs/adr/0012-modular-agent-capabilities.md`
Artifact API: `docs/adr/0017-tenant-local-artifact-owner.md`
Bundle ADR: `../tabula-bundles/docs/adr/0007-curated-cross-session-activity.md`

## Source Study

Ouroboros separates product activity projections from raw execution logs. Its Activity dashboard presents scheduled, queued, running, and background work, while task event APIs project progress, messages, tool records, supervisor records, artifacts, and terminal results into correlated task timelines. Durable task results remain authoritative after restart and gateway projections synthesize missing terminal presentation without changing raw logs.

Tabula already owns canonical session aggregates and committed events in the kernel SQLite repository, auxiliary session records for bounded technical evidence, raw hook logs in `observability:hook-logger`, tenant-local artifacts in `async:tool-result-store`, and project/workspace semantics in workspace components. The kernel exposes generic `after_turn` observations and turn correlation metadata; no new kernel event type is needed.

## Decision

Add independent `activity` bundle with one warm `activity` plugin and public Python package `tabula_activity_sdk`.

Activity owns tenant-local state under `state/plugins/activity/`:

- `events.jsonl`: append-only curated activity events.
- `projections.json`: rebuildable cache containing current work and recent outcomes.

Event kinds are `work`, `decision`, `artifact`, and `outcome`. Every event carries tenant, project, session, actor, source, correlation metadata, optional work ID, optional artifact references, summary, structured details, and timestamp. Work events use explicit lifecycle status. Event IDs provide idempotent retry and conflict detection.

Public SDK exposes explicit `emit_work`, `emit_decision`, `emit_artifact`, and `emit_outcome` methods bound to active plugin tenant/source identity. Activity depends transitively on `async:tool-result-store`; artifact bytes remain artifact-owner state and activity stores only `artifact://` references.

Tools expose event emission, artifact creation plus event emission, timeline query, current-work projection, and recent-outcome projection.

## Hook Aggregation

Optional `after_turn` capture is disabled by default. When enabled, activity emits one idempotent outcome event per terminal turn correlation. It never mirrors individual tool calls or replaces raw hook logging.

## Recovery And Retention

Writes use a process-safe file lock, fsynced append-only journal, atomic projection replacement, and strict validation. Reads rebuild projections from the journal, so worker restart or interrupted cache writes cannot lose current-work or recent-outcome state. Corrupt journal state fails closed.

Events are retained indefinitely by default. Query limits and configured projection limits bound reads; no hidden truncation or automatic deletion occurs. Future archival may remove only explicitly selected complete journal generations through a separate operator contract.

## Ownership Boundaries

- Sessions owns transcript/history and technical ledger semantics.
- Hook logger owns raw hook audit.
- Artifact service owns artifact bytes and metadata.
- Workspace/project components own repository and project semantics.
- Activity owns curated narrative events and projections only.

## Non-Goals

- Replacing session replay, ledger, or hook logs.
- Recording every tool call.
- Generalized memory, reflection, initiative, or evolution policy.
- Gateway-specific presentation or kernel event changes.
