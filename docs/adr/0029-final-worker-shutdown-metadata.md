# ADR 0029 - Distinguish final runtime shutdown from worker replacement

Date: 2026-08-08
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Runtime-scoped plugins may supervise detached child processes. A plugin worker
can be replaced while `tabula-runtime` remains active, for example during a
catalog reload, or it can be stopped because the runtime process itself is
exiting.

The worker protocol previously exposed only free-form shutdown diagnostic text.
That did not provide a stable lifecycle signal. Gateway supervisors therefore
preserved their detached daemons on every worker shutdown to avoid interrupting
hot reloads. The same behavior also preserved stale gateway daemons across a
full agent stop or development reinstall.

The kernel and installer must not know which concrete plugins supervise child
processes or how those children should be cleaned up.

## Decision

The runtime worker `shutdown` frame gains optional boolean field `final`.

- `final = false` or absent means the runtime remains active and is replacing or
  evicting this worker. Plugin-owned detached services may remain available for
  the replacement worker to reclaim.
- `final = true` means the runtime process is exiting. Plugins must release all
  runtime-owned resources, including supervised child processes.

The existing `reason` field remains diagnostic text and is not a lifecycle enum.
Plugin SDKs expose both values to shutdown handlers without moving
plugin-specific policy into the runtime.

`Pool.Reload` sends a non-final shutdown. `Pool.Close` sends a final shutdown.
Other worker replacement and failure paths remain non-final.

A detached service also records the PID of its owning runtime process. Replacement
workers reuse it only when that PID matches their own runtime parent. The service
monitors that runtime PID and exits if the runtime disappears unexpectedly. This
closes the race where a host stops the runtime process before its cooperative
worker cleanup finishes, while still preserving the service across worker-only
reloads inside the same runtime.

## Consequences

Positive:

- hot reload can preserve long-lived plugin-owned services;
- full runtime and agent shutdown deterministically remove plugin-owned child
  processes;
- the kernel, runtime, and installer remain generic lifecycle boundaries;
- plugins can choose cleanup behavior without parsing diagnostic strings.

Negative:

- SDK-backed supervisors that detach children must inspect final shutdown
  metadata;
- old SDKs ignore the optional field and retain their previous cleanup behavior
  until updated.

## Verification

Implementation must prove:

- worker protocol round-trips `shutdown.final`;
- runtime reload sends a non-final shutdown;
- runtime close sends a final shutdown;
- Python SDK shutdown handlers can inspect reason and final metadata;
- gateway Web and API supervisors preserve daemons only for replacement workers
  under the same runtime PID, and stop them on final runtime shutdown or runtime
  disappearance;
- a real isolated `make agent dev` reinstall replaces the previous gateway
  daemon and starts a fresh one.
