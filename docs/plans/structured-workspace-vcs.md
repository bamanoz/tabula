# Structured Workspace VCS Component

## Discovery

### Ouroboros behavior worth preserving

- `ouroboros/supervisor/git_ops.py:500-558` captures dirty status, binary diff, a `git stash create` snapshot pinned under `refs/rescue/*`, untracked files, unpushed commits, and metadata before destructive recovery.
- `ouroboros/ouroboros/tools/git.py:941-962` serializes Git mutation with an exclusive lock; commit, restore, and revert run inside that boundary.
- `ouroboros/ouroboros/tools/git.py:1892-2044` previews restore/revert impact, requires explicit confirmation, requires a clean worktree for revert, and aborts a failed revert.
- `ouroboros/ouroboros/tools/git_rollback.py:19-69` separates rollback preview from confirmed execution.
- `ouroboros/supervisor/update_merge.py:40-152` uses temporary detached worktrees for isolated operations and always removes/prunes them.
- Ouroboros evolution transactions, protected product paths, review gates, automatic push/tag behavior, managed remotes, supervisor restart, and runtime modes are product policy and remain excluded.

### Current Tabula behavior

- `workspace:fs` resolves project/configured roots, rejects traversal and symlink escapes, and exposes workspace context.
- `workspace:exec` provides general shell execution but does not enforce repository identity and is unsuitable as the VCS safety boundary.
- `tabula_plugin_sdk.tool_policy` normalizes known tool families for permission and approval hooks; `vcs_*` is currently absent.
- `tabula_subagents_sdk` privately implements Git worktree creation/removal without shared per-repository serialization.
- Kernel protocol and persistence do not need VCS semantics.

## Design

### Ownership

The optional `workspace:vcs` plugin owns structured Git tools. The workspace bundle exports `tabula_workspace_vcs` as the shared Python implementation used by the plugin and subagent worktree orchestration.

The kernel remains unaware of repositories, refs, worktrees, previews, and rescue state.

### Repository identity and confinement

Configuration accepts `repositories` and `extra_repositories`. `TABULA_PROJECT_ROOT`, when present, becomes the first repository. Relative tool input selects the first configured repository; explicit input must identify one configured repository or one Git worktree attached to its common Git directory.

Every result includes a repository identity with configured root, worktree root, common Git directory, branch, and HEAD. Git commands disable interactive credential prompts.

### Tool contract

Tools:

- `vcs_status`
- `vcs_diff`
- `vcs_log`
- `vcs_worktree_create`
- `vcs_worktree_remove`
- `vcs_rescue`
- `vcs_commit`
- `vcs_restore`
- `vcs_revert`
- `vcs_rollback`

Read tools return structured entries and bounded patch text. Mutating results include base/head, changed paths, created/removed refs and worktrees, rescue metadata, and warnings.

`restore`, `revert`, and `rollback` are two-phase. Without `confirm=true`, they return a structured preview and perform no mutation. Confirmed destructive operations create a rescue ref first when repository state can be captured. Restore may discard selected tracked and untracked paths. Revert requires a clean worktree. Rollback resets the current branch/worktree to a resolved target and never pushes.

Commit stages only explicit paths or all changes when explicitly requested, then returns resulting commit identity. No automatic push, force-push, tag, author policy, or review policy exists.

### Rescue and recovery

Rescue creates a commit object with `git stash create` and pins it under `refs/tabula/rescue/<timestamp>-<id>` without altering worktree or stash list. Metadata includes status, binary diff summary, untracked paths, base/head, and ref. Empty clean state returns no ref.

### Concurrency

All ref/index/worktree mutations take a cross-process lock keyed by common Git directory. Read operations remain parallel. Lock files live under tenant-local `state/plugins/vcs/locks` when runtime context exists, otherwise under a temporary directory for SDK consumers and tests.

### Hooks

`tabula_plugin_sdk.tool_policy` normalizes `vcs_*` calls as resource `vcs`, including operation, repository, paths, target, and confirmation. Existing permission and approval hooks therefore govern every call. Destructive tools additionally enforce their own preview/confirm contract and rescue creation; they do not depend on shell-command parsing.

### Installed verification

Canonical and generated testbeds install `workspace:vcs` with permission and approval hooks. The suite executes status, worktree creation/removal, commit, restore preview/confirmation, rescue-ref verification, and rollback recovery against a temporary configured repository.
