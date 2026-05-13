# AM-19 — App memory modes

Status: completed
Type: AFK
Repo: tabula, tabula-distrib, tabula-bundles
Labels: needs-triage, area/memory, area/tenancy, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add real app-level memory coverage showing that project/app memory is
tenant-local by default, while shared memory is explicit and can be shared across
apps only when configured by distro/plugin config.

## Acceptance criteria

- [x] App A project memory writes under `tenants/<app-a>/state/...`.
- [x] App B project memory does not see App A memory by default.
- [x] Shared memory mode points at an explicit configured path or reference.
- [x] Two apps configured with the same shared memory path can both read shared
      entries.
- [x] No generic kernel memory semantics are added.

## Blocked by

- AM-15.

## Notes

- Memory mode semantics belong to the distro/plugin materializer, not the kernel.
- Verified by the real Claw installed suite. Kernel/runtime only route tenant
  plugin config; memory mode meaning stays in the Claw materializer and memory
  bundle config.
