# Deliver reviewed change transactions

**Type:** AFK  
**Status:** completed

## What to build

Add a reusable `change-control` component inside the future `evolution` bundle. It must turn an objective against one or more declared source repositories into an isolated, auditable reviewed commit without touching installed runtime trees. The component provides source-change transactions, protected-path enforcement, host-attested verification evidence, independent reviews, freshness checks, reviewed commits, abort, and deterministic rescue. It must remain useful when autonomous evolution and activation are disabled.

This issue ends at a reviewed source commit plus durable evidence. It does not build release artifacts, install candidates, restart Tabula, or decide activation.

## Required discovery and design

- Inspect Ouroboros runtime mode policy, protected/frozen paths, `tools/commit_gate.py`, `tools/review.py`, `tools/verify.py`, reviewed commit flow, restart prerequisites, and failure cleanup.
- Trace exactly how hashes, receipts, reviews, scopes, branches/stashes, and rescue snapshots interact.
- Compare with Tabula hooks, permissions, approvals, artifacts, VCS, subagents, workspace roots, and distro policy.
- Define a threat model for path escape, symlink traversal, case folding, stale evidence, reviewer mutation, forged receipts, concurrent source changes, and interrupted cleanup.
- Write the transaction schema and state machine before implementation. Add an ADR if the work changes shared bundle, hook, VCS, artifact, or installer contracts.

## Implementation plan

1. Define a transaction descriptor containing objective, actor, repository identity, canonical source root, base revision, isolated worktree, allowed paths, protected paths, required checks, required reviews, and expiry/freshness policy.
2. Extend the structured workspace VCS component to create and recover isolated transaction worktrees outside `TABULA_HOME`, capture exact base/tree hashes, and produce deterministic rescue references before cleanup.
3. Add enforceable write policy through existing permission/hook surfaces. Canonicalize every path and fail closed on unknown ownership, symlink escape, traversal, case ambiguity, generated-file indirection, or writes outside the transaction worktree.
4. Add host-side verification runners that execute configured commands and emit signed or host-attested receipts bound to transaction ID, base revision, candidate tree hash, command, exit status, runner identity, timestamp, logs, and artifacts.
5. Add read-only reviewer profiles for plan, scope, architecture, security, and acceptance review. Reviewers receive candidate diff and verification evidence but no write authority.
6. Implement freshness rules: any candidate tree change invalidates prior verification and review receipts; base movement or transaction expiry blocks commit until explicitly reconciled.
7. Implement `commit_reviewed` as the only reviewed commit path. It rechecks scope, exact candidate fingerprint, required evidence, reviewer independence, and current base before creating the commit and durable receipt.
8. Implement abort and failure recovery. Preserve diagnostics and rescue references, release locks, and clean worktrees deterministically without modifying active installed distro, bundle, runtime, or kernel files.
9. Expose narrow tools for create, inspect, verify, review, commit, abort, and rescue operations. Keep activation and restart tools out of this component.
10. Add unit, integration, security, and installed-layout testbed coverage, including interruption and recovery between every durable transaction phase.

## Acceptance criteria

- [x] Transaction binds repository identity, canonical source root, base revision, isolated worktree, allowed/protected paths, objective, actor, and freshness policy.
- [x] Source worktrees live in a configured workspace root outside `TABULA_HOME`; installed trees are never treated as authoring source.
- [x] Writes outside transaction scope and protected-path changes are blocked by enforceable hooks/policy.
- [x] Symlink escape, path traversal, case ambiguity, unknown ownership, and generated-file indirection fail closed.
- [x] Verification receipts bind exact transaction, base revision, candidate hash, command, result, runner identity, timestamp, logs, and artifacts.
- [x] Candidate changes, base movement, or expiry invalidate stale verification and review receipts.
- [x] Independent review profiles record plan/scope/architecture/security/acceptance decisions without write authority.
- [x] `commit_reviewed` is blocked until configured scope, evidence, freshness, and independent-review gates pass.
- [x] Successful commit records exact source revision and immutable evidence references for later release assembly.
- [x] Abort/failure preserves diagnostics and rescue references while cleaning worktree state deterministically.
- [x] Component has no authority to install, activate, restart, mark healthy, retain known-good releases, or roll back runtime state.
- [x] Installed testbed proves successful reviewed commit plus blocked protected-path, out-of-scope, stale-evidence, reviewer-write, and symlink-escape attempts.
- [x] Canonical testbed and generated testbed template are updated and execute the installed change-control tools rather than only asserting catalog presence.

## Review status

Completed reviewed-commit lifecycle and installed adversarial coverage. Transaction actor comes from trusted tool context. Worker binding revalidates configured repository identity, exact transaction worktree path, and Git top-level before an evolution profile starts. Verification stores full stdout/stderr as owner-bound artifacts and signed receipts bind their references, digests, and byte counts to exact transaction/base/candidate evidence. Committed receipts are signature-checked and expose attested check/review evidence whose digests must match before evolution can attach them. Installed testbed proves successful commit and fail-closed protected-path, out-of-scope, stale-evidence, reviewer-mutation, and symlink-escape cases.

## Blocked by

- [03 Expose reusable subagent orchestration SDK](03-subagent-orchestration-sdk.md)
- [05 Expose durable artifact storage API](05-artifact-storage-api.md)
- [06 Add structured workspace VCS component](06-workspace-vcs-component.md)
