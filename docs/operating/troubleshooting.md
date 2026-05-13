# Troubleshooting

## `runtime_unavailable`

The selected runtime disconnected, was revoked, or failed before completing a
tool call.

Check:

```sh
tabula status --json
```

Look at runtime `attached`, `last_error`, and target lifecycle diagnostics.

## `tenant_forbidden`

The tenant is not allowed to use the selected runtime. Check tenant config:

```toml
[tenant]
allowed_runtimes = ["local", "laptop"]
default_runtime = "local"
```

## mTLS Handshake Failures

Common causes:

- runtime cert not signed by `client_ca`
- `client_auth = "require"` but runtime has no cert
- client cert Common Name does not match runtime id
- runtime does not trust kernel server certificate CA

Use `openssl s_client` or runtime logs to inspect TLS errors.

## Token Failures

Tokens are issued on the kernel host:

```sh
tabula runtime token issue --runtime-id ID --json
tabula runtime token list --json
tabula runtime token revoke --runtime-id ID --json
```

The token is printed only once at issue time. The on-disk store contains salted
hashes, not plaintext tokens.

## SSH Failures

Run the equivalent SSH command manually:

```sh
ssh -v host tabula-runtime stdio
```

If key auth or known_hosts fails manually, fix SSH first. Tabula deliberately
delegates SSH configuration to OpenSSH.

## Service Failures

macOS logs:

```text
$TABULA_HOME/logs/kernel.log
```

Linux logs:

```sh
journalctl --user -u tabula-kernel.service
```

Check service state:

```sh
tabula status --json
```
