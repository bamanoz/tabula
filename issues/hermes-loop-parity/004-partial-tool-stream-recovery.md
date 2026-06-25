---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Recover from partial streamed tool calls

## What to build

Handle provider stream failures that occur after partial tool-call deltas have
arrived but before a complete valid tool call can be sent. The driver should not
poison provider history with malformed partial tool calls. It should preserve a
safe partial marker and ask the model to continue without retrying the same huge
call verbatim.

## Acceptance criteria

- [ ] OpenAI Chat streaming detects partial tool-call state when a stream raises.
- [ ] OpenAI Responses streaming detects partial function-call argument state
      when a stream raises.
- [ ] Malformed partial tool calls are not persisted as normal completed
      assistant tool calls.
- [ ] Driver injects a bounded continuation prompt that names dropped partial
      tools when known.
- [ ] User-visible stream state closes cleanly before recovery starts.
- [ ] Tests simulate stream failure after partial tool name/arguments deltas.
- [ ] After implementation, compare the completed behavior against Hermes'
      partial-stream recovery paths and add follow-up work for any missing
      parity.

## Blocked by

- `001-tool-input-repair.md`
- `003-provider-history-preflight.md`
