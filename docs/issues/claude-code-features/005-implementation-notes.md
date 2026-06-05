# 005 Isolated Worktree Subagent Tasks: Implementation Notes

Context dump for resuming work on `005-isolated-worktree-subagent-tasks.md`.

## Current conclusion

Tabula already has a substantial partial implementation for 005 in
`tabula-bundles/subagents/subagents/run.py`.

What already exists:

- `subagent_spawn` accepts `worktree` / `isolated_worktree`.
- `_resolve_worktree_request()` supports:
  - `true`
  - string branch name
  - object with `branch`, `path`, `root`, `keep`, `source`
- `_prepare_worktree()` creates or reuses a git worktree.
- child env sets `TABULA_PROJECT_ROOT=<worktree path>`.
- child process `cwd` is switched to the worktree path.
- task metadata already stores `worktree`.
- prompt/task text already gets an isolation notice injected.
- bundle unit test already proves child edits do not modify the parent repo.

This note started as a planning dump. The implementation described below is now
landed in the current working tree and validated with focused unit + installed
testbed coverage.

## Implementation status

Completed in current working tree.

Delivered behavior:

- `worktree` is now treated as isolated workspace request, not git-only request.
- backend selection is:
  - external `worktree_create_command` if configured
  - else git worktree
  - else explicit error mentioning git repo requirement and external command option
- registry/task worktree metadata now includes:
  - `backend`
  - `cleanup_policy`
  - `cleanup_state`
  - `cleanup_error`
  - `cleaned_at`
  - `created_head` for git backend
- terminal lifecycle now finalizes worktrees from:
  - `_reconcile()`
  - `_complete_from_result()`
  - `subagent_kill()`
  - `_clear_terminal_record()` before id reuse cleanup
- git cleanup semantics now match the intended parity model:
  - `keep=true` => `cleanup_state=kept`
  - `keep=false` + unchanged => remove worktree and branch, `cleanup_state=removed`
  - `keep=false` + changes/new commits => keep worktree, `cleanup_state=kept_due_to_changes`
- external backend keeps workspaces by default with explicit cleanup note because
  generic change detection is unavailable
- canonical and template installed testbed suites now cover ACP-backed isolated
  git worktree flows

## Claude Code reference findings

Local source reviewed at `/Users/mak/src/claude-code`.

Relevant files:

- `src/utils/worktree.ts`
- `src/utils/hooks.ts`
- `src/tools/AgentTool/AgentTool.tsx`
- `src/tools/AgentTool/forkSubagent.ts`
- `src/tools/AgentTool/resumeAgent.ts`
- `src/tools/AgentTool/runAgent.ts`
- `src/tools/AgentTool/agentToolUtils.ts`
- `src/utils/sessionStorage.ts`
- `src/utils/sessionRestore.ts`
- `src/tasks/LocalAgentTask/LocalAgentTask.tsx`
- `src/utils/cleanup.ts`
- `src/setup.ts`
- `src/bridge/bridgeMain.ts`
- `src/utils/hooks/hooksConfigManager.ts`

Important behavior in Claude Code:

1. Worktree support is first-class and always enabled.
2. There are separate APIs for main-session worktrees and subagent worktrees.
3. Worktree creation is idempotent: existing worktrees are resumed, not recreated.
4. Backend selection is hooks-first, git-second, explicit error otherwise.
5. Worktree paths/branches are deterministic from a validated slug.
6. Post-create setup copies local settings, configures hooks, symlinks selected
   directories, and copies `.worktreeinclude` gitignored files.
7. Subagents get an explicit notice that inherited paths refer to the parent
   workspace and files should be reread before editing.
8. Worktree path is persisted in per-agent metadata and restored on resume.
9. Resumed worktrees bump mtime so stale cleanup does not race them.
10. Cleanup is change-aware:
    - if no changes/new commits, remove worktree and branch
    - if changed, keep the worktree
11. Hook-based subagent worktrees are kept because generic change detection is
    not available for arbitrary VCS backends.
12. Task completion notifications include worktree metadata.

## Claude Code exact mechanics

### Backend selection

`claude-code` does **not** treat worktree isolation as git-only.

Observed behavior in source:

- if `WorktreeCreate` hook exists, it takes precedence
- hook backend is treated as a VCS-agnostic isolation provider
- if no hook exists, it falls back to git worktrees
- if neither hook nor git repo is available, it fails clearly

Relevant source points:

- `src/utils/worktree.ts:createWorktreeForSession()`
- `src/utils/worktree.ts:createAgentWorktree()`
- `src/setup.ts`
- `src/bridge/bridgeMain.ts`
- `src/utils/hooks.ts`
- `src/utils/hooks/hooksConfigManager.ts`

Hook contracts:

- `WorktreeCreate`: JSON input `{ name }`, stdout must be absolute worktree path
- `WorktreeRemove`: JSON input `{ worktree_path }`

The hook path is not a soft error fallback. It is a first-class backend.

### Slug, path, and branch rules

- slug max length: `64`
- allowed slug segments: `[a-zA-Z0-9._-]+`
- `/` is allowed in input slug but flattened to `+` for both directory and branch
- branch name: `worktree-<flattened slug>`
- directory: `<repo>/.claude/worktrees/<flattened slug>`
- validation rejects `.` / `..`, empty segments, and path traversal

Important reason for flattening:

- nested directory worktrees are unsafe because removing a parent worktree can
  delete child worktrees underneath it
- nested branch refs create git file/dir conflicts

### Git create/resume behavior

- create path is deterministic and idempotent
- fast resume reads the worktree `.git` pointer / HEAD directly without spawning git
- agent worktrees use `findCanonicalGitRoot()` so spawning from inside a
  worktree still creates children under the main repo's managed worktree root,
  not nested under the current worktree
- git create uses `git worktree add -B <branch> <path> <base>`
- `-B` is intentional so orphaned leftover branches are reset instead of causing
  create failures

### Git base branch selection

- normal path prefers local `origin/<defaultBranch>` ref if already present
- otherwise fetches `origin <defaultBranch>`
- PR path fetches `pull/<pr>/head` and uses `FETCH_HEAD`
- if fetch fails in the non-PR path it falls back to `HEAD`

### Optional post-create setup

For newly created git worktrees, Claude Code additionally:

- copies `settings.local.json`
- configures `core.hooksPath` to the main repo hooks directory when needed
- optionally symlinks selected directories from settings
- copies gitignored files matched by `.worktreeinclude`
- optionally installs commit attribution hook material in the worktree

These are useful reference ideas but are not the core minimum for 005.

### Child prompt notice

The injected child notice explicitly says:

- the child inherited context from a parent workspace
- child cwd is an isolated git worktree
- relative file layout is the same
- inherited paths refer to the parent workspace and must be translated
- files should be reread before editing if parent may have changed them
- child changes stay isolated from the parent files

Source:

- `src/tools/AgentTool/forkSubagent.ts:buildWorktreeNotice()`

### Metadata persistence and resume

- `runAgent()` writes per-agent metadata sidecar with:
  - `agentType`
  - `worktreePath` when present
  - `description`
- `resumeAgentBackground()` reads metadata and:
  - verifies the worktree directory still exists
  - falls back to parent cwd if it no longer exists
  - bumps worktree mtime on successful resume
  - reruns the agent under `runWithCwdOverride(worktreePath)`
  - re-emits `worktreePath` into metadata so later overwrites do not lose it

Task/session-level worktree state is also persisted separately for interactive
session worktrees and restored on `/resume`.

### Terminal cleanup behavior for subagents

For async subagents, cleanup result is resolved after terminal state and then
attached to notification output.

Git backend behavior:

- if `hasWorktreeChanges(worktreePath, headCommit)` is false:
  - remove worktree via `git worktree remove --force`
  - delete branch via `git branch -D`
  - clear `worktreePath` from persisted agent metadata
- if changes or new commits exist:
  - keep the worktree
  - return `worktreePath` and `worktreeBranch` in terminal result

Hook backend behavior:

- hook-based agent worktrees are kept on normal subagent completion
- reason: Claude Code cannot do generic change detection across arbitrary VCS

Terminal notifications include worktree data on:

- completed
- failed
- killed

Sources:

- `src/tools/AgentTool/AgentTool.tsx`
- `src/tools/AgentTool/agentToolUtils.ts`
- `src/tasks/LocalAgentTask/LocalAgentTask.tsx`

### Change detection details

`hasWorktreeChanges(worktreePath, headCommit)` is fail-closed.

It keeps the worktree when any of these is true:

- `git status --porcelain` fails
- working tree is dirty
- `git rev-list --count <headCommit>..HEAD` fails
- there are commits after the creation baseline

### Periodic stale cleanup

Claude Code also has periodic cleanup for leaked ephemeral agent/workflow
worktrees.

Behavior:

- scans managed worktrees under the canonical root
- only considers names matching exact ephemeral patterns
- skips the current active worktree
- only removes worktrees older than the cleanup cutoff date
- fail-closed: skips entries if git status fails or tracked changes exist
- fail-closed: skips entries with commits not reachable from a remote
- after removals, runs `git worktree prune`

This is separate from per-task completion cleanup.

## Current Tabula implementation status vs Claude Code

### Already covered

- Opt-in isolated git worktree request shape.
- Deterministic managed worktree root.
- Child `cwd` / workspace root override.
- Prompt notice about parent paths and reread requirement.
- Worktree metadata stored on task entry.
- Unit proof that child edits do not touch parent repo.

### Former gaps now closed

1. Terminal worktree finalization lifecycle implemented.
2. Change-aware git cleanup implemented.
3. Cleanup outcome metadata persisted on task/registry worktree state.
4. Installed testbed coverage added for worktree behavior.
5. `created_head` baseline persisted for git cleanup decisions.

## Parity target for Tabula

User requirement: do **not** ship a behavior that is narrower than current
`claude-code`.

That means the parity target is:

- backend selection: `hook backend if configured, else git, else explicit error`
- deterministic managed paths/branches for git backend
- persisted worktree metadata for resume/notifications
- change-aware git cleanup on terminal states
- keep-on-complete semantics for non-git hook backends unless Tabula can define
  generic change detection for them

Implication:

- a pure git-only Tabula implementation is **not** sufficient for parity with
  current `claude-code`

## Design implications for Tabula

Tabula currently has git-worktree foundations in `subagents/run.py`, but no
obvious existing `WorktreeCreate` / `WorktreeRemove` equivalent was found during
this pass.

So parity likely requires adding one of these:

1. A new external worktree-provider hook contract for subagents.
2. A pluggable isolation provider abstraction with at least:
   - `git`
   - `external hook/command provider`

Open implementation question to resolve during coding:

- where should the non-git provider contract live so the kernel stays generic
  and distro policy stays outside the kernel?

Likely answer:

- keep the policy/provider wiring in bundle/runtime/plugin space, not kernel

## Recommended implementation scope

### Repo

- Primary: `tabula-bundles`
- Testbed: `tabula-distrib`
- Template sync: `tabula/tools/tabula-testbed`

### Files likely to change

- `tabula-bundles/subagents/subagents/run.py`
- `tabula-bundles/subagents/tests/test_subagents_plugin.py`
- `tabula-distrib/testbed/tests/test_subagents.py`
- `tabula/tools/tabula-testbed/src/tabula_testbed_runner/testbed_template/tests/test_subagents.py`
- optionally `docs/issues/claude-code-features/005-isolated-worktree-subagent-tasks.md`

## Concrete design for Tabula

### Keep current request surface

Do not redesign the spawn interface. Keep:

- `worktree: true`
- `worktree: "branch-name"`
- `worktree: { source, root, path, branch, keep }`

But for parity, treat this as an isolation request whose backend may be:

- git-managed worktree
- external hook/provider-managed isolated workspace

### Extend stored worktree metadata

Add fields like:

- `enabled`
- `path`
- `source`
- `branch`
- `backend`: `git` | `hook`
- `keep`
- `reused`
- `created_head`
- `cleanup_policy`: `keep` | `auto_remove`
- `cleanup_state`: `pending` | `kept` | `removed` | `remove_failed` | `kept_due_to_changes`
- `cleanup_error`
- `cleaned_at`

### Add cleanup helpers

Suggested helpers in `subagents/run.py`:

- `_worktree_head(parent, path) -> str`
- `_worktree_has_changes(path, created_head) -> bool`
- `_finalize_worktree(entry) -> dict`
- `_create_external_worktree(request) -> dict`
- `_remove_external_worktree(entry) -> bool`

Behavior:

- if `keep=true`: mark `cleanup_state="kept"`
- if `keep=false` and no changes: remove worktree and delete branch,
  mark `cleanup_state="removed"`
- if `keep=false` and changes exist: keep it, mark
  `cleanup_state="kept_due_to_changes"`
- if backend is external/hook and generic change detection is unavailable:
  keep the workspace by default and mark `cleanup_state="kept"`
- on removal error: `cleanup_state="remove_failed"`, store `cleanup_error`

### Call finalization from terminal transitions

Invoke worktree finalization when subagent becomes terminal:

- completion path
- failed path
- kill path

### Keep current notice, only tune wording if needed

Current Tabula notice is already close enough. Desired semantics:

- same repository
- separate working copy
- inherited paths refer to parent workspace
- reread files before editing
- child changes do not affect parent files

## Test plan

### Bundle unit tests

Add/extend tests for:

1. `worktree.keep=true` leaves worktree in place and marks `cleanup_state=kept`
2. `worktree.keep=false` with no child changes removes worktree and branch
3. `worktree.keep=false` with modified file keeps worktree and marks
   `cleanup_state=kept_due_to_changes`
4. external/hook backend path can create an isolated workspace without git
5. missing git repo and no external provider returns explicit error

### Installed testbed coverage

Need real installed behavior, not unit-only.

Recommended scenario:

1. Create small git repo fixture in testbed temp dir.
2. Spawn async subagent with `worktree={source: repo, keep: true}`.
3. Verify child runner sees:
   - `cwd == worktree path`
   - `TABULA_PROJECT_ROOT == worktree path`
4. Make child modify a file.
5. Verify parent repo file is unchanged.
6. Verify task metadata includes worktree path.
7. Add second case with `keep=false` and no changes:
   - final metadata says `cleanup_state=removed`
   - worktree path no longer exists
8. Add third case with `keep=false` and changes:
   - final metadata says `cleanup_state=kept_due_to_changes`
   - worktree path still exists
9. Add external/hook-provider case without git repo:
   - isolated workspace path is returned
   - child cwd/root point there
   - completion keeps workspace unless provider-specific change detection exists

## Validation commands to run when implementing

Bundle-focused:

```bash
python3 -m unittest subagents.tests.test_subagents_plugin
```

Installed testbed:

```bash
PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite subagents --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib
```

Verified passing on 2026-06-04:

- `python3 -m unittest subagents.tests.test_subagents_plugin`
- `PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite subagents --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib`

## Non-goals for this pass

Do not add yet:

- copy-based isolation backend
- cross-distro policy in kernel
- broad resume UI work for worktrees

Goal for 005 is to reach current `claude-code` parity for subagent worktree
isolation semantics, then prove it in installed testbed.

## Concrete implementation checklist

This checklist is written against the current Tabula code in
`tabula-bundles/subagents/subagents/run.py`.

### 1. Replace git-only assumption with provider selection

Current state:

- `_prepare_worktree()` assumes git immediately
- it errors on `if not (parent / ".git").exists()`
- tool schema text says `isolated git worktree`

Implementation steps:

- split current `_prepare_worktree()` into backend-aware flow:
  - `_prepare_git_worktree(...)`
  - `_prepare_external_worktree(...)`
  - `_prepare_worktree(...)` as dispatcher
- backend selection order should match `claude-code` parity:
  - external provider if configured/available for this tenant/runtime
  - else git if source is a git repo
  - else explicit error
- add `backend` to stored worktree metadata
- update schema/help text so `worktree` means isolation request, not only git

### 2. Define external provider contract

Current gap:

- no obvious `WorktreeCreate` / `WorktreeRemove` equivalent exists in bundles

Implementation steps:

- decide the narrowest generic contract Tabula can support now
- likely shape:
  - create hook/provider input:
    - suggested stable name/id
    - parent/source path
    - optional requested root/path
    - task/session metadata needed for logging only
  - create result:
    - absolute isolated workspace path
    - optional provider metadata
  - remove hook/provider input:
    - isolated workspace path
    - any provider metadata needed for cleanup
- keep this contract outside kernel policy
- store enough metadata in registry entry to call remove later

Open design note:

- if there is already a generic plugin hook mechanism usable from bundles, reuse
  that instead of inventing a parallel one

### 3. Preserve current git path, but align details with parity target

Current state:

- `_prepare_worktree()` already creates deterministic managed paths under
  `_worktree_root() / sid`
- it already supports `branch`, `root`, `path`, `source`, `keep`
- it already injects a notice and sets child `cwd` / `TABULA_PROJECT_ROOT`

Implementation steps:

- keep existing request surface unchanged
- add `created_head` capture for git backend immediately after create/resume
- add `backend="git"`
- keep `reused` flag
- consider improving branch/path determinism toward a stable slug strategy if
  current `sid`-based path is not enough for resume semantics

### 4. Implement finalization lifecycle

Current state:

- `_reconcile()`, `_complete_from_result()`, and `subagent_kill()` change task
  status, but do not finalize worktree lifecycle
- current metadata has `cleanup: "keep" if keep else "manual"`, which is not
  enough for actual cleanup outcome tracking

Implementation steps:

- add helper such as `_finalize_worktree_for_entry(entry) -> dict`
- make it idempotent so terminal paths can safely call it more than once
- update worktree metadata with terminal cleanup result:
  - `cleanup_policy`
  - `cleanup_state`
  - `cleanup_error`
  - `cleaned_at`
- invoke finalization from every terminal path:
  - `_reconcile()` when pid is dead
  - `_complete_from_result()` when result file finalizes completion
  - `subagent_kill()` after kill completes
  - failed path in `subagent_kill()` when pid was already dead

### 5. Implement git change detection

Current gap:

- no `created_head` baseline stored
- no dirty/new-commit check before removal

Implementation steps:

- add helper `_git_worktree_has_changes(path, created_head) -> bool`
- make it fail-closed like `claude-code`
- keep worktree if any of these happen:
  - git status fails
  - working tree is dirty
  - commit comparison fails
  - there are commits after `created_head`
- if unchanged and `keep=false`:
  - remove worktree
  - delete temporary branch
  - mark `cleanup_state="removed"`
- if changed and `keep=false`:
  - keep worktree
  - mark `cleanup_state="kept_due_to_changes"`

### 6. Define external-backend completion semantics

To stay aligned with `claude-code` minimum semantics:

- if backend is external/non-git and no generic change detection exists:
  - keep workspace on completion/failure/kill
  - mark `cleanup_state="kept"`
  - optionally call provider remove only when explicit policy says always-remove

Do not guess at VCS state for arbitrary providers.

### 7. Update registry/session metadata surfaces

Current state:

- registry entry stores `worktree`
- `write_session_meta()` already includes `worktree`
- `_task_snapshot()` already includes `worktree`

Implementation steps:

- ensure final cleanup outcome mutations are persisted back into:
  - registry entry
  - task snapshot / ledger events
  - session metadata if needed for resume/inspection
- ensure terminal tool results expose final worktree metadata
- if a worktree was auto-removed, decide whether returned metadata should keep:
  - original path plus `cleanup_state="removed"`
  - or clear path and keep removal audit fields

Recommended:

- keep the path string in metadata for auditability, plus explicit removal state

### 8. Revisit id reuse behavior

Current state:

- `_clear_terminal_record()` deletes registry/result files so an id can be reused

Implementation concern:

- if worktree cleanup is deferred to terminal reconciliation, id reuse must not
  orphan provider metadata or managed worktrees

Implementation steps:

- ensure terminal cleanup runs before terminal record deletion
- if cleanup already happened, record enough final state before clearing
- add regression test for `id` reuse after worktree-backed subagent completion

### 9. Unit tests to add in `subagents/tests/test_subagents_plugin.py`

Keep existing `test_async_spawn_can_use_isolated_git_worktree()`.

Add focused tests for:

- git backend selected when source is a git repo and no external provider exists
- external provider selected when configured, even without git repo
- explicit error when neither external provider nor git repo exists
- `keep=true` leaves worktree and marks `cleanup_state="kept"`
- `keep=false` + unchanged tree removes worktree/branch and marks `removed`
- `keep=false` + modified file keeps worktree and marks `kept_due_to_changes`
- external backend terminal path keeps workspace by default
- finalization runs on `_reconcile()` terminal completion
- finalization runs on `subagent_kill()` terminal path
- id reuse after finalized worktree-backed task does not conflict

### 10. Installed testbed coverage to add in `tabula-distrib/testbed/tests/test_subagents.py`

Current state:

- testbed covers provider override, tenant delivery, ACP async lifecycle, and
  installed layout
- there is no installed worktree-isolation test yet

Add installed tests for:

- git worktree spawn:
  - create temp git repo fixture
  - spawn subagent with `worktree={source: repo, keep: true}`
  - verify child cwd and `TABULA_PROJECT_ROOT` point to isolated path
  - verify parent file is unchanged after child-side mutation
  - verify task/worktree metadata is surfaced
- git auto-remove path:
  - `keep=false`
  - no child changes
  - verify final metadata says removed and path no longer exists
- git changed-tree path:
  - `keep=false`
  - child edits file
  - verify final metadata says kept due to changes and path still exists
- external provider path:
  - only if parity implementation lands in this change
  - verify non-git source can still isolate through provider contract

### 11. Template sync

If testbed coverage changes, sync canonical and generated template files:

- `tabula-distrib/testbed/tests/test_subagents.py`
- `tabula/tools/tabula-testbed/src/tabula_testbed_runner/testbed_template/tests/test_subagents.py`

### 12. Documentation updates

When code lands, update at least:

- `docs/issues/claude-code-features/005-isolated-worktree-subagent-tasks.md`
  with completed scope/status notes
- subagent tool schema descriptions in `run.py`
- any bundle docs that describe isolated worktree behavior if they exist

### 13. Suggested execution order

1. Refactor `_prepare_worktree()` into backend-aware helpers without changing
   the public request shape.
2. Add metadata fields and git `created_head` baseline.
3. Implement finalization helpers and wire them into all terminal paths.
4. Add unit tests for git cleanup semantics.
5. Add external-provider path and tests if parity backend is part of the same
   change.
6. Add installed testbed coverage.
7. Sync testbed template.
8. Update docs.
