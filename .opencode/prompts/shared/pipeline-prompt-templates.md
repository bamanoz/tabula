# Pipeline Prompt Templates

These are EXACT templates for the Sequential Pipeline router.
The router MUST copy-paste these VERBATIM, replacing ONLY the variables in {curly braces}.

---

## Template: First Agent (BUILD)

```
You are Agent 1 of {N} in a Sequential BUILD Pipeline (Level {L}).

Read `memory-bank/tasks.md` for task metadata, scope, and relevant creative files.
Read `memory-bank/activeContext.md` for the compact handoff and working set.
Read `memory-bank/progress.md` for the current task state and current phase log.

Optional rule loading for this pass:
- Read `rules/visual-maps/build-mode-map.md` only if this pass is primarily visual/layout/component-composition work.
- Read `rules/Core/optimization-integration.md` only if this is Level 4 work and real integration/performance/coupling trade-offs emerge.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Do NOT read all Memory Bank docs by default. Read additional Memory Bank files only if the task, handoff, or real files explicitly require them.

Do your work, then update Memory Bank:
- tasks.md: update task status/scope if needed (not pipeline logs)
- progress.md: add your entry under "### Pipeline Build Log"
- activeContext.md: update current state and refresh "## Pipeline Handoff"
```

## Template: Mid Agent — after CONTRIBUTE (BUILD)

```
You are Agent {i} of {N} in a Sequential BUILD Pipeline (Level {L}).

Read `memory-bank/tasks.md` for task status/scope and relevant creative files.
Read `memory-bank/activeContext.md` for the compact handoff, working set, and next checks.
Read `memory-bank/progress.md` — see "### Pipeline Build Log" for what previous agents did on the current task.

Optional rule loading for this pass:
- Read `rules/visual-maps/build-mode-map.md` only if this pass is primarily visual/layout/component-composition work.
- Read `rules/Core/optimization-integration.md` only if this is Level 4 work and real integration/performance/coupling trade-offs emerge.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Start with the files named in `activeContext.md` and the latest build log entries.
Do NOT re-read all Memory Bank docs or broadly explore the project unless the current evidence is insufficient.

Do your work or DECLINE if all critical work is complete.
If CONTRIBUTE: update tasks.md for status/scope, progress.md for the pipeline log entry, and activeContext.md for current state plus the refreshed handoff block.
If DECLINE: return [DECISION: DECLINE] with justification.
Return [DECISION: CONTRIBUTE] or [DECISION: DECLINE].
```

## Template: Mid Agent — after DECLINE (BUILD)

```
You are Agent {i} of {N} in a Sequential BUILD Pipeline (Level {L}).

Previous agent DECLINED: "{decline_justification}"
Consider whether you agree, or look at the task from a different angle.

Read `memory-bank/tasks.md` for task status/scope and relevant creative files.
Read `memory-bank/activeContext.md` for the compact handoff, working set, and next checks.
Read `memory-bank/progress.md` — see "### Pipeline Build Log" for what previous agents did on the current task.

Optional rule loading for this pass:
- Read `rules/visual-maps/build-mode-map.md` only if this pass is primarily visual/layout/component-composition work.
- Read `rules/Core/optimization-integration.md` only if this is Level 4 work and real integration/performance/coupling trade-offs emerge.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Start with the files named in `activeContext.md` and the latest build log entries.
Do NOT re-read all Memory Bank docs or broadly explore the project unless the current evidence is insufficient.

Do your work or DECLINE if you also agree all critical work is complete.
If CONTRIBUTE: update tasks.md for status/scope, progress.md for the pipeline log entry, and activeContext.md for current state plus the refreshed handoff block.
If DECLINE: return [DECISION: DECLINE] with justification.
Return [DECISION: CONTRIBUTE] or [DECISION: DECLINE].
```

## Template: Mid Agent — potentially last (BUILD)

Use the applicable BUILD mid template above, then append this note exactly:

```
NOTE: You may be the last agent in this pipeline. If you CONTRIBUTE, also finalize:
- activeContext.md: set next phase to QA for Level 3; set next phase to SECURITY for Level 4; set next phase to REFLECT for Level 1 or 2
If you DECLINE and the pipeline ends after you, the router will handle finalization.
```

---

## Template: First Agent (PLAN)

```
You are Agent 1 of {N} in a Sequential PLAN Pipeline (Level {L}).

Read `memory-bank/tasks.md` for task metadata and planning scope.
Read `memory-bank/activeContext.md` for the compact handoff and current focus.
Read `memory-bank/progress.md` for the current task state and current phase log.

Optional rule loading for this pass:
- Read `rules/visual-maps/plan-mode-map.md` only if this pass is primarily visual planning or UI/UX decomposition work.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Do NOT read all Memory Bank docs by default. Read additional Memory Bank files only if the task, handoff, or real files explicitly require them.

Explore the codebase only as needed. Start from the handoff, current scope, and unresolved issues before broadening the search. Do your planning work, then update Memory Bank:
- tasks.md: update task plan/scope if needed (not pipeline logs)
- progress.md: add your entry under "### Pipeline Plan Log"
- activeContext.md: update current state and refresh "## Pipeline Handoff"
```

## Template: Mid Agent — after CONTRIBUTE (PLAN)

```
You are Agent {i} of {N} in a Sequential PLAN Pipeline (Level {L}).

Read `memory-bank/tasks.md` for task plan/scope.
Read `memory-bank/activeContext.md` for the compact handoff, working set, and next checks.
Read `memory-bank/progress.md` — see "### Pipeline Plan Log" for what previous agents did on the current task.

Optional rule loading for this pass:
- Read `rules/visual-maps/plan-mode-map.md` only if this pass is primarily visual planning or UI/UX decomposition work.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Start with the files and open questions named in `activeContext.md` and the latest plan log entries.
Do NOT re-read all Memory Bank docs or broadly explore the project unless the current evidence is insufficient.

Explore the codebase only as needed. Do your planning work or DECLINE if all critical work is complete.
If CONTRIBUTE: update tasks.md for plan/scope, progress.md for the pipeline log entry, and activeContext.md for current state plus the refreshed handoff block.
If DECLINE: return [DECISION: DECLINE] with justification.
Return [DECISION: CONTRIBUTE] or [DECISION: DECLINE].
```

## Template: Mid Agent — after DECLINE (PLAN)

```
You are Agent {i} of {N} in a Sequential PLAN Pipeline (Level {L}).

Previous agent DECLINED: "{decline_justification}"
Consider whether you agree, or look at the task from a different angle.

Read `memory-bank/tasks.md` for task plan/scope.
Read `memory-bank/activeContext.md` for the compact handoff, working set, and next checks.
Read `memory-bank/progress.md` — see "### Pipeline Plan Log" for what previous agents did on the current task.

Optional rule loading for this pass:
- Read `rules/visual-maps/plan-mode-map.md` only if this pass is primarily visual planning or UI/UX decomposition work.
- Read `rules/Core/memory-bank-paths.md` only if Memory Bank target files or paths are ambiguous.
- Otherwise do NOT load extra rules.

Start with the files and open questions named in `activeContext.md` and the latest plan log entries.
Do NOT re-read all Memory Bank docs or broadly explore the project unless the current evidence is insufficient.

Explore the codebase only as needed. Do your planning work or DECLINE if you also agree all critical work is complete.
If CONTRIBUTE: update tasks.md for plan/scope, progress.md for the pipeline log entry, and activeContext.md for current state plus the refreshed handoff block.
If DECLINE: return [DECISION: DECLINE] with justification.
Return [DECISION: CONTRIBUTE] or [DECISION: DECLINE].
```

## Template: Mid Agent — potentially last (PLAN)

Use the applicable PLAN mid template above, then append this note exactly:

```
NOTE: You may be the last agent in this pipeline. If you CONTRIBUTE, also finalize:
- activeContext.md: set next phase to CREATIVE (or BUILD if creative is skipped)
If you DECLINE and the pipeline ends after you, the router will handle finalization.
```

---

## RULES FOR THE ROUTER

- Copy the selected template VERBATIM
- Replace {N} with total agent count (8 or 16)
- Replace {L} with level (3 or 4)
- Replace {i} with current agent position number
- Replace {decline_justification} only when using an after-DECLINE template
- For potentially last calls, use the matching mid template and append the potentially-last note exactly
- Create a NEW Task tool call for each normal pipeline position; do NOT pass `task_id` except when retrying that exact failed/empty/malformed position
- Do NOT add ANY other text, context, instructions, or file lists
- Do NOT explain what previous agents did
- Do NOT assign tasks, roles, or steps
- Do NOT modify the template in ANY way beyond variable substitution and the required potentially-last append step
