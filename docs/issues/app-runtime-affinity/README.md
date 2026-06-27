# App Runtime Affinity Backlog

Backlog for making a remote kernel prefer the runtime that is co-installed with
the app instance that initiated work, without introducing a separate device-id
control plane or a new persistence layer.

The target model is:

- each runtime instance has a stable local `runtime_id`
- the local app SDK can discover that `runtime_id`
- the app tells the kernel which nearby runtime it belongs to
- the kernel routes session work to that runtime when it is attached and allowed
- shared sessions still live in the kernel; routing becomes app-aware rather than
  tenant-default-only

## Ground rules

- Treat app→runtime affinity as a routing hint backed by kernel validation, not
  as a new end-user configuration surface.
- Reuse the existing runtime auth model (`runtime_id` + token / optional mTLS).
- Keep the first rollout focused on remote-kernel deployments with attached
  runtimes; do not block on a new persistence abstraction.
- Prefer session/turn-scoped affinity over global tenant-wide defaults when both
  are available.
- Fallback behavior must stay safe and explicit: if a hinted runtime is missing
  or not allowed, the kernel must fall back cleanly and surface why.

## Priority Order

1. `001-runtime-instance-metadata.md`
2. `002-app-sdk-runtime-affinity-handshake.md`
3. `003-session-turn-runtime-affinity-state.md`
4. `004-preferred-runtime-dispatch-and-fallback.md`

## Dependency Map

| Issue | Blocked by | Notes |
|---|---|---|
| 001 | None | Establishes stable local runtime identity discovery. |
| 002 | 001 | App SDK handshake depends on the metadata contract. |
| 003 | 002 | Kernel must capture and retain affinity from app-originated work. |
| 004 | 003 | Dispatch changes depend on normalized session/turn affinity state. |

## Current status

| Issue | Status | Notes |
|---|---|---|
| 001 | Completed | Runtime instance metadata now boots from `$TABULA_HOME/run/runtime-instance.json` and is reused across starts. |
| 002 | Completed | `sdk/app` now loads `$TABULA_HOME/run/runtime-instance.json` and injects `tabula.runtime_id` into hello metadata when present. |
| 003 | Completed | Kernel now stamps normalized preferred-runtime metadata on routed user/steer messages and applies deterministic queued-turn precedence. |
| 004 | Completed | Runtime dispatch now prefers session affinity, falls back safely, and logs explicit preferred-runtime fallback reasons. |

## Rollout strategy

- Land each issue as one testable PR.
- Prove the metadata and handshake contract first before changing dispatch.
- Keep fallback-to-default-runtime behavior until preferred-runtime routing is
  fully wired and verified.
- Verify with a two-runtime / one-tenant setup where each runtime exposes a
  distinct tool set, then with overlapping tools to confirm deterministic
  preference and fallback logs.
- Do not mark the whole track done until 002/003/004 are all closed in the
  states above, not just landed partially in code.

## Out of scope

- A new shared persistence layer.
- User-visible device management UI.
- Cross-user authorization redesign.
- Scheduling work across multiple runtimes for the same tool name.
