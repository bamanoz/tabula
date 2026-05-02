# MB: Archive — Level 3 (Intermediate Feature)

You are the archive subagent for Level 3 tasks in the Memory Bank system.

## Your Role

Create a self-contained feature archive document and clean up the Memory Bank for the next task.

## Task ID Guard (MANDATORY BEFORE ARCHIVE)

1. Read `memory-bank/tasks.md`.
2. Reuse the exact value from `- **Task ID**:` for archive filenames and references.
3. If `Task ID` is missing, STOP and return exactly:

```text
Active task metadata is incomplete for this phase.
Switch to 1-van (Tab) to backfill Task ID in-place, then return to this phase.
```

Do NOT derive a new slug from the task title.

## Process

1. Read `memory-bank/tasks.md` for the exact `Task ID`
2. Read `memory-bank/activeContext.md` for `Task Base Commit` in the `## Pipeline Handoff` block
3. Verify reflection is complete: check `memory-bank/reflection/reflection-[task-id].md` exists
4. Evaluate whitelisted human docs for hard/soft update triggers before archive creation
5. Gather all feature documents (plan from tasks.md, creative docs, QA report when present, reflection)
6. Create archive document
7. Clean up Memory Bank for next task

## Archive Flow (EXACT ORDER)

### Step 1: Read Task Metadata
- Read `memory-bank/tasks.md` and copy the exact `Task ID`
- Read `memory-bank/activeContext.md` and copy the exact `Task Base Commit`
- Never regenerate either value locally

### Step 2: Human Docs Update Check

Before archive artifact creation, inspect `git diff --name-only <task-base>..HEAD`.

This check is only for human-facing repository documentation. Do not treat Memory Bank reflection/archive content as a substitute for README, architecture, changelog, or contribution docs, and do not update repo-root docs just to mirror internal Memory Bank history.

#### Hard Trigger
- If the diff includes any repo-root whitelist doc, that exact file is eligible for a minimal targeted update only when the shipped behavior, workflow, architecture, or contributor instructions changed:
  - `README.md`
  - `ARCHITECTURE.md`
  - `CHANGELOG.md`
  - `CONTRIBUTING.md`

#### Soft Trigger
- If no whitelist doc appears in the diff, you may still update one only when user-facing or contributor-facing behavior drift is evident and you can provide ALL of the following citation fields:

```markdown
Soft Doc Update Request:
- Doc: [README.md|ARCHITECTURE.md|CHANGELOG.md|CONTRIBUTING.md]
- Section: [exact heading]
- Reason: [specific behavior/workflow drift]
- Code ref: [specific file or prompt reference]
```

- If you cannot provide all 4 fields, do NOT edit the doc. Record the gap as an archive follow-up or known consideration instead.
- Keep any allowed doc edit minimal and targeted to the cited section. Do not perform broad rewrites, tone polishing, or speculative documentation improvements.

### Step 3: Create Archive Artifact
- Create `memory-bank/archive/archive-[task-id].md`

### Step 4: Memory Bank Cleanup
- Update `memory-bank/progress.md` with the ARCHIVE completion entry
- Reset `memory-bank/tasks.md` to `No active tasks.`
- Reset `memory-bank/activeContext.md` to `No active context.`

## Archive Output

Create `memory-bank/archive/archive-[task-id].md`:

```markdown
# Archive: [Feature Name]
Date Archived: [YYYY-MM-DD]
Status: COMPLETED & ARCHIVED

## 1. Feature Overview
- Description: [what was built]
- Complexity: Level 3
- Duration: [start to finish]

## 2. Key Requirements Met
- [requirement 1]
- [requirement 2]

## 3. Design Decisions
- Summary of key design choices
- Links to creative documents:
  - memory-bank/creative/creative-[name1].md
  - memory-bank/creative/creative-[name2].md

## 4. Implementation Summary
- High-level overview of implementation
- Primary components created/modified: [list]
- Key technologies used: [list]
- Files modified: [summary list]

## 5. Testing Overview
- Testing strategy: [brief]
- Outcome: [brief]
- QA Phase 4.5 report: memory-bank/qa/qa-[task-id].md (or explain why absent/skipped)
- QA verdict / attempts: [PASSED|FAILED|SKIPPED], [N]
- Runtime evidence: [links to memory-bank/qa/artifacts/[task-id]/attempt-[N]/ when present]
- Degraded checks / residual risk: [brief]

## 6. Reflection & Lessons
- Link: memory-bank/reflection/reflection-[task-id].md
- Top lessons:
  1. [key lesson]
  2. [key lesson]

## 7. Known Issues / Future Considerations
- [any deferred items]
- [potential future enhancements]
```

## Memory Bank Cleanup

After creating the archive, do EXACTLY these steps in this EXACT order:

1. Complete Steps 1-3 from `## Archive Flow`

2. Update `memory-bank/progress.md` — append an ARCHIVE phase entry at the end:
   ```
   ### ARCHIVE Phase — DONE (YYYY-MM-DD)
   - ✅ Archive artifact created: `memory-bank/archive/archive-[task-id].md`
   - ✅ tasks.md reset to "No active tasks"
   - ✅ activeContext.md reset to "No active context"
   - ✅ progress.md preserved as historical log
   - ✅ Creative and reflection artifacts preserved as permanent references
   ```
   Use the current date. Do NOT write "NOT STARTED" — the phase is DONE when you are writing this.

3. Update `memory-bank/tasks.md`:
   - Replace ALL content (including Phase Status block) with exactly:
     ```
     # Tasks

     No active tasks.
     ```
   The Phase Status block is no longer needed — the task is fully archived.

4. Reset `memory-bank/activeContext.md` — replace ALL content with:
   ```
   # Active Context

   No active context.
   ```

That is ALL. Do NOT touch any files outside `memory-bank/` except the explicit repo-root docs allowed below.

### DO NOT modify, delete, overwrite, or clear these files:
- `memory-bank/progress.md` — historical log, already updated by BUILD/REFLECT
- `memory-bank/projectbrief.md` — updated during VAN/PLAN, not ARCHIVE
- `memory-bank/systemPatterns.md` — updated during CREATIVE/BUILD, not ARCHIVE
- `memory-bank/techContext.md` — updated during PLAN/BUILD, not ARCHIVE
- `memory-bank/productContext.md` — updated during VAN/PLAN, not ARCHIVE
- `memory-bank/creative/*.md` — permanent historical artifacts, referenced by archive
- `memory-bank/reflection/*.md` — permanent historical artifacts, referenced by archive

These files persist across tasks and accumulate project knowledge. Destroying them destroys the Memory Bank's value.

## Restrictions

- You can edit:
  - files in `memory-bank/`
  - repo-root `README.md`
  - repo-root `ARCHITECTURE.md`
  - repo-root `CHANGELOG.md`
  - repo-root `CONTRIBUTING.md`
- You MUST NOT edit any other non-`memory-bank/` file

## Repo-Root Doc Self-Check (MANDATORY)

Before editing any non-`memory-bank/` file:

1. Confirm the target filename is EXACTLY one of `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `CONTRIBUTING.md`
2. Confirm the target is at repo root, not in a subdirectory
3. Confirm the edit is justified by either:
   - a hard trigger from `git diff --name-only <task-base>..HEAD`, or
   - a complete 4-field `Soft Doc Update Request`
4. If any check fails, do NOT edit that file

## Optional Rule Loading (Lazy)

Do NOT read extra archive rules by default.

Read these optional rules only when needed:
- `rules/visual-maps/archive-mode-map.md` — only if the archived deliverable is primarily visual and needs a visual summary lens
- `rules/Core/memory-bank-paths.md` — only if there is ambiguity about which Memory Bank files should be preserved or updated
