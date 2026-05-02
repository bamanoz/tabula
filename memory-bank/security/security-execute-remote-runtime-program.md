# Security Review: execute-remote-runtime-program
Date: 2026-05-02
Attempt: 1 / 3
Category: deep
Security Agent: 4-7-security-l4
Verdict: PASSED

## Scope
- BUILD changes audited: M1 Runtime API contract, M2 codec/unix transport, `tabula-runtime` daemon/config/dialer/manifest/pool/bare worker policy, M2 token auth, kernel runtime attachment/read model, `tabula status`, and runtime config strictness hardening as recorded in `memory-bank/progress.md` Agents 1-16.
- Primary files / areas audited: `internal/runtime/**`, `cmd/tabula-runtime/**`, `internal/kernel/runtime_attach.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/snapshot.go`, `internal/kernel/kernel.go`, `cmd/tabula/main.go`, `cmd/tabula/status.go`, `go.mod`, `go.sum`.
- New dependencies introduced: `github.com/coder/websocket v1.8.14`.
- Evidence directory: `memory-bank/security/artifacts/execute-remote-runtime-program/attempt-1/`.

## Checklist
| # | Item | Verdict | Notes / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK | Runtime attach requires first-frame `Hello` bearer-token validation via `internal/runtime/auth` and `internal/kernel/runtime_attach.go`; rejected auth is sanitized. Internal runtime snapshots are loopback/Host guarded. Evidence: `auth-review.md`. |
| 2 | Secrets | OK | No production hard-coded secrets found. Local runtime token is randomly generated, written to `$TABULA_HOME/run/runtime-token` with `0600` under `0700`, and not logged. Evidence: `grep.log`, `internal/runtime/auth/token.go`. |
| 3 | Input validation | OK | Runtime frames validate op, IDs, target kind, required call/tool fields, and 1 MiB frame limit. Runtime config rejects unsupported aliases. Manifest entry paths must be relative and reject `..`. Worker spawn uses `exec.CommandContext(ctx, bin, entry)`, not shell concatenation. Evidence: `findings.md`. |
| 4 | Untrusted-data boundaries | OK | Invoke args stay as JSON payloads across the worker protocol; connection handling explicitly avoids logging frame/arg content; status snapshot URL normalizes wildcard bind hosts to loopback. Evidence: `findings.md`. |
| 5 | Dependency review | WARNING | Go dependency `github.com/coder/websocket v1.8.14` added. SECURITY checklist supports npm/pip audit only; no applicable product JS/Python lockfile delta. `.opencode` npm audit reported moderate vulnerabilities outside product delta. Evidence: `audit.log`. |
| 6 | Logging / PII | OK | Runtime auth/logging emits runtime IDs, paths, and coarse error values only; disconnect errors are sanitized to `runtime connection failed`; invoke args are not logged. Evidence: `grep.log`, `internal/kernel/runtime_registry.go`, `internal/runtime/conn/conn.go`. |
| 7 | Configuration drift | OK | Runtime config uses singular `[[kernel]]`, accepts `token_file`, and rejects stale `token`, plural `[[kernels]]`, speculative `runtime_id`, and kernel-side `[[runtime]]` shape. Evidence: `cmd/tabula-runtime/config/config.go`, config tests in BUILD log. |

## Findings
### Blocking
- None.

### Warning
- Dependency audit is degraded for the newly added Go module because the SECURITY protocol allows `npm audit` / `pip-audit` but not Go vulnerability tooling. Manual review identified only `github.com/coder/websocket v1.8.14` as the product dependency delta. The available `.opencode/package-lock.json` audit reports moderate vulnerabilities in the local opencode plugin environment, not in the runtime program source or dependency delta.

### Degraded checks
- Dependency review: no applicable allowed Go vulnerability audit command for `go.mod`/`go.sum`; `.opencode` npm findings are non-product-delta evidence.

## Re-entry State
- BUILD re-opened: no
- Attempts after this run: 1

## Next Phase
- REFLECT
