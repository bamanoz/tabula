# AM-20 — Two apps in one kernel

Status: completed
Type: AFK
Repo: tabula, tabula-distrib
Labels: needs-triage, area/runtime, area/kernel, area/testbed, area/tenancy

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add a live testbed scenario with two app manifests running against one kernel.

Each app must have its own tenant surface, runtime catalog, tool config, state,
and boot metadata. The same plugin ids may appear in both app catalogs without
colliding.

## Acceptance criteria

- [x] Two app manifests apply successfully.
- [x] Both apps can join one kernel concurrently with distinct tenant ids.
- [x] Each app sees only its own tenant-scoped tool catalog.
- [x] Duplicate plugin ids across app catalogs do not collide.
- [x] `fs` and `exec` operations use each app's own configured roots/cwd.
- [x] App state and memory remain isolated unless explicitly shared.

## Blocked by

- AM-16.
- AM-18.

## Notes

- AM-09 added the tenant-aware runtime catalog machinery. This issue proves the
  final app-manifest behavior end to end.
- Verified by `claw-app-manifest` with `claw-alpha` and `claw-beta` in one live
  kernel using duplicate `fs`, `exec`, and memory plugin ids.
