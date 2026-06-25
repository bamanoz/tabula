---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Centralize per-turn retry and recovery state

## What to build

Introduce a `TurnRetryState` equivalent for Tabula's driver loop so recovery
guards are explicit, testable, and logged. The object should track provider
retry attempts, context-overflow retry, partial-stream recovery, history repair,
tool-input repair, and steer interruption for the current turn.

## Acceptance criteria

- [ ] Main driver loop uses a per-turn retry state object instead of scattered
      local booleans/counters.
- [ ] State covers provider retry attempts, context overflow retry,
      partial-stream recovery, history repairs, tool-input repairs, and steer
      interrupt consumption.
- [ ] Existing provider retry and context compaction behavior remains
      behaviorally equivalent.
- [ ] State is visible in ledger/debug logs at turn end.
- [ ] Tests verify retry limits and one-shot recovery guards.
- [ ] After implementation, compare the completed behavior against Hermes'
      `TurnRetryState` and add follow-up work for any missing parity.

## Blocked by

- `001-tool-input-repair.md`
- `004-partial-tool-stream-recovery.md`
