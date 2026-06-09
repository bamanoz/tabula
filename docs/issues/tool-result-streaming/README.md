# Tool Result Streaming And Artifact Hook Backlog

Backlog for replacing kernel-owned semantic artifacting with streamed
`tool.result` transport, a kernel `before_tool_result` rewrite hook, and a
distro-installed artifacting plugin.

The immediate driver is the current runtime API frame limit:

- `internal/runtime/codec/codec.go` sets `MaxFrameBytes = 1 << 20`

That limit is much smaller than modern model context windows and already too
small for some real tool outputs. The first rollout should make large
`tool.result` delivery safe without forcing the kernel to decide that large
results must become artifacts.

## Scope

- Stream large tool results across the Runtime API transport between runtime host
  and kernel.
- Spool streamed results in the kernel with explicit timeout and cleanup rules.
- Add a modifying `before_tool_result` hook that can rewrite a completed tool
  result before the driver sees it.
- Move semantic artifacting into a bundle/plugin installed by the distro.
- Keep final driver-facing `tool.result` bounded and explicit.
- Cover stalled, interrupted, and invalid streams on both server and client
  paths.

## Out of Scope For First Rollout

- Streaming arbitrary large user messages all the way into provider HTTP
  requests.
- Treating provider context windows as transport limits.
- Replacing compaction, file inputs, or artifact references as the main strategy
  for very large user-provided context.

Notes:

- Provider context windows already range into the high `100k+` token range and
  up to `1M` tokens for some model families, but that does not remove the need
  for bounded internal transport.
- The immediate pressure is large single `tool.result` payloads, not whole
  session replay or normal conversation history.

## Ground Rules

- Keep kernel behavior generic. Artifacting policy belongs in a bundle and the
  distro.
- The runtime and kernel may stream and spool large results, but they should not
  silently decide product policy for how the model sees them.
- If a large result is still undeliverable after `before_tool_result`, fail
  explicitly instead of silently truncating it.
- Every streamed tool result must end in exactly one terminal state:
  `completed`, `failed`, `cancelled`, `timed_out`, or `protocol_error`.
- Startup and timeout cleanup must prevent orphaned kernel spools.
- Browser and driver state must not keep infinite pending tool results.

## Priority Order

1. `001-runtime-tool-result-stream-transport.md` - completed
2. `002-kernel-tool-result-spool-lifecycle.md` - completed
3. `003-before-tool-result-hook-contract.md` - completed
4. `004-plugin-owned-tool-result-artifacting.md` - completed
5. `005-driver-and-gateway-tool-result-stream-handling.md` - completed

## Dependency Map

| Issue | Blocked by | Notes |
|---|---|---|
| 001 | None | Introduces the transport primitive for large results. |
| 002 | 001 | Adds spool lifecycle, timeouts, and terminal cleanup. |
| 003 | 002 | Hook contract depends on a stable spool/source abstraction. |
| 004 | 003 | Artifacting plugin depends on the new result hook. |
| 005 | 002, 003 | Client behavior depends on stable stream and terminal semantics. |

## Rollout Strategy

- Land transport and lifecycle first with explicit too-large failure behavior.
- Add the hook contract next, then move artifacting into a plugin.
- Expose bounded live stream UX only after terminal semantics and cleanup rules
  are stable.
- Verify every slice with focused unit tests before running installed or live
  checks.
