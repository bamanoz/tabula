# Security Review: skill-plugin-architecture
Date: 2026-04-27
Attempt: 1 / 3
Category: deep
Security Agent: 4-7-security-l4
Verdict: PASSED

## Scope
- BUILD changes audited: kernel cleanup and PluginRuntime (`internal/kernel/**`, `cmd/tabula/main.go`), reference plugin/SDK (`examples/plugin-hello/**`, `examples/plugin-sdk-python/**`), distro plugin/bundle install support (`tools/tabula-distro/src/tabula_distro/**`), docs updates, and related tests.
- New dependencies introduced: `github.com/BurntSushi/toml v1.5.0` in `go.mod`/`go.sum`; no new npm lockfile dependency; no new pinned Python dependency.
- Canonical evidence directory: `memory-bank/security/artifacts/skill-plugin-architecture/attempt-1/`

## Checklist
| # | Item | Verdict | Notes / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | WARNING | No auth bypass found. Plugin boot is local-config driven; register handshake validates protocol version and plugin id; plugin tool calls still pass through `before_tool_call`. Warning: unauthenticated `/internal/snapshot/plugins` exposes local diagnostics if server is network-exposed; spawn-token/MaxChildren invariant is intentionally dead-code-kept pending subagent plugin migration. Evidence: `auth-review.md`. |
| 2 | Secrets | OK | No hard-coded secrets/API keys/passwords/bearer tokens found. Grep hits were generated spawn-token code/tests; invalid-token logging does not log token values. Evidence: `grep.log`. |
| 3 | Input validation | OK | `plugin.toml` validates id/version/runtime and rejects absolute/parent-traversal entry paths; runtime command is allowlisted to `python3`/`node`; `bundle.toml` components reject abs/`..`; reference child spawn uses argv list and clamps duration. No raw SQL/eval/Function injection paths found. |
| 4 | Untrusted-data boundaries | OK | User tool input is forwarded to plugin over NDJSON, not evaluated in kernel. Plugin results are relayed as tool outputs; hook replies map to existing actions. Malformed plugin stdout is capped/restart-thresholded. |
| 5 | Dependency review | WARNING | `npm audit --json` degraded with `ENOLOCK`; `pip-audit` unavailable. New Go TOML dependency manually reviewed as manifest-parser-only. Evidence: `audit.log`. |
| 6 | Logging / PII | WARNING | No direct secret logging found. Plugin stderr and boot stderr are logged and can leak secrets if plugin/boot authors print them; plugin structured log fields are dropped by `plugin_tools.go`, limiting PII exposure. Evidence: `grep.log`. |
| 7 | Configuration drift | WARNING | WebSocket origin default remains local-only. Warning: legacy `cmd/tabula/kernel.tools.json`/`filterKernelTools` still advertise old builtin names, but runtime dispatch rejects them unless registered as skill/plugin tools. |

## Findings
### Blocking
- None.

### Warning
- **Unauthenticated plugin diagnostics endpoint**: `/internal/snapshot/plugins` exposes plugin ids, PID, restart count, last error, tools, and subscriptions. This follows existing local diagnostics patterns but should not be exposed to untrusted networks.
- **Degraded dependency audit tooling**: `npm audit` cannot run without lockfile; `pip-audit` unavailable. No npm/Python deps were added, but release CI should include appropriate audit tooling.
- **Plugin/boot stderr logging**: local plugin/boot code can leak secrets if it prints them to stderr. Authoring docs and future redaction should address this.
- **Stale legacy builtin metadata**: embedded legacy `shell_exec`/`process_*` tool metadata remains in `cmd/tabula/kernel.tools.json` and tests despite removal of runtime dispatch. This is not a live dispatch bypass but should be cleaned up to avoid accidental advertisement confusion.

### Degraded checks
- Dependency review / npm: degraded because no npm lockfile exists (`ENOLOCK`).
- Dependency review / Python: degraded because `pip-audit` is not installed.

## Re-entry State
- BUILD re-opened: no
- Attempts after this run: 1

## Next Phase
- REFLECT
