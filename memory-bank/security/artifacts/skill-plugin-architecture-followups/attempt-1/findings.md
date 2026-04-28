# Findings — skill-plugin-architecture-followups — Attempt 1

Date: 2026-04-27

## Checklist verdicts

| # | Item | Verdict | Rationale |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK | `/internal/snapshot/plugins` is guarded to loopback caller + loopback/local host, forwarded headers are ignored, and plugin registration/update failures do not mutate auth-relevant dispatch/hook state. |
| 2 | Secrets / credentials | OK | Scoped grep found docs placeholders and generic protocol fields only; no hard-coded real API keys, passwords, bearer tokens, or spawn-token helper exposure was found in changed files. |
| 3 | Input validation / injection | OK | BUILD added descriptor/catalog validation for boot skills, distro `SKILL.md`, plugin `register`, and `update_tools`; no new raw SQL/eval/user-controlled shell construction was introduced. Trusted manifest `exec` commands remain the intended skill execution model. |
| 4 | Untrusted-data boundaries | OK | Malformed plugin `event_reply` / `tool_result` payloads reject or release pending calls without synthesizing permissive outcomes; diagnostic endpoint locality does not trust forwarded headers. |
| 5 | Dependency review | WARNING | No dependency manifests changed, but `.opencode` `npm audit --json` reports 3 existing moderate findings via `@opencode-ai/plugin` → `effect` → `uuid`; `pip-audit` is unavailable in this environment. |
| 6 | Logging / PII leakage | WARNING | No direct secret logging was introduced, but plugin `log` messages, plugin stderr, boot stderr, and boot/spawn command logging remain unredacted trusted-boundary outputs; plugins/operators must not emit secrets there. |
| 7 | Configuration drift | OK | No remote diagnostics opt-in was added; wildcard binds do not allow public host headers for plugin snapshots; `TABULA_ALLOWED_ORIGINS` remains WebSocket-only and is not reused for unauthenticated HTTP diagnostics. |

## Blocking findings

- None.

## Warnings

1. Existing `.opencode` npm audit warning: moderate `uuid` advisory through `effect` / `@opencode-ai/plugin`. This is not introduced by BUILD and is outside product runtime changes, but should be tracked separately.
2. `pip-audit` is not installed; Python dependency auditing is degraded. No Python dependency manifests changed in BUILD.
3. Trusted plugin/boot logging surfaces forward plugin log messages, plugin stderr, boot stderr, and command strings without redaction. No BUILD-introduced direct secret logging was found, but plugin/operator guidance should continue to treat logs as sensitive.
4. External `tabula-bundles` evidence rows remain `unknown/blocker`; this is not blocking SECURITY because BUILD preserved the corresponding compatibility bridges, but REFLECT/ARCHIVE should not claim D1.11(b) or Phase 6 deletion completion.

## Degraded checks

- Dependency review: `pip-audit` unavailable (`zsh:1: command not found: pip-audit`).
