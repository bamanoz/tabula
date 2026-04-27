# Security Phase 4.7 — Shared Contract (L4 Security/CSO Gate)

> **TL;DR:** Security Phase 4.7 is a security-review gate inserted between BUILD and REFLECT for Level 4 tasks. SECURITY records evidence; SECURITY never fixes product source code. On failure (`Attempts < 3`), SECURITY routes back to BUILD via metadata in `tasks.md`. Pattern parity with QA Phase 4.5 is intentional.

## Phase Position

```
VAN → PLAN → CREATIVE → BUILD → QA → SECURITY → REFLECT → ARCHIVE
                                     ^^^^^^^^^^
                                     Phase 4.7 — this contract
```

SECURITY runs ONLY when Level == 4. For L1/L2/L3, the Phase Status block contains `- SECURITY: SKIPPED` and the SECURITY router is a transparent no-op. Future L3 expansion is possible by flipping `SKIPPED → NOT_STARTED` in VAN templates.

## Hard Constraints

1. SECURITY subagents MUST NOT edit product source files. Allowed edit scope is Memory Bank only: `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`, and `memory-bank/security/*`.
2. SECURITY subagents MUST NOT Task-call any BUILD or implementation subagent. On failure, SECURITY mutates `tasks.md` metadata so the user/router routes back to BUILD.
3. The Phase Status enum stays four-valued: `DONE`, `IN_PROGRESS`, `NOT_STARTED`, `SKIPPED`. **There is no `SECURITY: FAILED` Phase Status value.** Failure is recorded as metadata under `## Task Details`.
4. Attempt cap = 3. On the 3rd failed attempt (and any later failed attempt), SECURITY emits a hard-stop and refuses to re-open BUILD.
5. SECURITY subagents MUST close every PTY session before returning (PTY is denied at this layer; this rule guards against future expansion).

## Failure Metadata Contract

On SECURITY failure with `Attempts after this run < 3`, the SECURITY subagent atomically updates `memory-bank/tasks.md`:

- Phase Status: `- BUILD: NOT_STARTED` AND `- SECURITY: NOT_STARTED`.
- Under `## Task Details` (upsert in this exact line shape):
  - `- SECURITY Last Verdict: FAILED`
  - `- SECURITY Attempts: N`
  - `- SECURITY Last Report: memory-bank/security/security-[task-id].md`

On SECURITY failure with `Attempts after this run >= 3`, the SECURITY subagent MUST NOT set BUILD re-entry state. It writes/upserts the same failure metadata and the SECURITY report hard-stop note, leaves Phase Status in a non-reentry state, and returns `Next phase: HARD_STOP`.

On SECURITY success or skip:
- Phase Status: `- SECURITY: DONE`.
- Under `## Task Details`:
  - `- SECURITY Last Verdict: PASSED` (or `SKIPPED`)
  - `- SECURITY Attempts: N`
  - `- SECURITY Last Report: memory-bank/security/security-[task-id].md`

## BUILD Re-entry Rule (mechanical)

BUILD router allows BUILD when: `BUILD: NOT_STARTED` AND (`SECURITY: NOT_STARTED` OR `SECURITY: SKIPPED` OR `SECURITY` line absent-legacy). If `SECURITY Last Verdict: FAILED` AND `SECURITY Attempts >= 3` → hard-stop.

## `0-ultrawork` Step 7 Narrow Exception

After a `4-7-security` call, if all of the following are true, classify as a SUCCESSFUL SECURITY-failure-re-entry (dispatch BUILD next), NOT a regressed phase:

- target phase `SECURITY` is `NOT_STARTED`
- `SECURITY Last Verdict: FAILED`
- `SECURITY Attempts < 3`
- `BUILD: NOT_STARTED`

## Canonical Artifact Paths

- Report: `memory-bank/security/security-[task-id].md` (single canonical file per task; updated across attempts)
- Evidence: `memory-bank/security/artifacts/[task-id]/attempt-[N]/...` (dependency audit logs, grep transcripts, dependency lockfile snapshots, configuration diffs)

## Verdict Vocabulary (artifact-level, NOT Phase Status)

- `PASSED` — all checklist items resolved to `OK` (warnings allowed)
- `FAILED` — at least one `BLOCKING` finding
- `SKIPPED` — degraded environment forced all checklist dimensions to be skipped (see degraded-tool semantics below)

## Audit Checklist (authoritative scope)

The L4 SECURITY subagent MUST cover:

1. **AuthN/AuthZ surface**: introduced or modified authentication / authorization surfaces.
2. **Secrets/credentials handling**: storage, logging, env exposure, hard-coded keys.
3. **Input validation / injection vectors**: SQL, command, prompt-injection, path traversal.
4. **Untrusted-data boundaries**: user input flowing into eval/exec/render paths.
5. **Dependency review** for new packages added in BUILD: CVE check via `npm audit` / `pip-audit` if present, otherwise documented as degraded.
6. **Logging & PII leakage**: secrets/PII written to logs or telemetry.
7. **Configuration drift**: default-allow flags, debug endpoints, exposed admin routes.

Each checklist item gets a verdict: `OK` / `WARNING` / `BLOCKING`. Any `BLOCKING` ⇒ overall verdict `FAILED`. Only warnings ⇒ `PASSED` (with warnings noted).

## Degraded-Tool Semantics

If a checklist dimension cannot be evaluated (no `npm audit` available, no test fixtures, no PTY for live probes), record `Degraded: <reason>` and verdict `WARNING` for that item. REFLECT MUST surface degraded dimensions.

If ALL dimensions are degraded (rare), overall verdict = `SKIPPED`.

## REFLECT/ARCHIVE Consumption

- `mb-reflect-l4.md` MUST read `memory-bank/security/security-[task-id].md` when present and include a "Security Review" subsection that explicitly addresses any degraded checks and any unresolved warnings.
- `mb-archive-l4.md` MUST link the SECURITY report and summarize verdict + attempt count.

## CREATIVE Threat-Modeling Carve-In

L4 CREATIVE rubric review SHOULD include a security/threat dimension when the task touches authn/authz, secrets, untrusted input, or cross-tenant boundaries. This stays inside CREATIVE; it does NOT create a separate phase.

## Category Routing

| Level | SECURITY Subagent |
|---|---|
| L4 (any Category) | `4-7-security-l4` |
| L1 / L2 / L3 | n/a — `SECURITY: SKIPPED` |

See `security-phase-l4.md` for the L4 audit matrix.
