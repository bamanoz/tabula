# ADR 0018: Structured Workspace VCS Owner

- Status: Accepted
- Date: 2026-07-30
- Supersedes: none
- Superseded by: none

## Context

Agents and controlled-change workflows need Git status, diff, history, isolated worktrees, commits, and recoverable destructive changes. General shell execution exposes only command prose and cannot establish repository identity, structured impact previews, rescue refs, or safe serialization. Subagent orchestration already contains private worktree code, creating duplicate behavior and race risk if a second VCS implementation is added.

Embedding Git policy in the kernel would violate the dumb-kernel boundary. Ouroboros demonstrates useful rescue and rollback mechanics, but its evolution lifecycle, managed remotes, review gates, automatic push/tag behavior, and supervisor policy are product-specific.

## Decision

The optional `workspace:vcs` component owns generic structured Git operations. The workspace bundle exports `tabula_workspace_vcs` as shared implementation for the plugin and subagent worktree orchestration.

Repositories are explicit configured identities. Calls may select only configured repositories or attached worktrees belonging to their common Git directories. Results expose repository/worktree roots, Git directory, branch, HEAD/base, changed paths, and resulting refs.

Mutating ref/index/worktree operations are cross-process serialized per common Git directory. Destructive restore, revert, and rollback use a preview/confirm contract and create a recoverable rescue ref where repository state exists. Rescue refs are local `refs/tabula/rescue/*` snapshots and never imply push or retention policy.

`vcs_*` calls are normalized as a first-class `vcs` security resource so normal permission and approval hooks govern them. No VCS-specific policy enters the kernel.

## Consequences

- Normal agents and higher-level reviewed-change workflows share one roots-aware structured Git contract.
- Subagent worktrees can reuse the same serialized implementation instead of private Git process code.
- Recovery remains possible after confirmed destructive local operations.
- Distro policy may decide which repositories and actors are allowed, but the workspace bundle does not encode evolution, review, remote, push, tagging, or protected-path policy.
- Kernel protocol, session persistence, and runtime API remain unchanged.
