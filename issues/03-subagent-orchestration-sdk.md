# Expose reusable subagent orchestration SDK

**Type:** AFK  
**Status:** completed

## What to build

Refactor existing subagent orchestration behind a reusable Python SDK so plugins can spawn, inspect, wait for, steer, and cancel typed subagents without invoking agent-facing tools or duplicating process/registry logic.

## Required discovery and design

- Inspect Ouroboros supervisor, worker, task-tree, swarm, cancellation, result, and recovery paths.
- Compare them with `tabula-bundles/subagents`, session SDK, driver selection, worktree behavior, and ledger events.
- Document which proven lifecycle/failure patterns should carry over and which existing Tabula contracts remain authoritative.
- Design SDK ownership under the existing `subagents` bundle; add no Ouroboros policy or model-specific calls.

## Acceptance criteria

- [x] Existing `subagent_*` tools are thin wrappers over one reusable SDK/service implementation.
- [x] SDK supports spawn, list/get, wait, send/steer, kill, and recovery of persisted jobs.
- [x] Existing provider, ACP, worktree, cancellation, artifact, notification, and ledger semantics remain intact.
- [x] Plugin callers can correlate jobs with their own task/campaign IDs.
- [x] Concurrent calls and process cleanup are race-tested.
- [x] An installed fixture plugin uses SDK to run a subagent and consume its result end to end.
- [x] No direct local-model invocation API is introduced.

- [x] Canonical testbed and generated testbed template are updated; the suite executes the installed SDK consumer and verifies result delivery and cleanup.

## Blocked by

None - can start immediately.
