# M5-03 — Workspace boundary enforcement migration

Status: open
Phase: M5
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/bundle, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

The current `code/hook-workspace-boundary` plugin enforces
workspace boundaries by hooking pre-tool-call events. After
M5, the boundary is enforced by `fs` plugin's roots config
itself — the hook becomes redundant for fs operations.

This slice migrates / deletes the hook plugin appropriately.

Components:

- Audit `code/hook-workspace-boundary/run.py`:
  - What does it actually validate?
  - Does it cover only fs tools (now redundant) or also other
    tools (still useful)?
- Decision tree:
  - **If the hook only inspected `files` skill calls**:
    delete the plugin entirely. fs plugin's roots
    enforcement (M5-01) provides equivalent protection at the
    correct layer.
  - **If the hook inspected `base/shell` calls and enforced
    cwd boundaries**: per Q3.4c (exec independence), this
    enforcement was theatre. Delete it. Document the policy
    change in the plugin's removal commit message and in
    `docs/plans/REMOTE_RUNTIME.md` §6.
  - **If the hook covered other tools (e.g. mcp, gateway)**:
    keep those checks, rename the plugin to reflect actual
    scope (`hook-tool-allowlist` or similar), update tests.
- Update any distro that depends on `hook-workspace-boundary`:
  - `tabula-distrib/claw/` if it pins this plugin in its
    distro manifest, switch to fs plugin's roots config or
    drop the dependency.
- Update bundle docs / READMEs.
- Migration note in CHANGELOG (if `tabula-bundles` keeps one).

## Acceptance criteria

- [ ] Audit produced and decision recorded in this issue's
      PR description before code changes.
- [ ] Hook plugin either deleted or scoped down with all
      references updated.
- [ ] Distro manifests updated to reflect new scope.
- [ ] No test still asserts old hook-workspace-boundary
      behavior.
- [ ] Tabula-distrib (if affected) tests pass.
- [ ] Documentation updated.

## Blocked by

- M5-01 (fs plugin must exist before deleting the hook that
  protected file ops, otherwise there's a window with no
  protection)

## Notes

- This is the "no legacy / no backward compat" cleanup
  applied at the bundle layer. Don't deprecate; delete
  (per `tabula-bundles/AGENTS.md`).
- If the audit finds the hook genuinely useful for non-fs
  scope, scoping it down (rather than deleting) is fine. The
  intent is "no ceremonial boundary enforcement that
  duplicates the source of truth".
