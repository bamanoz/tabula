# ADR 0008 — Generic per-turn lifecycle hooks

Date: 2026-07-03
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

`session_start` and `before_prompt_build` cover join-time prompt shaping, while
`before_message` can rewrite user text. That still leaves a gap for bundle-owned
memory systems and other turn-scoped domain logic:

- they need a per-turn prompt-context seam that does not rewrite the user's text
- they need a terminal turn signal that fires after the driver has produced a
  turn result

Without those seams, memory bundles either depend on manual tools, driver-local
policy, or product-specific lifecycle code outside the kernel.

## Decision

Add generic lifecycle hook events:

- `before_turn`: a domain modifying hook that may return additive `context`
  fragments for the current turn only
- `after_turn`: an observability hook that fires on terminal turn events such as
  `turn.done` and terminal `error`
- `before_compaction`: a synchronous domain hook that runs when a driver emits
  `compaction.start`, before the event is forwarded to the rest of the session

`before_turn` does not rewrite user text. The kernel stores returned context in
`meta.kernel.turn_context` on the routed message so the active driver can apply
it transiently to the provider request without persisting it as session init
context.

`after_turn` is fire-and-forget. Its payload carries session, tenant,
turn-correlation id, terminal message facts, and the original message metadata so
bundle-owned lifecycle workers can read turn artifacts from session history or
ledger files.

`before_compaction` is waited on so bundle-owned pre-compaction hooks can flush
session artifacts before driver compaction proceeds. The kernel ignores returned
modifications and blocks because this hook is not an authorization boundary.

## Consequences

- Bundle-owned memory implementations can stay generic and kernel-agnostic.
- Drivers keep responsibility for how transient turn context reaches provider
  APIs, but the kernel owns when that context is requested.
- `before_message` remains the text-rewrite seam; `before_turn` is for prompt
  context only.
- `after_turn` is fail-open and must not be used as an execution authorization
  boundary.
- `before_compaction` gives memory bundles a generic equivalent to editor
  pre-compact hooks without coupling the kernel to a concrete memory system.
