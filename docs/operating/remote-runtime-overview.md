# Remote Runtime Overview

Tabula separates the kernel from runtimes. The kernel owns sessions, routing,
tenant policy, and the client WebSocket. Runtimes own plugin and skill execution.

## Backend Choices

Use the local runtime when the kernel and execution environment are on the same
host. This is the default installed layout and uses the local unix socket plus a
managed `tabula-runtime start` child.

Use WSS when a runtime process dials into a kernel over the network. This is the
production remote-runtime path. Add mTLS when the network boundary is not fully
trusted or when operators want certificate identity in addition to bearer tokens.

Use SSH when the kernel should reach an operator-controlled host through the
system `ssh` binary. This inherits `~/.ssh/config`, ssh-agent, ProxyJump,
known_hosts, ControlMaster, and hardware-key behavior from OpenSSH.

## Security Layers

Remote execution is protected in layers:

1. Transport encryption: WSS/TLS or SSH.
2. Optional client certificate identity for WSS mTLS.
3. Runtime bearer token authorization.
4. Kernel runtime registry and tenant runtime allowlists.
5. Runtime worker tenant environment checks.

Bearer tokens are still required when mTLS is enabled. Certificates prove the
runtime identity; tokens authorize the runtime to attach.

## Operator Sequence

1. Choose backend: local, WSS, mTLS WSS, or SSH.
2. Configure `$TABULA_HOME/config/global.toml` runtime definitions.
3. Issue runtime tokens with `tabula runtime token issue --runtime-id ID`.
4. Configure tenant runtime bindings where multiple runtimes exist.
5. Start `tabula serve` directly or install it as a service.
6. Verify with `tabula status --json`.

See also:

- `docs/operating/wss-deployment.md`
- `docs/operating/ssh-deployment.md`
- `docs/operating/service-install.md`
- `docs/operating/troubleshooting.md`
