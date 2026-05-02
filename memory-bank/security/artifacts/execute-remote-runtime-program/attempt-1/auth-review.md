# AuthN/AuthZ Review

## Surfaces touched

- `internal/runtime/auth/token.go`: token issuance, in-memory validation store, sanitized `HelloAck` rejection.
- `internal/runtime/conn/conn.go`: authenticated server handshake helper and post-handshake frame serving.
- `internal/kernel/runtime_attach.go`: kernel accepts runtime Hello, registers authenticated runtime in read model, detaches on close.
- `cmd/tabula/main.go`: `tabula serve` issues a local runtime token, opens a unix runtime listener, and serves authenticated runtime connections.
- `internal/runtime/transport/unixsock/unixsock.go`: local AF_UNIX websocket listener with parent dir `0700` and socket `0600` where supported.
- `cmd/tabula/status.go` and `cmd/tabula/main.go`: loopback-only internal runtime snapshot endpoint consumed by status CLI.

## Findings

- Bearer token auth exists before runtime attachment (`Authenticator.HelloAck` validates runtime ID and token; rejects with canonical `unauthorized`).
- Token generation uses 32 random bytes with base64url and `rtk_` prefix; file and run dir modes are tightened to `0600` and `0700`.
- Rejected auth logs runtime ID only and returns sanitized error text.
- Internal runtime snapshot endpoint is guarded by loopback remote address and Host/listener-host checks.
- M2 intentionally has no tenant runtime whitelist yet; this is planned for M4. Worker pool keys warm workers by `(kernel_id, tenant_id, target_id)`, reducing cross-tenant worker reuse risk in the current M2 scope.

## Verdict

OK for M1/M2 scope. Later M4/M6 tenant whitelist, hashed token persistence, mTLS, and revoke semantics remain future program work, not blockers for this BUILD delta.
