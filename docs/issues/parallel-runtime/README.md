# Parallel Warm Runtime Backlog

Backlog for adding plugin-declared concurrent execution to warm runtime workers
without making concurrency user-configurable.

The immediate driver is `fs_grep`: one warm `fs` worker serializes all `fs_*`
calls, so expensive reads queue behind each other and hit per-call deadlines.
The goal is to let a plugin declare which tools can run in parallel and which
must stay serialized, while keeping hook behavior explicit and safe.

## Ground rules

- Concurrency policy is declared by the plugin manifest, not by user config.
- Default behavior stays exactly as today: tools are serial unless a plugin opts
  in explicitly.
- Hooks are not part of the first concurrency rollout. Keep them on the current
  warm path until tool-call concurrency is stable.
- Start with `fs` as the first opt-in plugin after the runtime and SDK layers
  are proven.
- Keep kernel behavior generic. Plugin-specific policy lives in manifests.

## Priority Order

1. `001-tool-execution-policy-metadata.md`
2. `002-concurrent-warm-worker-transport.md`
3. `003-python-plugin-sdk-concurrent-tool-calls.md`
4. `004-runtime-scheduler-for-execution-groups.md`
5. `005-fs-opt-in-and-live-verification.md`

## Dependency Map

| Issue | Blocked by | Notes |
|---|---|---|
| 001 | None | Adds manifest/runtime metadata only; no behavior change. |
| 002 | 001 | Worker transport must support multiple inflight `call_id`s. |
| 003 | 001 | Python SDK must safely execute and reply concurrently. |
| 004 | 001, 002, 003 | Scheduler depends on both runtime and SDK concurrency. |
| 005 | 004 | `fs` is the first real plugin opt-in and live proof. |

## Rollout strategy

- Each issue should land as one independently testable PR.
- Keep the rollout opt-in. No existing plugin becomes concurrent until its
  manifest says so.
- Verify runtime changes first with synthetic fixture plugins, then opt `fs`
  in, then run live checks against `gateway-web` and focused kernel/runtime
  suites.

## Out of scope

- User-facing concurrency tuning.
- Hook concurrency policy.
- Replacing warm workers globally with cold workers.
- Search-scope tuning for `fs` beyond what is required to keep tests stable.
