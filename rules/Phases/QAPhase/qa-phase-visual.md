# QA Phase 4.5 — Visual Category Checklist

## Required Checks

- Browser load of the primary route exercised by BUILD via `playwright` MCP.
- Walk one representative user flow (click primary CTA, verify state change).
- Capture two viewports: desktop **1280x800** and mobile **390x844**.
- Capture console messages (errors + warnings).
- Capture network request summary (failed same-origin requests).

## Evidence (per attempt)

Saved under `memory-bank/qa/artifacts/[task-id]/attempt-[N]/`:

- `desktop-1280.png`, `mobile-390.png` (and additional viewports as needed)
- `console.log` (raw console output)
- `network.json` or `network.log` (failed same-origin requests)
- `notes.md` (optional context)

## Pass / Fail Gate

| Severity | Examples | Effect |
|---|---|---|
| Blocking | Uncaught JS exceptions; failed same-origin app/API requests; main container fails to render | Verdict = `FAILED` |
| Warning | 3rd-party/analytics errors; slow-but-successful requests; deprecation warnings | Logged in artifact, non-blocking |

## Degraded Environment Path

If `playwright` MCP is unavailable:

1. Set artifact field `Environment: playwright_unavailable=true`.
2. Mark all browser-dependent checks as `SKIPPED` in the artifact.
3. If ALL mandatory visual checks were degraded → Verdict = `SKIPPED` (Phase Status still moves to `QA: DONE`).
4. If only some checks degraded → Verdict = `PASSED` with explicit warnings.
5. `mb-reflect-l3.md` MUST surface degraded checks in the Runtime Validation section.

## Canonical Artifact Schema

The artifact at `memory-bank/qa/qa-[task-id].md` follows this exact structure:

```markdown
# QA Report: [task-id]

## Metadata
- Task ID: [task-id]
- Task: [task title]
- Level: 3
- Category: visual
- Attempt: [N]
- Date: [YYYY-MM-DD HH:mm]
- QA Agent: 4-5-qa-l3-visual
- Environment: [OS, playwright availability, tool context]
- Verdict: [PASSED|FAILED|SKIPPED]
- Phase Status Action: [QA DONE | BUILD re-entry | QA SKIPPED]

## Scope Under Test
- Requirements checked:
- Files/features exercised:
- Out of scope:

## Pre-flight
- BUILD status confirmed:
- Server/app startup method:
- Test data or fixture notes:

## Evidence
- Commands run:
- Browser routes/viewports:
- Screenshots: [paths]
- Console messages: [summary + raw log path]
- Network/API observations:
- Logs/artifact paths:

## Findings
- Blocking failures:
- Warnings/non-blocking noise:
- Degraded checks and rationale:

## Attempt History
- Attempt 1: [verdict, date, summary]
- Attempt 2: ...

## Re-entry Instructions (only if Verdict=FAILED)
- Root symptom:
- Repro steps:
- Suggested BUILD focus:
- QA Attempts after this run: [N]
```
