# Session and turn runtime affinity state

Type: Kernel / Routing

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/app-runtime-affinity/README.md`

## Problem

Tool calls are not always emitted by the same websocket client that originally
sent the user message. In a shared session, an app-originated message may lead
to another session participant (for example an agent client) emitting the later
`tool.call`.

That means client-local affinity alone is not enough. The kernel needs a
normalized session/turn-level place to remember which runtime should be
preferred for the work that originated from a particular app.

## What to build

Capture runtime affinity from app-originated session input and retain it in a
normalized kernel-owned form that later dispatch steps can read.

Behavior:

- When an app client with runtime affinity sends a user message or steer, the
  kernel records the preferred runtime for that session/turn.
- Routed messages should carry kernel-owned metadata for the normalized
  preferred runtime so downstream participants can observe it.
- Session state should expose the current preferred runtime where needed for
  later tool dispatch.
- Define precedence rules for conflicting inputs, with the default rule being:
  latest app-originated turn affinity wins for that turn.

## Acceptance criteria

- [x] Kernel extracts runtime affinity from authenticated client metadata.
- [x] User-message / turn-steer paths stamp normalized preferred-runtime
      metadata for downstream routing.
- [x] Session state can answer “what runtime is currently preferred for this
      turn/session?”
- [x] Conflicting app inputs have deterministic precedence rules.
- [x] Existing sessions without runtime affinity continue to work unchanged.

## Files

- Edit: `internal/kernel/session.go`
- Edit: `internal/kernel/message_router.go`
- Edit: `internal/kernel/metadata.go`
- Edit: `internal/kernel/helpers.go`
- Edit: `internal/kernel/kernel_test.go`
- Edit: `internal/kernel/session_test.go`

## Verify

```bash
go test ./internal/kernel -race -count=1
```

## Risk

Medium.

This is the first behavior-bearing issue in the track. The biggest risk is
accidentally creating sticky affinity rules that survive longer than intended.

## Notes

- Keep the first version simple: session/turn-scoped preference is enough.
- Do not introduce per-tool device routing policy here.
- Routed session messages now expose the normalized preference in
  `meta.kernel.preferred_runtime_id`.
- Precedence is deterministic for queued turn inputs: a new app-originated
  affinity does not overwrite the active turn immediately, but it becomes the
  preferred runtime when that queued turn is dispatched.
