# Auth Review — execute-m2-m6-runtime-cutover-backlog

## Reviewed files

- `cmd/tabula/main.go`
- `cmd/tabula/local_runtime.go`
- `cmd/tabula-runtime/config/config.go`
- `cmd/tabula-runtime/dialer/dialer.go`
- `internal/kernel/runtime_attach.go`
- `internal/runtime/auth/token.go`

## Notes

- Local runtime attachment remains gated by `runtimeauth.Authenticator` in `internal/kernel/runtime_attach.go`; unauthenticated runtimes get `HelloAck{accepted:false}` and are not registered.
- Runtime tokens are still issued via `runtimeauth.IssueLocalTokenFile(...)` from both `tabula serve` and `tabula run`.
- Token storage remains permission-hardened in `internal/runtime/auth/token.go`: run directory `0700`, token file `0600`, constant-time token comparison, and rejection messages that do not echo token material.
- The managed runtime launcher in `cmd/tabula/local_runtime.go` uses a fixed sibling binary / PATH lookup plus explicit argv (`start --config ... --runtime-id local`); there is no shell interpolation on the local-runtime launch path.
- Runtime reload now requires an attached local runtime and no longer reactivates a kernel plugin reload fallback.

## Verdict

- AuthN/AuthZ review: OK
