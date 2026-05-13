# AM-03 — Kernel and runtime launch orchestration

Status: proposed
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/runtime, area/kernel, area/cli

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Make `tabula-install app run ./tabula.app.toml` able to start or reuse the
kernel and runtimes described by the manifest.

This belongs to the installer/launcher layer, not distro semantics. The manifest
is still the runnable definition, so a repo can be cloned and run without first
hand-writing separate kernel/runtime config files.

Manifest topology examples:

```toml
[kernel]
mode = "managed"
id = "claw-tabula"
url = "ws://127.0.0.1:8089/ws"

[[runtimes]]
id = "local"
mode = "managed"
tenants = ["claw-tabula"]

[runtimes.exec]
backend = "bare"
```

```toml
[kernel]
mode = "external"
id = "team"
url = "wss://team-tabula.example.com/ws"

[[runtimes]]
id = "makbook-ssh"
mode = "managed"
tenants = ["claw-tabula"]

[runtimes.exec]
backend = "ssh"
host = "${local.ssh.host}"
user = "${local.ssh.user}"
root = "${project_root}"
```

Runtime modes:

- `embedded`: runtime is in-process with the managed kernel.
- `managed`: installer starts/reuses a runtime process.
- `external`: runtime must already connect to the kernel.

Runtime execution backends:

- `bare`: execute workers locally from the runtime host.
- `ssh`: runtime executes workers/commands through SSH on a configured host.
- `docker`: runtime executes workers/commands in Docker.

## Acceptance criteria

- [ ] `app run` starts a managed kernel when it is not already running.
- [ ] `app run` reuses a running managed kernel when URL/status match.
- [ ] `app run` refuses an unreachable `kernel.mode = "external"` with a clear
      error.
- [ ] `app run` starts managed runtimes with the configured tenant list and
      execution backend.
- [ ] `runtime.mode = "external"` is treated as a readiness requirement, not as a
      process to start.
- [ ] `runtime.mode = "embedded"` works only with a managed/compatible kernel
      and is rejected for external kernels.
- [ ] Local-only substitutions such as `${local.ssh.host}` are loaded from
      `.tabula/local.toml` or fail before startup.
- [ ] Runtime startup never requires the kernel to predeclare runtime topology;
      runtimes attach and advertise capabilities dynamically.

## Blocked by

- AM-01 for parsing runnable topology.
- Existing runtime attach/auth surfaces.

## Notes

- Kernel server config remains listen/url/auth only. The runnable manifest is
  the source for startup orchestration; the installer may generate process-local
  runtime config from it.
- SSH and Docker are runtime execution backends, not kernel transport modes.
