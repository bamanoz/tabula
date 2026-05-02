# Findings

## Checklist Verdicts

| # | Item | Verdict | Rationale |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK | Runtime attachment requires bearer-token Hello auth; unauthorized path is sanitized; internal snapshots are loopback guarded. Tenant whitelist is future M4 and not implemented yet in this M2 slice. |
| 2 | Secrets | OK | Production source has no hard-coded secret material. Token file is generated randomly and written `0600` under a `0700` run dir; logs omit token values. |
| 3 | Input validation / injection | OK | Wire frames validate op, IDs, target kind, call/tool fields, and frame size. Manifest entries require relative paths and reject `..`; runtime config rejects unsupported/stale aliases. Worker spawn uses `exec.CommandContext(ctx, bin, entry)` without shell concatenation. |
| 4 | Untrusted-data boundaries | OK | User/tool args remain JSON payloads passed to worker protocol; connection code intentionally avoids logging frame content. Status internal snapshot URL normalizes wildcard hosts to loopback. |
| 5 | Dependency review | WARNING | New Go dependency `github.com/coder/websocket v1.8.14`; no allowed Go vulnerability audit tool in the SECURITY checklist. `npm audit` for unrelated `.opencode` lockfile reports moderate plugin-environment vulnerabilities outside product delta. |
| 6 | Logging / PII | OK | Runtime auth/logging emits runtime IDs, paths, and coarse errors only. `runtime_registry` sanitizes disconnect errors as `runtime connection failed`; conn write failure path avoids logging invoke args. |
| 7 | Configuration drift | OK | Runtime config uses singular `[[kernel]]`, accepts `token_file`, rejects stale `token`, plural `[[kernels]]`, speculative `runtime_id`, and kernel-side `[[runtime]]` shape. |

## Blocking

- None.

## Warning

- Dependency review degraded for the new Go dependency because the checklist only permits npm/pip audit tooling and this repo's runtime delta is Go-module based. Manual review found `github.com/coder/websocket v1.8.14` added for Runtime API codec/transport; no product JavaScript dependency delta was introduced.

## Degraded checks

- Dependency review: no allowed Go vulnerability audit command in SECURITY protocol; `npm audit` applies only to `.opencode` local plugin environment and is not the product delta.
