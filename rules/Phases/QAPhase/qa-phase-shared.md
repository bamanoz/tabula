# QA Phase 4.5 — Shared Contract (L3 Runtime Validation Gate)

> **TL;DR:** QA Phase 4.5 is a runtime validation gate inserted between BUILD and REFLECT for Level 3 tasks. QA records evidence; QA never fixes product source code. On failure, QA routes back to BUILD via metadata in `tasks.md`.

## Phase Position

```
VAN → PLAN → CREATIVE → BUILD → QA → REFLECT → ARCHIVE
                                ^^^^
                                Phase 4.5 — this contract
```

QA runs ONLY when Level == 3. For L1/L2/L4, the Phase Status block contains `- QA: SKIPPED` and the QA router is a transparent no-op.

## Hard Constraints

1. QA subagents MUST NOT edit product source files. Allowed edit scope is Memory Bank only: `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`, and `memory-bank/qa/*`.
2. QA subagents MUST NOT Task-call any BUILD or implementation subagent. On failure, QA mutates `tasks.md` metadata so the user/router routes back to BUILD.
3. The Phase Status enum stays four-valued: `DONE`, `IN_PROGRESS`, `NOT_STARTED`, `SKIPPED`. **There is no `QA: FAILED` Phase Status value.** Failure is recorded as metadata under `## Task Details`.
4. Attempt cap = 3. On the 3rd failed attempt (and any later failed attempt), QA emits a hard-stop and refuses to re-open BUILD.
5. QA subagents MUST close every PTY session before returning.

## Failure Metadata Contract

On QA failure with `QA Attempts after this run < 3`, the QA subagent atomically updates `memory-bank/tasks.md`:

- Phase Status: `- BUILD: NOT_STARTED` AND `- QA: NOT_STARTED`.
- Under `## Task Details` (upsert in this exact line shape):
  - `- QA Last Verdict: FAILED`
  - `- QA Attempts: N`
  - `- QA Last Report: memory-bank/qa/qa-[task-id].md`

On QA failure with `QA Attempts after this run >= 3`, the QA subagent MUST NOT set BUILD re-entry state. It writes/upserts the same failure metadata and the QA report hard-stop note, leaves Phase Status in a non-reentry state, and returns `Next phase: HARD_STOP`.

On QA success or skip:
- Phase Status: `- QA: DONE`.
- Under `## Task Details`:
  - `- QA Last Verdict: PASSED` (or `SKIPPED`)
  - `- QA Attempts: N`
  - `- QA Last Report: memory-bank/qa/qa-[task-id].md`

## BUILD Re-entry Rule (mechanical)

BUILD router allows BUILD when: `BUILD: NOT_STARTED` AND (`QA: NOT_STARTED` OR `QA: SKIPPED` OR `QA` line absent-legacy). If `QA Last Verdict: FAILED` AND `QA Attempts >= 3` → hard-stop.

## `0-ultrawork` Step 7 Narrow Exception

After a `4-5-qa` call, if all of the following are true, classify as a SUCCESSFUL QA-failure-re-entry (dispatch BUILD next), NOT a regressed phase:

- target phase `QA` is `NOT_STARTED`
- `QA Last Verdict: FAILED`
- `QA Attempts < 3`
- `BUILD: NOT_STARTED`

## Canonical Artifact Path

- Report: `memory-bank/qa/qa-[task-id].md` (single canonical file per task; updated across attempts)
- Evidence: `memory-bank/qa/artifacts/[task-id]/attempt-[N]/...` (screenshots, logs, console/network dumps)

## Verdict Vocabulary (artifact-level, NOT Phase Status)

- `PASSED` — all required checks for the Category passed
- `FAILED` — at least one blocking finding
- `SKIPPED` — degraded environment forced category checks to be skipped (see CR5 in `memory-bank/creative/creative-qa-phase-4-5.md`)

## REFLECT/ARCHIVE Consumption

- `mb-reflect-l3.md` MUST read `memory-bank/qa/qa-[task-id].md` when present and include a "Runtime Validation" subsection that explicitly addresses any degraded checks.
- `mb-archive-l3.md` MUST link the QA report and summarize verdict + attempt count.

## Category Routing

| Category | QA Subagent |
|---|---|
| `visual` | `4-5-qa-l3-visual` |
| `backend` | `4-5-qa-l3-backend` |
| `deep` | `4-5-qa-l3-deep` |
| `quick` | `4-5-qa-l3-backend` (minimal smoke; rare for L3) |

See `qa-phase-visual.md`, `qa-phase-backend.md`, `qa-phase-deep.md` for category-specific check matrices.
