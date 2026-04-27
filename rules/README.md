# Rules Strategy For OpenCode

This repository uses a hybrid rules strategy for token efficiency in OpenCode.

## 1. Always Eager

These rules are loaded directly in agent prompts via `opencode.json` because they define execution contracts:

- `Core/complexity-decision-tree.md`
- `Core/file-verification.md`
- `Core/command-execution.md`
- `Core/platform-awareness.md`
- `Core/iron-law-debugging.md`
- `Level1/workflow-level1.md`
- `Level1/quick-documentation.md`
- `Level2/workflow-level2.md`
- `Level2/reflection-basic.md`
- `Level3/workflow-level3.md`
- `Level3/planning-comprehensive.md`
- `Level3/implementation-intermediate.md`
- `Level3/task-tracking-intermediate.md`
- `Level3/reflection-intermediate.md`
- `Level3/archive-intermediate.md`
- `Level4/workflow-level4.md`
- `Level4/architectural-planning.md`
- `Level4/phased-implementation.md`
- `Level4/task-tracking-advanced.md`
- `Level4/reflection-comprehensive.md`
- `Level4/archive-comprehensive.md`
- `Phases/CreativePhase/creative-phase-architecture.md`
- `Phases/CreativePhase/creative-phase-uiux.md`

## 2. Optional / Lazy

These rules are NOT loaded by default. Agents may read them via the Read tool only when a specific condition applies:

- `Core/optimization-integration.md`
  - Lazy for L4 BUILD when integration/performance/system-wide coupling becomes a real issue.
- `Core/memory-bank-paths.md`
  - Lazy for agents that hit path ambiguity or Memory Bank path confusion.
- `Core/creative-phase-enforcement.md`
  - Lazy for creative work when deciding whether a design concern truly belongs in CREATIVE.
- `Core/creative-phase-metrics.md`
  - Lazy for creative/reflection work when evaluating design quality.
- `Level1/optimized-workflow-level1.md`
  - Lazy reference only when reviewing or evolving Level 1 workflow conventions.
- `Level2/task-tracking-basic.md`
  - Lazy for L2 work if step tracking in `tasks.md` becomes insufficiently structured.
- `Level2/archive-basic.md`
  - Lazy only if a real Level 2 archive path is introduced.
- `Phases/CreativePhase/optimized-creative-template.md`
  - Lazy for creative work when the default creative template is insufficient.
- `Phases/CreativePhase/rubric-architecture.md`
  - Lazy for architecture-heavy creative reviews after options are drafted.
- `Phases/CreativePhase/rubric-uiux.md`
  - Lazy for UI/UX creative reviews when the task is genuinely visual.
- `Phases/CreativePhase/rubric-devex.md`
  - Lazy for workflow/devex-oriented creative reviews after options are drafted.
- `visual-maps/build-mode-map.md`
  - Lazy for visual BUILD paths only.
- `visual-maps/plan-mode-map.md`
  - Lazy for visual planning scenarios only.
- `visual-maps/creative-mode-map.md`
  - Lazy for visual-heavy creative scenarios only.
- `visual-maps/reflect-mode-map.md`
  - Lazy for visual reflection/review scenarios only.
- `visual-maps/archive-mode-map.md`
  - Lazy for archive tasks that need a visual deliverable summary.
- `visual-maps/van-mode-map.md`
  - Lazy for debugging VAN behavior only.
- `visual-maps/qa-mode-map.md`
  - Lazy only if a QA-style validation path is introduced.

## 3. Keep Out Of Prompt

These are reference/meta documents and should not be eagerly loaded into agents:

- `Core/hierarchical-rule-loading.md`
- `Core/mode-transition-optimization.md`

They may be read manually during architecture maintenance, but they are not part of runtime agent context.
