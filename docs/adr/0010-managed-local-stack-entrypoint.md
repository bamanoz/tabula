# ADR 0010 - Managed local stack entrypoint

Date: 2026-07-23
Status: Accepted
Supersedes: ADR 0001 local runtime ownership and local startup topology
Superseded by: nothing

## Context

ADR 0001 introduced `tabula-runtime` and originally assigned local runtime
ownership to the kernel. Later implementation moved local startup into
`tabula-runner`, which launched `tabula serve` and `tabula-runtime` as sibling
processes. Foreground app runs, user services, Docker entrypoints, and direct
kernel launches consequently used different orchestration, environment,
readiness, signal, and shutdown paths.

`tabula serve` already supports `--runtime-mode managed` and supervises the
local runtime child. Keeping a second wrapper duplicates lifecycle behavior and
makes service ownership unclear.

## Decision

`tabula serve --runtime-mode managed` is the sole standard local stack
entrypoint.

- launchd and systemd execute it directly.
- Foreground app and agent launches execute it directly.
- Docker entrypoints execute it directly.
- Kernel starts and owns the local `tabula-runtime` child.
- Kernel shutdown terminates and waits for that child.
- `tabula serve --runtime-mode external` remains the composable entrypoint for
  separately managed or remote runtimes.
- `tabula-runner` and all runner-specific install, environment, readiness, and
  signal paths are removed without a compatibility alias.

Readiness remains observable through kernel health and runtime status. Tenant
readiness requires an attached runtime whose tenant catalog and capability
snapshot include the selected tenant; frontend launch then passes that tenant
explicitly.

## Consequences

- Foreground, service, Docker, and agent-managed startup share one process tree.
- Service manager owns kernel; kernel owns local runtime.
- Environment loading and shutdown behavior have one implementation.
- Existing commands invoking `tabula-runner` must switch to
  `tabula serve --runtime-mode managed`.
- External runtime deployments remain unchanged except for using explicit
  `--runtime-mode external`.
