# Security Model

Tabula kernel clients and remote runtimes use layered defenses. No single layer
replaces the others.

## Layers

1. Transport protection: unix socket, SSH, or WSS/TLS.
2. Optional WSS mTLS client certificate identity.
3. Runtime bearer token authorization.
4. Kernel runtime registry and tenant runtime allowlists.
5. Runtime worker tenant environment checks before execution.

## Kernel Client Token

Kernel WebSocket clients connect to `/ws` and must authenticate in their first
protocol-v4 `connection.open` command with `data.auth_token`. On startup, `tabula serve` writes a fresh
local token to `$TABULA_HOME/run/kernel-client-token` with mode `0600`, exports
it to child processes as `TABULA_KERNEL_TOKEN`, and stores only the in-memory
expected value in the running hub.

This token is distinct from Runtime API bearer tokens. Runtime tokens authorize
runtime workers to attach to the Runtime API; the kernel client token authorizes
drivers, gateways, and other bus clients to use the kernel client WebSocket.

Hook replies are also identity-bound. A `hook_reply` is accepted only from the
client or runtime hook subscriber that received that specific hook id.

## Runtime Bearer Tokens

Remote runtime tokens are issued with:

```sh
tabula runtime token issue --runtime-id ID --json
```

The token is shown once and must be placed in the runtime host token file with
mode `0600`. Kernel-side storage persists salted token hashes in
`$TABULA_HOME/state/runtime-tokens.json`.

Revocation:

```sh
tabula runtime token revoke --runtime-id ID --json
```

The live kernel detects revocation and detaches the runtime within one second.

## mTLS

mTLS adds certificate identity to WSS. It does not replace bearer tokens. If a
client certificate is present, its Common Name must match `hello.runtime_id`.
Unknown runtime certificate identities are rejected before the WebSocket upgrade.

Use `client_auth = "require"` when all remote runtimes can present certs. Use
`request` during staged rollout or mixed deployments; cert-less clients still
need valid bearer tokens.

## SSH

SSH backend security is OpenSSH security. Tabula shells out to system `ssh` and
inherits known_hosts, ssh-agent, ProxyJump, ControlMaster, and host-key policy.

## Tenant Isolation

The kernel selects a runtime using tenant runtime bindings. The runtime also
receives tenant identity and enforces tenant-scoped worker environment checks.
Workspace filesystem roots live in tenant config and are enforced by the
`workspace/fs` plugin, not by the kernel.
