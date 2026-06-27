---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Enforce driver-side tool result budgets

## What to build

Add a driver-side safety net for oversized tool results before they are added to
provider history. This complements the existing `tool-result-store` plugin:
the hook remains the preferred storage path, but the driver should still guard
against provider-history blowups when the hook is absent, disabled, or misses a
result shape.

## Acceptance criteria

- [ ] Driver applies a per-result and aggregate per-turn character/token budget
      before provider history receives tool results.
- [ ] Oversized results are replaced with bounded previews plus artifact/source
      metadata when available.
- [ ] Existing `artifact` and `truncated` fields continue to flow into provider
      history and UI history.
- [ ] The guard is configurable from tenant/global driver config, with safe
      defaults.
- [ ] Tests cover large plain-text results and existing artifacted results.
- [ ] After implementation, compare the completed behavior against Hermes'
      tool-result budget/artifact handling and add follow-up work for any
      missing parity.

## Blocked by

- `003-provider-history-preflight.md`
