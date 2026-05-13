# AM-21 — App boot metadata audit

Status: completed
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/runtime, area/docs

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Tighten and audit the app tenant layout so the persisted app surface matches the
target model or documents a deliberate equivalent.

This slice makes app-scoped boot metadata inspectable and stable through
`tabula-install app inspect` and audit output.

## Acceptance criteria

- [x] `tenants/<app>/tenant.toml`, `app.toml`, `app.lock.json`, and
      `values.toml` are present and internally consistent.
- [x] App-scoped boot metadata is present and consumed by runtime/client launch
      path.
- [x] App-scoped `distrib/` or an explicitly documented equivalent exists.
- [x] `tabula-install app inspect <app-id>` reports the app's source, lock,
      bindings, runtime surface, and materializer status.
- [x] Audit output highlights missing or stale app-scoped surfaces.

## Blocked by

- AM-14.

## Notes

- This issue should clarify whether the target `tenants/<app>/distrib/` layout is
  physical, symlinked, or represented by equivalent lock/source metadata.
- Implemented as inspectable tenant-local app metadata plus lock/source metadata;
  installed tests assert `app inspect --json` for the real Claw app path.
