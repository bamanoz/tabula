# ADR 0014: Reusable Subagent Orchestration SDK

- Status: Accepted
- Date: 2026-07-29
- Supersedes: none
- Superseded by: none

## Context

The `subagents` plugin already owned process launch, persisted job records,
provider and ACP runner selection, worktree lifecycle, result artifacts,
notifications, cancellation, and session ledger events. Other plugins needed the
same lifecycle for campaigns and durable tasks, but their only integration path
was to invoke agent-facing `subagent_*` tools or duplicate plugin internals.

Ouroboros demonstrates useful failure patterns: durable parent/root lineage,
serialized worktree mutation, startup orphan pruning, kill-before-terminalize,
and a post-kill terminal-result recheck. Its model-lane policy, local-model
selection, swarm roles, and product-specific task tree are not Tabula contracts.

## Decision

The `subagents` bundle exports public Python package `tabula_subagents_sdk`.
`SubagentService` is the single reusable orchestration implementation for:

- spawn and parallel batch spawn;
- get, list, wait, and persisted-job recovery;
- send and steer;
- kill and process/worktree cleanup.

Agent-facing `subagent_*` tools are JSON protocol adapters over this service.
SDK methods return Python dictionaries and accept optional caller-owned
`correlation` metadata, which is persisted in job snapshots and ledger events.
Explicit job and batch IDs are serialized with tenant-scoped cross-process file
locks. Existing provider, ACP, worktree, artifact, notification, lease, and
ledger behavior remains owned by the bundle.

The SDK does not expose direct provider or local-model invocation. Callers choose
typed subagent presets and the existing runner/provider contracts remain
authoritative. Kernel receives no subagent policy or SDK-specific behavior.

## Consequences

- Plugins can orchestrate subagents without pretending to be an agent tool call.
- Persisted jobs can be reconciled after plugin or kernel restart.
- Campaign/task correlation remains caller-owned and does not create a kernel
  task model.
- Process and worktree semantics have one implementation shared by tools and
  plugin callers.
- Installed testbeds must execute an SDK consumer plugin, not only inspect the
  exported package or tool catalog.
