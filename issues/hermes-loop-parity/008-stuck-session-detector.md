---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: HITL
---

# Detect stuck sessions across restarts

## What to build

Design and implement a generic stuck-session detector that notices when the same
session repeatedly survives process/gateway restarts with an active turn or
active tool calls. After a threshold, Tabula should stop auto-resuming the loop
and surface a recoverable suspended/diagnostic state.

This may require an ADR because it affects kernel session lifecycle semantics
and persisted runtime state.

## Acceptance criteria

- [ ] Add or update an ADR before changing persisted session lifecycle behavior.
- [ ] Kernel/runtime records enough restart/liveness data to detect repeated
      active-session restarts without distro-specific assumptions.
- [ ] Detector emits a structured status/event when a session crosses the stuck
      threshold.
- [ ] Stuck sessions are not silently resumed into the same loop forever.
- [ ] Users can recover by cancelling, resetting, or explicitly resuming the
      session.
- [ ] Tests simulate repeated restarts with active tool calls/turns.
- [ ] After implementation, compare the completed behavior against Hermes'
      stuck-loop detector tests and add follow-up work for any missing parity.

## Blocked by

- `006-turn-exit-diagnostics.md`
