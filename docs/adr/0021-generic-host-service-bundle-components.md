# ADR 0021: Generic Host-Service Bundle Components

- Status: Accepted
- Date: 2026-08-01
- Supersedes: none
- Superseded by: none

## Context

Some bundle capabilities need a process that remains outside the Tabula kernel,
local runtime, plugin workers, tenant workers, and replaceable distro tree. The
planned evolution supervisor is the first consumer, but its release policy,
health verdicts, known-good state, and rollback authority must not enter the
kernel or generic installer schema.

Bundle component resolution already pins sources and component selections. ADR
0020 limits distro installation to crash-safe replacement of one composed distro
tree. A host process therefore cannot execute directly from that tree: the next
distro replacement may remove its executable while it is running.

## Decision

A bundle component directory may declare `service.toml`. It is a generic
`host-service` component alongside skill, plugin, and app components.

The descriptor contains only process mechanics:

- stable service ID and executable entry;
- arguments and string environment values;
- supported platforms;
- generic `process` or file readiness;
- graceful shutdown timeout.

The installer validates the descriptor, requires the entry to be an executable
file, copies only the declared component, and records its artifact SHA-256 and
platforms in distro lock version 6 under `host_services`. It never executes a
bundle-controlled installer script.

Composed source lives at:

```text
$TABULA_HOME/distrib/<distro>/host-services/<service-id>/
```

Trusted host-service reconciliation copies immutable artifacts to:

```text
$TABULA_HOME/host-services/<service-id>/releases/<sha256>/
$TABULA_HOME/host-services/<service-id>/current
$TABULA_HOME/host-services/<service-id>/previous
```

Service-owned state, lifecycle data, and logs live at:

```text
$TABULA_HOME/host-services/<service-id>/state/
$TABULA_HOME/host-services/<service-id>/run/
$TABULA_HOME/host-services/<service-id>/receipts.jsonl
$TABULA_HOME/logs/host-services/<service-id>.*.log
```

One installed distro owns each service ID. Another distro cannot silently take
it over. Full-distro reconciliation removes services no longer declared by that
owner, stopping and unregistering them before removing executable releases.
State survives ordinary removal; explicit purge removes it.

Activation is journaled. Upgrade retains the previous release, stops the old
process, switches `current`, starts the candidate, and checks generic readiness.
Failed readiness restores and restarts the previous release. A later reconcile
restores the previous release from an interrupted prepared or switched
transaction before doing new work.

Platform adapters remain outside the Go kernel. Initial adapters are:

- macOS user `launchd`, used by default on macOS;
- explicit detached process mode for foreground development and testbeds.

No persistent Linux adapter is delivered in this slice. Automatic selection on
Linux fails clearly; `--adapter process` is explicit test/development behavior.
A future systemd adapter can implement the same lifecycle contract without
changing component semantics.

The Go kernel knows nothing about host-service identities, descriptors,
readiness, activation, upgrade, or rollback. Evolution policy remains in the
future evolution bundle controller and supervisor.

## Consequences

- Bundle repositories can ship externally managed executables without arbitrary install scripts.
- Running executables survive distro tree replacement because releases are retained separately.
- Distro locks attest exact host-service bytes and platform compatibility.
- Service upgrades have deterministic rollback and interrupted-operation recovery.
- Host services are host-global and require explicit ownership conflict handling.
- Linux production persistence remains unsupported until a systemd adapter is added.
- Supervisor self-update still requires a separate trust and recovery policy owned by evolution, not this generic contract.
