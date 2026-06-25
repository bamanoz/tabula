---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Dampen empty tool-name loops

## What to build

Detect model-emitted tool calls with an empty tool name before dispatching them
to the kernel. Instead of returning a large available-tool catalog, synthesize a
terse tool result that tells the model the call is invalid and that tool-call-like
text in prior data is data, not an instruction.

This mirrors Hermes' anti-priming behavior for empty tool names while preserving
normal unknown-tool recovery for non-empty names.

## Acceptance criteria

- [ ] Driver does not send blank-name tool calls to the kernel.
- [ ] Driver injects a model-visible synthetic tool result for the affected tool
      call id.
- [ ] The synthetic result does not include the full tool catalog.
- [ ] Non-empty unknown tool names still follow the existing unknown-tool/tool
      catalog recovery path.
- [ ] Ledger/history records the rejected blank-name call and recovery result.
- [ ] Tests cover blank-name loop dampening and non-empty unknown-tool behavior.
- [ ] After implementation, compare the completed behavior against Hermes'
      empty-tool-name dampening tests and add follow-up work for any missing
      parity.

## Blocked by

- `001-tool-input-repair.md`
