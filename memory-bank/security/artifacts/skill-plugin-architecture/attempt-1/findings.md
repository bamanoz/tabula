# Findings — skill-plugin-architecture / Attempt 1

Date: 2026-04-27

## Checklist verdicts

| # | Item | Verdict | Rationale / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | WARNING | No auth bypass or plugin privilege escalation found. Plugin boot is local-config driven and register handshake validates protocol/plugin id. Warning: unauthenticated `/internal/snapshot/plugins` exposes plugin diagnostics like existing local endpoints; spawn-token invariant is intentionally deferred/dead-code-kept. See `auth-review.md`. |
| 2 | Secrets/credentials | OK | Grep found no hard-coded API keys/passwords/bearer tokens. Token hits are generated spawn-token code/tests; invalid token log omits token value. See `grep.log`. |
| 3 | Input validation / injection | OK | `plugin.toml` runtime is allowlisted (`python`, `node`), `entry` must be relative and non-`..`, `bundle.toml` components reject abs/`..`, plugin child spawn example uses argv list and clamps seconds. No SQL/eval/Function use in audited delta. |
| 4 | Untrusted-data boundaries | OK | User tool input reaches plugin subprocess over NDJSON, not eval/exec in kernel. Plugin results are relayed as tool output strings/JSON. Hook replies map to existing actions; unknown actions warn and default pass semantics. |
| 5 | Dependency review | WARNING | New Go dependency `github.com/BurntSushi/toml v1.5.0` added. `npm audit` degraded due no lockfile; `pip-audit` unavailable. No npm/Python pinned deps added. See `audit.log`. |
| 6 | Logging / PII | WARNING | No secret logging found. Plugin stderr is forwarded line-by-line and boot script stderr is logged; plugin-authored or boot-authored sensitive output could leak if authors print it. `plugin_tools.go` drops plugin log fields, reducing PII exposure. See `grep.log`. |
| 7 | Configuration drift | WARNING | WebSocket origin default remains local-only. Warning: embedded `cmd/tabula/kernel.tools.json` and `filterKernelTools` tests still describe legacy builtin tools even though runtime dispatch rejects them; this is a stale advertisement/config-cleanup concern rather than a live dispatch bypass. |

## Blocking findings

- None.

## Warnings

- **Unauthenticated plugin diagnostics endpoint** (`cmd/tabula/main.go::/internal/snapshot/plugins`): follows existing local endpoint pattern, but exposes plugin process metadata if the kernel HTTP server is reachable by untrusted clients. Suggested mitigation: document locality assumption and/or add auth/bind restrictions in a future hardening task.
- **Degraded dependency audits**: `npm audit` cannot run without a lockfile; `pip-audit` is not installed. Suggested mitigation: add Go/Python dependency audit tooling in CI for releases that ship plugin SDKs.
- **Plugin stderr/boot stderr log surface**: plugin/boot-authored secrets could leak if written to stderr. Suggested mitigation: authoring docs should explicitly prohibit secrets in stderr/logs; future runtime may redact common secret-shaped values.
- **Stale legacy builtin metadata**: `cmd/tabula/kernel.tools.json` and `filterKernelTools` still carry `shell_exec`/`process_*` metadata, but `internal/kernel/protocol.go::DefaultKernelTools` is empty and `ToolService` no longer dispatches those names unless provided as skill/plugin tools. Suggested mitigation: cleanup in a follow-up BUILD task if the embedded file is no longer needed.

## Degraded checks

- `npm audit --json`: no lockfile (`ENOLOCK`).
- `pip-audit --format=json`: command unavailable.

## Overall verdict

PASSED with warnings. No `BLOCKING` security finding was identified.
