# QA Phase 4.5 — Deep Category Checklist

Deep = visual matrix + backend matrix + integration boundary check.

## Required Checks

- Full visual matrix (see `qa-phase-visual.md`).
- Full backend matrix (see `qa-phase-backend.md`).
- **Integration boundary**: trigger a UI action and verify the corresponding backend-side effect (log entry, DB row, persisted state, API echoed back to UI).

## Evidence (per attempt)

Combined under `memory-bank/qa/artifacts/[task-id]/attempt-[N]/`:

- Visual: screenshots, console.log, network dump.
- Backend: smoke.log, probe.log, server.log.
- Integration: `integration.md` documenting the cause→effect chain (UI step, expected backend signal, observed evidence path).

## Pass / Fail Gate

| Severity | Trigger | Effect |
|---|---|---|
| Blocking | Any blocking from visual OR backend, OR broken integration boundary (UI action did not produce the expected backend effect) | Verdict = `FAILED` |
| Warning | Union of visual and backend warnings | Logged, non-blocking |

## Degraded Environment Path

- Visual-only degradation: mark visual checks `SKIPPED`, continue backend + integration. Verdict = `PASSED` with warnings if backend+integration pass; `FAILED` otherwise.
- Backend-only degradation: cannot verify integration; mark integration `SKIPPED`. Verdict = `SKIPPED` (cannot prove integration works).
- Full degradation (bash AND playwright unavailable): Verdict = `SKIPPED`.

## Canonical Artifact Schema

Same canonical schema as `qa-phase-visual.md`, with `Category: deep` and `QA Agent: 4-5-qa-l3-deep`. Add three explicit evidence subsections:

```markdown
## Evidence
### Visual
- [as in visual category]

### Backend
- [as in backend category]

### Integration Boundary
- UI action: [step]
- Expected backend effect: [signal]
- Observed evidence: [artifact path / log line]
- Result: [VERIFIED | NOT_VERIFIED]
```
