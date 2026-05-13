# AM-22 — Final app manifest docs

Status: completed
Type: AFK
Repo: tabula, tabula-distrib
Labels: needs-triage, area/docs, area/installer, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Refresh user-facing and architecture docs so they describe final app-manifest
behavior precisely and remove stale current-limit text.

Docs should explain what belongs to the generic installer, what belongs to the
kernel/runtime tenant substrate, and what belongs to each distro's application
contract.

## Acceptance criteria

- [x] `docs/AGENT_APPLICATIONS.md` matches implemented behavior.
- [x] Track README current risks and limits are updated.
- [x] User examples use `tabula-install`.
- [x] Docs explain what belongs in `tabula` vs installer vs distro.
- [x] Docs explicitly call out paused SSH/Docker backend support.
- [x] Testbed coverage references are up to date.

## Blocked by

- AM-16.
- AM-17.
- AM-18.
- AM-19.
- AM-20.

## Notes

- Do not overclaim support for future execution backends or distro-specific
  behavior that is not covered by installed tests.
- Final docs now describe concurrent tenant-scoped app catalogs, launcher
  bindings, inspect output, memory modes, and the paused SSH/Docker backend work.
