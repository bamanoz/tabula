# AM-10 — Runtime execution backends: SSH and Docker

Status: paused
Type: AFK
Repo: tabula, tabula-bundles
Labels: needs-triage, area/runtime, area/sandboxing, area/security

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Paused: do not pick this up as part of the agent-manifest continuation track.
SSH/Docker execution backends are a separate runtime/sandboxing project and can
be designed independently after the runnable manifest path is stable.

When resumed, this issue should implement runtime execution backends declared in
runnable manifests.

Current state:

- Manifest accepts `[runtimes.exec] backend = "ssh"` or `"docker"` shapes.
- `app run` rejects managed non-`bare` backends as not implemented yet.

Target model:

```text
kernel <-> runtime control plane
runtime -> execution backend -> worker process
```

Backends:

- `bare`: worker process runs on the runtime host.
- `ssh`: runtime starts worker commands over SSH on a configured host.
- `docker`: runtime starts worker commands inside Docker with configured image,
  workdir, mounts, env, and cleanup policy.

## Acceptance criteria

- [ ] `backend = "ssh"` can run plugin workers over SSH while preserving the
      existing worker stdio protocol.
- [ ] `backend = "docker"` can run plugin workers in a container while
      preserving the existing worker stdio protocol.
- [ ] Runtime advertises execution backend labels/capabilities in status.
- [ ] Secrets such as SSH credentials are not stored in app lockfiles.
- [ ] Backend config supports local overrides for machine-specific host/user/path
      values.
- [ ] Tests prove `exec_run` runs in the selected backend, not on the kernel host.
- [ ] Tests prove filesystem roots are scoped to the backend execution context.

## Blocked by

- AM-03 for manifest topology parsing and launch orchestration.
- AM-09 if backend capability routing needs tenant-aware catalogs.

## Notes

- SSH and Docker are runtime execution backends, not kernel transport modes.
- Kernel does not need to know how a runtime executes workers.
