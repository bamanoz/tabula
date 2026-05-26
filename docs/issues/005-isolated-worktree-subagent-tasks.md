# Isolated Worktree Subagent Tasks

Type: AFK

Priority: P1

Repos: `tabula-bundles`, optionally `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Add an opt-in worktree isolation mode for subagent tasks. When enabled, Tabula
creates or resumes a git worktree for the task, points workspace tools at that
worktree, and injects a child-agent notice explaining that inherited context
paths refer to the parent workspace and should be reread.

The feature should be generic and should not hardcode distro policy into the
kernel.

## Acceptance criteria

- [ ] A subagent spawn option can request an isolated worktree.
- [ ] The worktree is created under a deterministic Tabula-managed location or a
      documented workspace-local location.
- [ ] The child session's fs and exec tools use the isolated workspace root.
- [ ] The child prompt includes a bounded notice about parent path translation
      and stale inherited context.
- [ ] Task metadata records the worktree path and keep/cleanup state.
- [ ] Tests verify that a child edit does not modify the parent workspace.

## Blocked by

- `004-background-task-ledger-for-subagents.md`
