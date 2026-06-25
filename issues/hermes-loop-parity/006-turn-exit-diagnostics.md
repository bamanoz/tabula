---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Emit structured turn exit diagnostics

## What to build

Emit structured diagnostics when a turn exits abnormally or suspiciously. The
driver should record why the turn ended and, when useful, surface a concise
user-visible explanation instead of leaving the user with a blank response,
short fragment, or silent stop after tool activity.

## Acceptance criteria

- [ ] Driver emits a structured `turn.exit` ledger event with reason, retryable,
      partial, provider/model, retry counts, last tool, and response length.
- [ ] Provider retry exhaustion, tool infrastructure retry exhaustion, context
      compaction failure, empty final response, and suspicious short fragments
      receive distinct reason codes.
- [ ] Gateway-visible events include enough data to explain the stop without
      scraping logs.
- [ ] Healthy text responses do not receive noisy diagnostics.
- [ ] Tests cover empty response, short partial fragment, and pending-tool stop.
- [ ] After implementation, compare the completed behavior against Hermes'
      turn finalizer diagnostics and add follow-up work for any missing parity.

## Blocked by

- `005-turn-retry-state.md`
