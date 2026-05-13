# WSS Deployment

WSS lets a remote `tabula-runtime` dial into a kernel over `ws://` or `wss://`.
Use `wss://` for production.

## Kernel Endpoint

Boot config may expose a runtime endpoint:

```json
{
  "runtime_endpoints": {
    "wss": {
      "enabled": true,
      "listen": "0.0.0.0:7777",
      "path": "/runtime",
      "origins": ["https://runtime.example.com"],
      "cert_file": "/etc/tabula/kernel.crt",
      "key_file": "/etc/tabula/kernel.key",
      "client_ca": "/etc/tabula/runtime-ca.crt",
      "client_auth": "request"
    }
  }
}
```

Fields:

- `enabled`: turns on the runtime WebSocket listener.
- `listen`: TCP address. Empty means reuse the main kernel listener.
- `path`: runtime endpoint path, default `/runtime`.
- `origins`: accepted WebSocket Origin values. Empty allows all origins.
- `cert_file` and `key_file`: server TLS certificate and key.
- `client_ca`: CA used to verify runtime client certs.
- `client_auth`: `none`, `request`, or `require`.

## Runtime Config

Remote runtime config uses `[[kernel]]` in `runtime.toml`:

```toml
plugin_dirs = ["/opt/tabula/plugins"]
skill_dirs = ["/opt/tabula/skills"]

[[kernel]]
id = "prod-kernel"
url = "wss://kernel.example.com:7777/runtime"
token_file = "/opt/tabula/run/runtime-token"
tenants = ["*"]
ca_file = "/etc/tabula/kernel-ca.crt"
cert_file = "/etc/tabula/runtime.crt"
key_file = "/etc/tabula/runtime.key"
tls_insecure_skip_verify = false
```

Fields:

- `url`: `ws://` or `wss://` runtime endpoint.
- `token_file`: bearer token read before every reconnect attempt.
- `tenants`: tenant allowlist served by this runtime; `[*]` means all tenants.
- `ca_file`: CA for the kernel server cert.
- `cert_file` and `key_file`: runtime client certificate and key.
- `tls_insecure_skip_verify`: test-only escape hatch for self-signed local
  experiments. Do not use in production.

## Token Rotation

Issue a token on the kernel host:

```sh
tabula runtime token issue --runtime-id laptop --json
```

Write the returned token to the runtime host's `token_file` with mode `0600`.
Restart or let the runtime reconnect. The runtime reads `token_file` before every
connect attempt.

Revoke old tokens:

```sh
tabula runtime token revoke --runtime-id laptop --json
```

The live kernel watches the persisted token store and detaches revoked runtimes
within one second.

## mTLS Certificate Flow

Use your existing PKI or create a small private CA. The runtime client cert
Common Name must equal the runtime id in `global.toml`; otherwise the kernel
rejects the upgrade with `unknown_runtime` or unauthorized identity mismatch.

For `client_auth = "request"`, cert-less clients are allowed through the TLS
layer but still require bearer tokens. For `client_auth = "require"`, cert-less
clients fail before the WebSocket upgrade.

## Load Balancers

Each runtime maintains one long-lived connection to one kernel. If a load
balancer fronts multiple kernels, route a runtime consistently to the same
kernel or run separate runtime definitions per kernel. There is no per-call
stickiness requirement after the WebSocket is established.
