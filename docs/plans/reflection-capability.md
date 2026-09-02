# Reflection Capability

Status: Implemented
Date: 2026-07-31
Issue: `issues/10-reflection-capability.md`
Architecture: `docs/adr/0012-modular-agent-capabilities.md`
Subagent SDK: `issues/03-subagent-orchestration-sdk.md`
Artifact API: `docs/adr/0017-tenant-local-artifact-owner.md`
Bundle ADR: `../tabula-bundles/docs/adr/0008-reviewed-post-work-reflection.md`

## Source Study

Ouroboros reflects after costly, failed, workspace, deep-review, or evolution work. It builds a bounded source projection from task goal, execution trace, tool usage, concrete errors, reviewer evidence, child work, and a frozen usage snapshot. Reflection output is persisted separately from raw task execution. Memory actions, backlog candidates, and self-evolution promotion are parsed as explicit downstream requests rather than treated as prose side effects.

Tabula already owns canonical session projections and committed events, auxiliary session records, typed subagent orchestration, durable artifacts, todos, generalized memory, continuity, and curated activity. Reflection should consume explicit source projections and reuse subagent lifecycle/artifact storage without owning any of those domains.

## Decision

Add independent `reflection` bundle with one warm plugin. Manual `reflection_run` accepts a stable request ID, task goal, optional session transcript source, artifact references, structured evidence, and reviewer findings. The plugin creates one typed subagent job through `tabula_subagents_sdk`, requires one strict JSON result, stores the raw result as an owner-bound artifact, and persists a normalized reflection record.

Structured output contains `outcome`, `lessons`, `patterns`, `mistakes`, `follow_ups`, `memory_candidates`, and `identity_candidates`. Reflection stores pattern and proposal projections in bundle-owned state. Memory, identity, activity, todo, initiative, and evolution integration remains explicit downstream proposal handling; reflection never mutates optional capability state.

## State And Determinism

State lives under `$TABULA_TENANT_DIR/state/plugins/reflection/`:

- `requests/<request-id>.json`: atomic normalized request/result state.
- `patterns.jsonl`: append-only pattern observations.
- `proposals.jsonl`: append-only memory, identity, and follow-up proposals.

A request ID is idempotent for identical canonical input. Reusing it with different input fails. Completed requests return the persisted result. Failed or timed-out requests remain failed until an explicit retry, which increments attempt and uses a deterministic subagent job ID. Corrupt state fails closed.

## Source Boundaries

Session transcript and artifact content are bounded before entering the prompt. Reviewer findings remain a distinct structured source section. Artifact bytes remain owned by `tool-result-store`; reflection owns only its result artifact and references to source artifacts.

## Optional Integrations

No behavioral bundle is a dependency. Reflection records proposals only. Other installed capabilities may later consume those proposals through their public tools/SDKs after separate authority checks.

## Non-Goals

- Automatic reflection trigger policy in the kernel.
- Hidden memory, identity, todo, activity, initiative, or evolution mutation.
- Raw tool-call logging or replacement of kernel committed-event, auxiliary-record, or reviewer evidence.
- Direct provider/model invocation outside subagent orchestration.
