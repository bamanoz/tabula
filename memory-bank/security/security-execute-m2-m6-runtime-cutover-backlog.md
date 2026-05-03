# Security Review: execute-m2-m6-runtime-cutover-backlog
Date: 2026-05-04
Attempt: 2 / 3
Category: deep
Security Agent: 4-7-security-l4
Verdict: PASSED

## Scope
- BUILD changes audited: `git diff --stat 3c65cc15253d0db5fb5e63b1248951b521716e64` covering 50 changed files across runtime auth/attach, managed local runtime supervision, runtime protocol/pool/worker routing, kernel runtime dispatch/snapshots, packaging, installed-layout smoke, plus the SECURITY re-entry fixes for redacted plugin log fields and sanitized runtime diagnostics.
- New dependencies introduced: none.

## Checklist
| # | Item | Verdict | Notes / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK | Local runtime attach still requires authenticated `hello` → `hello_ack` before registration; token validation remains constant-time; internal snapshot endpoints are loopback-only and ignore forwarded-host spoofing. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/auth-review.md`, `internal/kernel/runtime_attach.go`, `internal/runtime/auth/token.go`, `cmd/tabula/main.go`, `cmd/tabula/main_test.go`. |
| 2 | Secrets | OK | Token issuance still uses `0700` run-dir and `0600` token-file permissions; token-bearing protocol errors are sanitized; detached runtime errors collapse to a coarse fixed string; structured plugin log fields are no longer emitted by the kernel. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/grep.log`, `internal/runtime/auth/token.go`, `internal/runtime/conn/conn.go`, `internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/snapshot_test.go`. |
| 3 | Input validation | OK | Managed runtime and worker spawn paths use explicit argv via `exec.Command` / `exec.CommandContext`; no shell interpolation was added; runtime/worker frames validate runtime ids, hook actions, send channels, and log levels. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/grep.log`, `cmd/tabula/local_runtime.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `internal/runtime/wire/types.go`, `internal/runtime/worker/wire/types.go`. |
| 4 | Untrusted-data boundaries | WARNING | Worker-controlled crash text still traverses the authenticated local runtime wire in `LifecycleNotice.Message`, but the audited kernel consumer now canonicalizes lifecycle/catalog diagnostics before any status/log publication, removing the prior externally visible leak path. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/findings.md`, `cmd/tabula-runtime/pool/pool.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/snapshot.go`, `internal/kernel/runtime_async_test.go`. |
| 5 | Dependency review | OK | No dependency manifests or lockfiles changed, so no new package CVE surface was introduced in this BUILD scope. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/scope.md`, `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/audit.log`. |
| 6 | Logging / PII | OK | Kernel runtime logging now preserves only the plugin log message plus `fields_redacted=true`; runtime snapshot diagnostics and detached `last_error` remain coarse and regression-tested against secret-bearing input. Evidence: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/findings.md`, `internal/kernel/runtime_async.go:71-99`, `internal/kernel/runtime_async_test.go:14-101`, `internal/kernel/runtime_registry.go:263-327`, `internal/kernel/snapshot_test.go:134-169`. |
| 7 | Configuration drift | OK | Runtime config remains strict (`token_file`, unix socket URL, plugin dirs only); runtime reload now fails closed if the local runtime is unattached; internal diagnostics endpoints remain loopback-only. Evidence: `cmd/tabula-runtime/config/config.go`, `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/auth-review.md`. |

## Findings
### Blocking
- None.

### Warning
- Worker-controlled crash text still exists on the authenticated local runtime wire via `LifecycleNotice.Message` in `cmd/tabula-runtime/pool/pool.go`, but the current kernel consumer canonicalizes that input before it reaches logs or snapshots.
- Suggested mitigation: keep lifecycle diagnostics canonicalized at every consumer boundary, or later add an allowlisted redaction policy before forwarding runtime crash text.

### Degraded checks
- None.

## Re-entry State
- BUILD re-opened: no
- Attempts after this run: 2

## Next Phase
- REFLECT
