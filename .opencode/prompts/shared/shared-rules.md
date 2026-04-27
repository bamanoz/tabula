# SHARED RULES — Memory Bank System

These rules apply to ALL Memory Bank agents regardless of phase or level.

## Memory Bank File Locations

CRITICAL: All Memory Bank files reside within the `memory-bank/` directory at the project root.

| File | Path | Purpose |
|------|------|---------|
| Tasks | `memory-bank/tasks.md` | Active task tracking (source of truth) |
| Active Context | `memory-bank/activeContext.md` | Current focus and status |
| Progress | `memory-bank/progress.md` | Implementation status |
| Task Backlog | `memory-bank/backlog.md` | Pending task queue for multi-cycle automation |
| Project Brief | `memory-bank/projectbrief.md` | Project foundation |
| Product Context | `memory-bank/productContext.md` | Product context |
| System Patterns | `memory-bank/systemPatterns.md` | System patterns |
| Tech Context | `memory-bank/techContext.md` | Technology context |
| Style Guide | `memory-bank/style-guide.md` | Style guide |
| Creative Docs | `memory-bank/creative/creative-[name].md` | Design decision documents |
| QA Reports | `memory-bank/qa/qa-[task-id].md` | Runtime validation evidence |
| Security Reports | `memory-bank/security/security-[task-id].md` | L4 security review evidence |
| Reflection Docs | `memory-bank/reflection/reflection-[id].md` | Reflection documents |
| Reflection Second Opinion | `memory-bank/reflection/reflection-[id]-second-opinion.md` | L4 independent reflection audit (CR-B) |
| Lessons Archive | `memory-bank/archive/lessons/lessons-archive-YYYY-MM.md` | Rotated lessons (CR-C, monthly bucket) |
| Archive Docs | `memory-bank/archive/archive-[id].md` | Archived task documentation |

## File Verification

Before any file operation on Memory Bank files:
1. Verify the path starts with `memory-bank/`
2. If the file does not exist, create it with appropriate template
3. Never create Memory Bank files outside `memory-bank/` directory

## Memory Bank Creation

Memory Bank MUST be verified/created BEFORE any other operation:

1. Check if `memory-bank/` directory exists
2. If not, create the directory structure:
   - `memory-bank/`
   - `memory-bank/creative/`
   - `memory-bank/qa/`
   - `memory-bank/qa/artifacts/`
   - `memory-bank/security/`
   - `memory-bank/security/artifacts/`
   - `memory-bank/reflection/`
   - `memory-bank/archive/`
   - `memory-bank/archive/lessons/`
3. Create essential files if missing:
   - `memory-bank/tasks.md`
   - `memory-bank/activeContext.md`
   - `memory-bank/progress.md`
   - `memory-bank/projectbrief.md`

## Complexity Levels

| Level | Type | Workflow | Description |
|-------|------|----------|-------------|
| 1 | Quick Bug Fix | VAN → BUILD → REFLECT | Single component, low risk |
| 2 | Simple Enhancement | VAN → PLAN → BUILD → REFLECT | Few components, moderate scope |
| 3 | Intermediate Feature | VAN → PLAN → CREATIVE → BUILD → QA → REFLECT → ARCHIVE | Multiple components, design decisions needed |
| 4 | Complex System | VAN → PLAN → CREATIVE → BUILD → SECURITY → REFLECT → ARCHIVE | System-wide, architectural implications |

## Memory Bank Update Rules

- Update only changed sections, not entire files
- `tasks.md` is the source of truth for current task state
- Always include timestamps when updating progress
- Use differential updates: skip files that haven't changed

## Sequential Pipeline Scoped Verification

For sequential PLAN / BUILD agents, optimize token use without giving up factual verification.

Default read order:
1. `memory-bank/tasks.md`
2. `memory-bank/activeContext.md`
3. `memory-bank/progress.md` — current task, current phase log first
4. Actual project files referenced by `tasks.md`, `activeContext.md`, and recent pipeline log entries

Read other Memory Bank docs (`systemPatterns.md`, `techContext.md`, `productContext.md`, etc.) only if:
- the active task, handoff, or actual files explicitly point to them
- you hit ambiguity or contradiction that cannot be resolved from the default read set
- the work is architecture-heavy enough that those docs are genuinely needed

Do NOT re-read all `memory-bank/*.md` files or do broad project exploration by default inside sequential pipelines.
The handoff block in `activeContext.md` is a routing aid, not a substitute for reading the real files you may change or verify.

## Active Context Handoff Block

When a sequential PLAN / BUILD agent contributes, keep a compact `## Pipeline Handoff` block in `memory-bank/activeContext.md` using this shape:

```markdown
## Pipeline Handoff
- Current task: [...]
- Current phase: [...]
- Next phase: [...]
- Current focus: [...]
- Working set: [...]
- Verified files: [...]
- Open issues: [...]
- Next checks: [...]
- Optional docs: [...]
```

Keep the values short, task-scoped, and actionable. Use `none` when a field has nothing useful.
`Optional docs` should list specific Memory Bank files to read only if needed.

## Intent & Category Metadata Contract

After `VAN` is `DONE`, all intent-aware subagent prompts MUST validate task metadata before doing phase work.

- Required `Intent` values: `fix`, `enhance`, `implement`, `refactor`, `research`
- Required `Category` values for BUILD and QA prompts: `quick`, `visual`, `backend`, `deep`
- Do NOT infer or backfill missing metadata locally in PLAN / CREATIVE / BUILD / QA / REFLECT
- The ONLY sanctioned recovery path is `1-van`, which performs in-place backfill in `memory-bank/tasks.md`

If metadata is missing or invalid, stop immediately and use this recovery guidance:

```text
Active task metadata is incomplete for this phase.
Switch to 1-van (Tab) to backfill Intent/Category in-place, then return to this phase.
```

Sequential pipeline subagents must express this stop via their required `[DECISION: DECLINE]` response format instead of continuing phase work.

## Intent & Category Boundaries

- `Intent` changes phase heuristics and quality criteria only
- `Category` changes BUILD routing/model selection and QA runtime-validation routing only
- Do NOT change workflow order or mechanical router branching based on `Intent`
- Non-BUILD phases may read `Category` for context, but must NOT branch on it except the dedicated QA router (`4-5-qa`), which routes mechanically by Category

## Intent-Aware Phase Heuristics

Use the detected `Intent` to adjust the phase approach:

- **PLAN**
  - `fix` → require repro path, root-cause hypothesis, blast radius, validation path
  - `enhance` → analyze existing behavior, compatibility constraints, integration points
  - `implement` → define new artifacts, interfaces, dependencies, rollout considerations
  - `refactor` → preserve behavior, define invariants, anti-regression checks, decomposition strategy
  - `research` → prioritize hypotheses, evidence sources, experiments, decision gates, deliverables
- **CREATIVE**
  - `refactor` → focus on boundaries, invariants, migration safety, design trade-offs
  - `research` → focus on evidence-oriented options and decision support, not speculative implementation detail
  - other intents → keep design work scoped to the requested task and referenced creative docs
- **BUILD**
  - `fix` → minimal scoped change, remove root cause, avoid unrelated refactor
  - `enhance` → incremental extension with backward compatibility
  - `implement` → full happy-path integration into the existing system
  - `refactor` → improve structure while verifying behavioral equivalence
  - `research` → create only evidence artifacts, probes, instrumentation, or spike code unless the user explicitly requested product implementation
- **QA**
  - `fix` → validate root-cause elimination through a focused repro/regression path
  - `enhance` → validate the new capability without degrading existing behavior
  - `implement` → validate happy-path runtime integration and required evidence coverage
  - `refactor` → validate behavioral equivalence and integration safety
  - `research` → validate evidence/probe artifacts rather than product behavior unless implementation was explicitly requested
- **REFLECT**
  - `fix` → verify root-cause elimination and regression safety
  - `enhance` → verify new capability integrates without degrading existing behavior
  - `implement` → verify coverage of new requirements and integration completeness
  - `refactor` → verify preserved behavior, reduced complexity, and migration safety
  - `research` → verify evidence quality, usefulness of conclusions, and decision support value

## Phase Status Block (CRITICAL)

`memory-bank/tasks.md` contains a machine-readable Phase Status block that ALL routers depend on.
The block looks like this:

```
<!-- PHASE_STATUS_START -->
- VAN: DONE
- PLAN: NOT_STARTED
- CREATIVE: NOT_STARTED
- BUILD: NOT_STARTED
- QA: NOT_STARTED
- SECURITY: NOT_STARTED
- REFLECT: NOT_STARTED
- ARCHIVE: NOT_STARTED
<!-- PHASE_STATUS_END -->
```

Valid statuses: `DONE`, `IN_PROGRESS`, `NOT_STARTED`, `SKIPPED`

**When you complete your phase's work**, you MUST update this block:
1. Set YOUR phase to `DONE`
2. Set the NEXT phase to `NOT_STARTED` (if it isn't already)
3. Do NOT change statuses of other phases

**Exception for sequential pipeline agents** (`seq-plan-*`, `seq-build-*`):
Do NOT modify the Phase Status block. The router that called you (`2-plan` or `4-build`) manages phase transitions in its finalization step. Your job is to write your pipeline log entry and update task scope — not to change phase status.

Example: a PLAN subagent finishing its work changes `- PLAN: NOT_STARTED` to `- PLAN: DONE`.

If this block is missing or malformed, routers will fail. Never delete or reformat it.

### CRITICAL: Editing tasks.md

ALWAYS use the **Edit** tool (not Write) when modifying `memory-bank/tasks.md`.
The Write tool **overwrites the entire file** — if you forget to include the Phase Status block,
it will be destroyed and all routers will break.

The Edit tool replaces only the specific string you target, keeping everything else intact.
The only exception is VAN creating `tasks.md` for the first time — that uses Write.

## Your Tools — Use Them Directly

You have your own tools: Read, Write, Edit, Glob, Grep, Bash. Use them DIRECTLY for all operations you can do yourself.

- **Read/Write/Edit Memory Bank files** — you know the paths, do it yourself
- **Read specific known files** — if you know the path, read it directly
- **Delegate to `explore`** (cheap read-only model) only for searching unknown files across the codebase
  - Give `explore` a **specific** search query, not open-ended exploration
  - Good: "Find files that implement authentication middleware"
  - Bad: "Explore the entire codebase and summarize it"

The Task tool is restricted by permissions — you can only call the agents explicitly allowed for your role. All other delegations will be blocked by the system.

## Frontend Defaults

For frontend work when the user did not provide design mockups:

- Prefer `shadcn/ui` for reusable UI primitives and common app components.
- Prefer `Tailwind CSS` for styling and layout.
- Reuse existing `shadcn/ui` patterns before creating custom primitives from scratch.
- Keep Tailwind usage disciplined: clear spacing, strong typography, restrained color palette, and minimal arbitrary values unless necessary.
- For meaningful UI changes, use the `playwright` MCP tools when available to verify desktop/mobile layout, key interactions, and obvious console/runtime issues.
- Default visual direction should feel polished and modern, not generic dashboard boilerplate.
- When unsure about a library API (shadcn/ui, Tailwind, React, Next.js, etc.), use `context7` MCP tools to look up current documentation before guessing.

## FINAL REMINDER

Before reporting completion to the user, verify you have updated the Phase Status block in `memory-bank/tasks.md`. If you did not change `- [YOUR_PHASE]: NOT_STARTED` to `- [YOUR_PHASE]: DONE`, do it now. The next phase CANNOT start without this update.
