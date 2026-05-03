# Auth Review — execute-m2-m6-runtime-cutover-backlog / attempt 2

## Reviewed surfaces

- Runtime API attach path in `internal/kernel/runtime_attach.go`
- Runtime token issuance/validation in `internal/runtime/auth/token.go`
- Internal diagnostics HTTP guards in `cmd/tabula/main.go`

## Findings

- Runtime attach still requires a valid authenticated `hello` before registration:
  - `Authenticator.HelloAck(...)` rejects invalid runtime/token pairs with a sanitized unauthorized error.
  - `MemoryStore.Validate(...)` uses constant-time token comparison.
  - `ServeAuthenticatedRuntime(...)` registers only accepted runtimes.
- Runtime IDs remain syntax-validated via `wire.ValidateRuntimeID(...)`.
- Local runtime credentials remain scoped to the local runtime id and token file path under `$TABULA_HOME/run/runtime-token`.
- Internal snapshot endpoints remain loopback-only via `internalDiagnosticsGuard(...)` / `isLocalInternalDiagnosticsRequest(...)`; forwarded-host spoofing is not trusted.

## Verdict

- AuthN/AuthZ: OK
