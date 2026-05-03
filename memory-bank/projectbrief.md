# Project Brief

## Current Project Focus
Tabula is implementing a remote runtime / multi-backend execution architecture across the kernel, runtime daemon, installer, testbed, docs, and companion repos. The current workstream executes the M1 → M6 program described in `docs/issues/TASK.md`.

## Current Task Context
- Task: Execute grouped M2-M6 runtime cutover backlog
- Intent: implement
- Category: deep
- Level: 4

## Success Criteria
- The implementation follows the ADR, amendments, milestone sequencing, and per-issue dependency graph without architectural drift.
- Runtime, backend, tenant, plugin, skill, installer, docs, and testbed changes are planned and executed with the required validation gates.

## Constraints / Notes
- Kernel changes must stay generic and be limited to documented cutover points unless a plan explicitly justifies otherwise.
- No legacy surfaces or backward-compatibility aliases should be introduced by default.
