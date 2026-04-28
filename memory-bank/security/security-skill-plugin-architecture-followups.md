# Security Review: skill-plugin-architecture-followups
Date: 2026-04-27
Attempt: 1 / 3
Category: deep
Security Agent: 4-7-security-l4
Verdict: PASSED

## Scope

- BUILD changes audited: current reopened repo-local safe hardening / regression-preservation scope and evidence-gated cleanup boundaries from the working tree. Scope includes `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `cmd/tabula/kernel.tools.json`, plugin runtime/protocol validation files under `internal/kernel/`, distro installer validation in `tools/tabula-distro/`, active docs, temporary SDK protocol/path files, and Memory Bank/evidence artifacts.
- New dependencies introduced: none. Dependency manifest diff was empty.
- Evidence directory: `memory-bank/security/artifacts/skill-plugin-architecture-followups/attempt-1/`
- Re-audit note: this pass rechecked the canonical artifacts and scoped product files after the reopened BUILD Agents 1–4 confirmed no evidence-safe bridge deletion remained; no product source files were edited by SECURITY.

## Checklist

| # | Item | Verdict | Notes / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK | `/internal/snapshot/plugins` now uses a local-only guard requiring loopback `RemoteAddr` and loopback/local `Host`; forwarded headers are not trusted. Plugin register/update failures do not install unsafe registry/dispatch/hook state. Evidence: `auth-review.md`, `scope.md`. |
| 2 | Secrets | OK | Scoped secret grep found only README placeholder/secret-store docs, a negative spawn-token doc note, and the generic protocol `token` field. No hard-coded real secrets were found. Evidence: `grep.log`. |
| 3 | Input validation | OK | Boot skill descriptors, distro `SKILL.md` advertised tools, plugin `register`, and plugin `update_tools` catalogs validate required names/execs and reject invalid shapes before mutation. No raw SQL/eval/user-input command construction was added by BUILD. Evidence: `findings.md`, direct reads of changed files. |
| 4 | Untrusted-data boundaries | OK | Malformed `event_reply` actions no longer default to pass; malformed/ambiguous `tool_result` is rejected and pending calls are released. Diagnostic locality rejects forged forwarded headers. Evidence: `auth-review.md`, `grep.log`. |
| 5 | Dependency review | WARNING | No BUILD dependency changes. `.opencode` `npm audit --json` reports existing moderate findings via `@opencode-ai/plugin` / `effect` / `uuid`; `pip-audit` is unavailable. Evidence: `audit.log`. |
| 6 | Logging / PII | WARNING | No direct secret logging was introduced. Existing trusted-boundary plugin log/stderr and boot stderr/command logging are unredacted and can expose sensitive data if upstream code emits it. Evidence: `grep.log`. |
| 7 | Configuration drift | OK | No remote diagnostics opt-in or default-allow flag was added. Wildcard binds do not authorize public Host headers for the plugin snapshot endpoint, and `TABULA_ALLOWED_ORIGINS` remains WebSocket-only. Evidence: `auth-review.md`. |

## Findings

### Blocking

- None.

### Warning

- Existing `.opencode` npm audit findings: 3 moderate vulnerabilities through `@opencode-ai/plugin` → `effect` → `uuid`. Not introduced by BUILD and not blocking this SECURITY gate, but should be tracked separately.
- Python dependency audit degraded because `pip-audit` is unavailable in this environment. No Python dependency manifest changed in BUILD.
- Plugin/boot logging remains a trusted boundary: plugin log messages, plugin stderr, boot stderr, and command strings are forwarded without redaction. No new direct secret logging was found.
- External `tabula-bundles` rows remain `unknown/blocker`; SECURITY accepts this only because BUILD preserved the D1.11(b) and Phase 6 compatibility bridges and did not claim deletion completion.

### Degraded checks

- Dependency review / Python: `pip-audit --format=json` unavailable (`zsh:1: command not found: pip-audit`).

## Re-entry State

- BUILD re-opened: no
- Attempts after this run: 1

## Next Phase

- REFLECT
