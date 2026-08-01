# Deliver reflection capability end to end

**Type:** AFK  
**Triage:** ready-for-human

## What to build

Create independent `reflection` bundle that synthesizes completed work into lessons, mistakes, patterns, follow-ups, and optional proposals for other capabilities. It must retain useful output when no memory, continuity, activity, or initiative bundle is installed.

## Required discovery and design

- Inspect Ouroboros reflection/consciousness prompts, post-task processing, pattern/backlog/memory updates, reviewer separation, and failure handling.
- Compare with Tabula sessions, subagent presets, artifacts, skills, todo, MemPalace, and optional plugin discovery.
- Design explicit source and result schemas. Memory and identity outputs must be proposals, not hidden mutations.

## Acceptance criteria

- [x] Manual reflection accepts session/task/artifact sources and runs through reusable subagent orchestration.
- [x] Structured result includes outcome, lessons, patterns, mistakes, follow-ups, memory candidates, and identity candidates.
- [x] Reflections, patterns, and proposals persist in bundle-owned state with artifact references.
- [x] Optional integrations use installed capabilities without hard dependencies or silent state mutation.
- [x] Failure, timeout, duplicate request, and retry behavior are deterministic.
- [x] Bundle works without MemPalace, continuity, activity, initiative, or evolution.
- [x] Installed testbed completes work, runs reflection, and retrieves persisted structured output.

- [x] Canonical testbed and generated testbed template are updated; the suite executes reflection through the installed plugin/subagent path and reads persisted output.

## Blocked by

- [03 Expose reusable subagent orchestration SDK](03-subagent-orchestration-sdk.md)
- [05 Expose durable artifact storage API](05-artifact-storage-api.md)
