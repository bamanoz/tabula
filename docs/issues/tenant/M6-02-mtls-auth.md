# M6-02 — mTLS auth (opt-in second auth mode)

Status: done
Phase: M6
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/security, phase/m6

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M6)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§7)

## What to build

Per Q4 phasing: bearer token (M2-05) is the universal default;
mTLS is an opt-in second mode for production deployments that
want certificate-based identity. Both modes coexist on the
same listener: kernel attempts mTLS first; if the client
presents no cert, falls back to bearer token validation.

Components:

- TLS listener (kernel side):
  ```toml
  [runtime.endpoints.wss]
  listen        = ":7777"
  path          = "/runtime"
  cert_file     = "/etc/tabula/server.crt"
  key_file      = "/etc/tabula/server.key"
  client_ca     = "/etc/tabula/clients.ca"   # mTLS opt-in
  client_auth   = "request"                  # request | require | none
  ```
  - `client_auth = "request"` (default when `client_ca` set):
    accepts both clients with and without certs; bearer token
    still required for cert-less.
  - `client_auth = "require"`: rejects connections without
    valid client cert; bearer token still required as second
    factor (defense in depth).
  - `client_auth = "none"`: bearer token only (M2 behavior).
- Cert validation:
  - Standard TLS chain validation against `client_ca`.
  - Pinning by Subject CN: extract CN, look up
    `(runtime_id, allowed_tenants[])` in kernel's runtime
    registry. CN unknown → reject.
  - Cert expiration handled by stdlib TLS layer.
- Runtime side:
  ```toml
  [[kernel]]
  id        = "prod"
  url       = "wss://kernel.example.com/runtime"
  ca_file   = "/etc/tabula/server.ca"           # verifies kernel
  cert_file = "/etc/tabula/runtime.crt"         # presents to kernel
  key_file  = "/etc/tabula/runtime.key"
  token_file = "${TABULA_HOME}/run/runtime-token"
  tls_insecure_skip_verify = false
  ```
- Token lookup move (groundwork from M2-05):
  - Kernel-side token store gets a hashed-on-disk
    implementation: `$TABULA_HOME/state/runtime-tokens.json`
    with `argon2id` hashes, runtime_id → hashed_token,
    metadata (created_at, expires_at, last_seen_at).
  - In-memory cache populated at startup; reload on file change.
- New CLI primitives `tabula runtime token`:
  - `tabula runtime token issue --runtime-id <id> [--tenants
    a,b,c] [--expires-in 90d]` → prints token to stdout
    (operator copies to runtime config). JSON output via
    `--json`.
  - `tabula runtime token list [--json]` → metadata only,
    never the plaintext token.
  - `tabula runtime token revoke --runtime-id <id>` → marks
    token revoked in store, kicks any active connection.
- New CLI primitives `tabula runtime cert` (thin wrappers around
  `openssl`/`go crypto/x509` to ease ops):
  - `tabula runtime cert sign --runtime-id <id> --ca <ca-key>`
    → produces a runtime client cert. Optional convenience;
    operators are also free to use their own PKI.
  - Skip if review prefers "BYO PKI"; document the openssl
    one-liner in operator docs instead.

## Acceptance criteria

- [x] mTLS handshake succeeds with valid cert + valid token.
- [x] mTLS handshake fails cleanly with invalid cert chain →
      TLS error before WS upgrade.
- [x] CN-not-in-registry → upgrade rejected with structured
      `unknown_runtime` error.
- [x] `client_auth = "request"` allows both cert-presenting
      and cert-less clients (cert-less still need bearer
      token).
- [x] `client_auth = "require"` rejects cert-less.
- [x] Token store: hashed on disk; restart preserves issued
      tokens; revoke kicks connection within 1s.
- [x] `tabula runtime token issue/list/revoke` ship with
      `--json`.
- [x] M2-05 file-based token (`runtime-token` flat file) still
      works for the local backend (managed-child case) — that
      flow uses a special `runtime_id = "local"` with
      auto-issued token at kernel startup.
- [x] Race-clean.

## Landed in this slice

- Extended runtime-side `[[kernel]]` config with:
  - `ca_file`
  - `cert_file`
  - `key_file`
  - existing `tls_insecure_skip_verify`
- Extended `internal/runtime/transport/wss/` with:
  - TLS client config loading (`ca_file`, `cert_file`, `key_file`)
  - TLS server config loading (`cert_file`, `key_file`, `client_ca`, `client_auth`)
  - `client_auth = request | require | none`
  - pre-upgrade client-cert validation hook with structured JSON rejection
- Extended kernel boot/runtime endpoint config with:
  - `runtime_endpoints.wss.cert_file`
  - `runtime_endpoints.wss.key_file`
  - `runtime_endpoints.wss.client_ca`
  - `runtime_endpoints.wss.client_auth`
- Added runtime cert identity binding:
  - peer cert CN is carried into auth context
  - when a client cert is present, auth requires `CN == hello.runtime_id`
  - unknown client-cert CN is rejected before websocket upgrade with
    `error.code = "unknown_runtime"`
- Added persisted runtime token storage:
  - `$TABULA_HOME/state/runtime-tokens.json`
  - salted token hashes only; plaintext tokens are printed once at issue time
  - metadata includes `created_at`, `expires_at`, `last_seen_at`, `revoked_at`
  - live auth reloads the token store on validate, so restart preserves issued
    tokens
- Added `tabula runtime token` CLI:
  - `issue --runtime-id ID [--expires-in DURATION] [--json]`
  - `list [--json]`
  - `revoke --runtime-id ID [--json]`
- `tabula serve` now chains the managed local in-memory token store with the
  persisted remote token store, preserving the flat local `runtime-token` file
  flow.
- Added a 1s revocation watcher that detaches active runtimes after their token
  is revoked on disk.

## Implementation notes

- This implementation uses salted SHA-256 with constant-time compare instead of
  Argon2id to avoid adding a new dependency to this minimal core repo. The file
  format records the algorithm prefix (`sha256:`) so a future stronger KDF can
  be introduced as an explicit migration if needed.
- This slice validates CN against configured runtime ids. Tenant/runtime binding
  enforcement still happens through the existing runtime registry and dispatch
  path rather than a certificate-side allow-tenants table.
- `tabula runtime cert ...` was treated as optional BYO-PKI convenience per the
  issue text and was not added; operators can use their existing PKI.

## Validation evidence

- `go test ./internal/runtime/transport/wss ./internal/runtime/auth ./internal/runtime/host/config ./internal/runtime/host/dialer ./cmd/tabula`
- `go test -race ./internal/runtime/transport/wss ./internal/runtime/auth ./internal/runtime/host/dialer`
- `go test ./cmd/tabula -run 'TestWriteLocalRuntimeConfig|TestRuntimeToken|TestWatchRuntimeToken'`
- `go test -race ./internal/runtime/auth ./cmd/tabula ./internal/runtime/transport/wss ./internal/runtime/host/dialer`

## Blocked by

- M6-01 (WSS transport exists to layer TLS over)
- M2-05 (token store interface to extend)

## Notes

- Per Q4d, refresh-on-restart still applies: a runtime that
  reconnects after kernel restart re-reads its token file and
  re-attempts. Token rotation by operator follows the same
  flow.
- Hashed token storage uses `argon2id` because we already
  ship a Go crypto stack — verify before merging that no
  smaller dependency exists. `bcrypt` or `scrypt` are
  acceptable alternatives if simpler.
- mTLS does NOT replace bearer token; it complements it.
  Defense in depth: cert proves identity, token proves
  authorization.
