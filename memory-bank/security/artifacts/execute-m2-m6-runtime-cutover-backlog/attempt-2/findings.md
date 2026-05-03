# Findings — execute-m2-m6-runtime-cutover-backlog / attempt 2

## Checklist verdicts

1. AuthN/AuthZ — OK
2. Secrets / credentials — OK
3. Input validation / injection — OK
4. Untrusted-data boundaries — WARNING
5. Dependency review — OK
6. Logging / PII — OK
7. Configuration drift — OK

## Blocking

- None.

## Warning

- `cmd/tabula-runtime/pool/pool.go:445-451` still forwards worker-controlled crash text inside `LifecycleNotice.Message` on the authenticated local runtime wire.
- The current kernel consumer now collapses lifecycle/catalog diagnostics to canonical state strings before publishing status or logs (`internal/kernel/runtime_registry.go:166-189,311-327`, `internal/kernel/snapshot.go:303-315`, `internal/kernel/runtime_async_test.go:41-101`, `internal/kernel/snapshot_test.go:134-169`), so the exposure is no longer directly user-visible in the audited paths.
- Residual risk: a future consumer that logs or republishes raw lifecycle frames without the current canonicalization could reintroduce diagnostic leakage.

Suggested follow-up:

- Either keep all kernel/runtime consumers on canonicalized lifecycle diagnostics, or later add an allowlisted redaction policy before forwarding worker crash text at the runtime layer.

## Degraded checks

- None.
