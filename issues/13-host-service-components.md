# Deliver generic host-service bundle components

**Type:** AFK  
**Status:** completed

## What to build

Add a generic bundle component type for executables that must run outside the Tabula kernel, plugin runtime, tenant workers, and active distro. A bundle may distribute a host-service artifact and service descriptor, while trusted installer/materializer code installs, registers, starts, upgrades, stops, and removes it through a platform adapter.

The first required consumer is `evolution-supervisor`, but core installer and runtime code must know only the generic `host-service` contract. They must not contain evolution policy, candidate semantics, health policy, known-good selection, or rollback decisions.

## Required discovery and design

- Inspect current bundle manifest/component resolution, source locks, payload fan-out, `tabula-install`, `tabula-distro`, `tabula-agent` service management, launchd behavior, runtime layout, reinstall/reload behavior, and uninstall semantics.
- Inspect Ouroboros packaged launcher/bootstrap boundary only as prior art for an immutable outer process; do not copy its product-specific update policy into Tabula core.
- Define host-service manifest schema, artifact provenance, installation ownership, runtime layout, platform adapter interface, lifecycle state, upgrade rules, and uninstall/purge behavior.
- Define how host-service dependencies participate in deterministic transitive bundle resolution and source locks.
- Add an ADR because this introduces a new bundle component and installed runtime surface. Update `tabula-guide` with installation, status, logs, service ownership, and troubleshooting behavior.

## Implementation plan

1. Define generic manifest metadata for a host-service component: component ID, executable artifact, arguments, environment/config references, readiness probe, shutdown timeout, platform support, and service identity. Product policy and candidate-specific health checks remain outside this schema.
2. Extend bundle dependency resolution and lock generation so host-service artifacts are pinned, hashed, deduplicated, and included in deterministic transitive closure like other components.
3. Define an installer-owned layout under `TABULA_HOME` for host-service artifacts, descriptors, logs/state references, and retained executable versions needed for safe service upgrade. Keep user source checkouts outside `TABULA_HOME`.
4. Materialize host-service artifacts transactionally without executing bundle-controlled install scripts. Validate executable hashes and descriptor schema before changing service registration.
5. Add a generic service adapter interface. Implement the existing supported local service path first, including launchd on macOS and a foreground/testbed adapter. Keep systemd support explicit if not delivered in this slice.
6. Implement install/start/status/stop/restart/upgrade/remove operations with durable lifecycle receipts. Reinstall of an unchanged artifact must be idempotent.
7. Make service upgrade install the new executable beside the old one, switch service registration atomically where the platform permits, verify generic readiness, and retain the previous executable until the upgrade is committed or reverted.
8. Define bundle removal semantics: disable and unregister service before removing active artifacts; preserve user-owned config/state by default; purge only when explicit.
9. Expose generic inspection through installer/agent CLI surfaces without adding host-service logic to the Go kernel.
10. Add an installed fixture bundle containing a harmless test service. Test install, start, status, restart, upgrade, interrupted upgrade recovery, rollback to previous executable, remove, reinstall, and generated testbed parity.

## Acceptance criteria

- [x] Bundle manifests can declare a generic `host-service` component without naming evolution or another product capability in core schemas.
- [x] Dependency resolution and lock files pin host-service source revision, artifact hash, component identity, and platform compatibility.
- [x] Installer materializes only declared artifacts and never executes arbitrary bundle install scripts.
- [x] Host service runs outside kernel, plugin runtime, tenant worker, and active distro process trees.
- [x] Host-service source code may live in a bundle repository, but installed executable and service lifecycle are owned by trusted host installer/materializer code.
- [x] Install, start, status, stop, restart, unchanged reinstall, upgrade, rollback, remove, and explicit purge have deterministic semantics and durable receipts.
- [x] Failed or interrupted host-service upgrade retains or restores the previous executable and reconciles deterministically on the next installer invocation.
- [x] Bundle removal stops and unregisters the service before active artifacts are removed and preserves user-owned state/config by default.
- [x] Go kernel remains unaware of concrete host services and contains no evolution, candidate, health-policy, or rollback semantics.
- [x] macOS launchd and foreground/testbed execution paths are covered; unsupported platforms fail with a clear compatibility error.
- [x] Installed testbed executes a fixture host service, proves process independence from `tabula serve`, upgrades it, forces failed readiness, and observes rollback.
- [x] Canonical testbed, generated testbed template, ADR, runtime docs, and installed `tabula-guide` are updated together.

## Blocked by

- [02 Resolve transitive bundle component dependencies](02-transitive-bundle-dependencies.md)
