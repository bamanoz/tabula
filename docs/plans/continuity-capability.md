# Continuity Capability

Status: Approved for implementation
Date: 2026-07-31
Issue: `issues/08-continuity-capability.md`
Architecture: `docs/adr/0012-modular-agent-capabilities.md`
Bundle ADR: `../tabula-bundles/docs/adr/0006-continuity-profile-and-biography.md`

## Source Study

Ouroboros couples constitution, identity, scratchpad, dialogue, and runtime state into one always-loaded context. `BIBLE.md:1-10` makes the constitution reviewed release policy; `BIBLE.md:64-75` requires identity recovery at every session; `ouroboros/memory.py:25-38` owns `identity.md` and its journal; `ouroboros/context.py:665-675` injects identity into model context; `ouroboros/reflection.py:497-549` records identity candidates instead of silently applying them.

Tabula already provides generic seams without kernel changes: tenant-local plugin state, `session_start`, `before_prompt_build`, tool-call approval exchanges, stable installed distro trees, and distro-owned prompt/rule policy.

## Decision

Add independent `continuity` bundle with one warm `continuity` plugin.

Plugin owns tenant-local state under `state/plugins/continuity/`:

- `current.json`: current profile pointer and fields.
- `revisions/<revision>.json`: immutable profile snapshots.
- `history.jsonl`: append-only revision audit records.
- `biography.jsonl`: append-only milestone records.

Profile updates and restores create new revisions. History records bind each revision to a canonical snapshot digest. Restore never rewrites history or makes an old snapshot mutable. Current and restore reads verify immutable snapshots; corrupt required state fails closed instead of appearing empty.

`session_start` and `before_prompt_build` inject one compact, session-stable profile snapshot. Exact rendered context is deduplicated, but arbitrary prompt text cannot suppress authoritative injection. Profile writes that would exceed configured context bounds are rejected; context is never silently truncated.

Plugin exposes current profile, history, update, milestone, and restore tools. Config defines allowed profile fields and fields requiring approval. Plugin owns approval suspension for those fields, so continuity remains independently installable without the security bundle. Approval resumes only the exact call ID and canonical input held in warm-worker memory; caller-supplied approval flags and unsolicited exchange replies are rejected.

Constitution, system prompts, distro rules, and installed distro content remain outside continuity state. Reserved policy fields are rejected even when approval is supplied.

## Failure And Recovery

Writes use a per-plugin process lock, same-directory temporary files, fsync, and atomic replace. Immutable revision files use exclusive creation. Audit or snapshot failure prevents current pointer advancement. Append-only biography/history records are fsynced before success returns.

Warm-worker restart reloads state from disk. Missing state means uninitialized continuity; malformed state is an explicit error.

## Non-Goals

- Generalized memory or semantic recall.
- Activity/event projection.
- Reflection-driven hidden identity mutation.
- Initiative or evolution policy.
- Mutable distro constitution or prompt rules.
