# Isolated Worktree Subagent Tasks

Type: AFK

Priority: P1

Status: Completed

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

Reference implementation notes for current `claude-code` behavior live in:

- `docs/issues/claude-code-features/005-implementation-notes.md`

Important parity note:

- current `claude-code` semantics are not git-only
- backend selection is: `WorktreeCreate hook if configured`, else `git worktree`,
  else explicit error
- do not intentionally ship a narrower behavior than current `claude-code`

## Acceptance criteria

- [x] A subagent spawn option can request an isolated worktree.
- [x] The worktree is created under a deterministic Tabula-managed location or a
      documented workspace-local location.
- [x] The child session's fs and exec tools use the isolated workspace root.
- [x] The child prompt includes a bounded notice about parent path translation
      and stale inherited context.
- [x] Task metadata records the worktree path and keep/cleanup state.
- [x] Tests verify that a child edit does not modify the parent workspace.

## Blocked by

- `004-background-task-ledger-for-subagents.md`

## Implementation Notes

Implemented in current Tabula behavior:

- `subagent_spawn` now treats `worktree` as isolated workspace request, not
  git-only request.
- Backend selection matches current `claude-code` semantics:
  - external `worktree_create_command` if configured
  - otherwise git worktree
  - otherwise explicit error
- Git-backed worktrees now persist cleanup metadata and creation baseline state.
- Terminal lifecycle finalizes worktrees on completion/failure/kill.
- Git cleanup is change-aware:
  - unchanged + `keep=false` => remove worktree and branch
  - changed + `keep=false` => keep worktree with explicit metadata
- External-provider workspaces are kept by default because generic change
  detection is not available for arbitrary backends.
- Installed ACP-based testbed coverage now verifies:
  - isolated child cwd / `TABULA_PROJECT_ROOT`
  - parent file isolation
  - `keep=true`
  - `keep=false` remove-clean-tree
  - `keep=false` keep-changed-tree

## Verification

- `python3 -m unittest subagents.tests.test_subagents_plugin`
- `PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite subagents --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib`
