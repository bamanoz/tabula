# Add structured workspace VCS component

**Type:** AFK  
**Status:** completed

## What to build

Add optional generic `vcs` component to the existing `workspace` bundle. Provide structured, roots-aware Git operations suitable for normal agents and controlled-change workflows without embedding evolution policy.

## Required discovery and design

- Inspect Ouroboros `supervisor/git_ops.py`, `tools/git.py`, `tools/git_rollback.py`, rescue refs, snapshots, branch/stash handling, and rollback behavior.
- Compare with Tabula filesystem/exec roots, security hooks, subagent worktrees, and dirty-worktree rules.
- Design a generic VCS contract with explicit repository identity, structured results, rescue behavior, and concurrency rules.

## Acceptance criteria

- [x] Tools cover status, diff, log, isolated worktree create/remove, rescue snapshot, commit, restore, revert, and rollback.
- [x] Every mutating operation is restricted to configured repositories and passes normal permission/approval hooks.
- [x] Destructive operations preview impact and create a recoverable rescue reference where applicable.
- [x] Structured results expose repository, base/head, changed paths, and resulting refs without parsing shell prose.
- [x] Concurrent worktree/ref operations are serialized safely and race-tested.
- [x] Existing subagent worktree behavior reuses shared implementation where practical.
- [x] Installed testbed executes representative read, worktree, commit, and recovery operations.

- [x] Canonical testbed and generated testbed template are updated; the suite executes installed VCS tools through normal permission hooks and verifies recovery.

## Blocked by

None - can start immediately.
