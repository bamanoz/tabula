# Resolve transitive bundle component dependencies

**Type:** AFK  
**Status:** completed

## What to build

Let a distro select one capability bundle such as `evolution` and have installer resolution include its declared supporting bundles/components at one pinned revision. Preserve deterministic locks, conflict detection, component allowlists, and installer fan-out.

## Required discovery and design

- Inspect how Ouroboros packages and validates optional/full capability modes and dependency prerequisites.
- Inspect current Tabula distro source resolution, bundle manifests, dependency validation, install locking, and tenant materialization.
- Design Tabula-native dependency closure; do not copy Python package/runtime assumptions from Ouroboros.
- Add or update ADR because bundle resolution and lock semantics are architecture-level contracts.

## Acceptance criteria

- [x] `bundle.toml` can declare required bundle components, not only exported SDK packages.
- [x] Resolution is transitive, deterministic, cycle-safe, and conflict-checked.
- [x] Resolved source revisions and selected components appear in immutable install locks.
- [x] Adding a fixture capability bundle to a distro installs and executes a required component end to end.
- [x] Explicit distro restrictions and incompatible component selections fail with actionable diagnostics.
- [x] Existing bundles without component dependencies keep current behavior.
- [x] Installer docs, schema, testbed fixture, generated testbed template, and ADR are updated.

- [x] Canonical testbed and generated testbed template are updated; the suite installs the fixture capability and executes its resolved dependency.

## Blocked by

- [01 Define modular capability architecture](01-modular-capability-architecture.md)
