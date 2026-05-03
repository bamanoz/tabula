# Findings — execute-m2-m6-runtime-cutover-backlog / attempt 1

## Checklist verdicts

1. AuthN/AuthZ — OK
2. Secrets / credentials — OK
3. Input validation / injection — OK
4. Untrusted-data boundaries — WARNING
5. Dependency review — OK
6. Logging / PII — BLOCKING
7. Configuration drift — OK

## Blocking

- `internal/kernel/runtime_async.go:96-97` logs runtime plugin `fields` JSON verbatim with `Logger.Debug(...)`.
- The runtime and worker protocols both allow optional arbitrary `fields` payloads (`internal/runtime/wire/types.go:499-502`, `internal/runtime/worker/wire/types.go:172-175`).
- Worker log validation enforces level/message but does not validate or redact `fields` (`internal/runtime/worker/wire/types.go:436-448`).
- Result: migrated plugins can emit secrets or PII in structured log fields and the kernel will persist them to logs with no redaction gate.

Suggested mitigation:

- Drop structured `fields` at the kernel boundary until a redaction policy exists, or
- enforce a strict allowlist / redaction pass before logging plugin fields.

## Warning

- Worker/runtime diagnostic strings are still trusted-by-contract rather than sanitized at the kernel boundary. `cmd/tabula-runtime/pool/pool.go:445-452` copies worker-controlled crash text into `LifecycleNotice.Message`, and `internal/kernel/runtime_registry.go:188-194` plus `internal/kernel/snapshot.go:303-314` expose that diagnostic through runtime status snapshots.

Suggested mitigation:

- Apply the same redaction / length-limiting policy to lifecycle diagnostics before exposing them in status or logs.

## Degraded checks

- None.
