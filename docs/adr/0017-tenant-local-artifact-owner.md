# ADR 0017: Tenant-Local Artifact Owner

- Status: Accepted
- Date: 2026-07-30
- Supersedes: none
- Superseded by: none

## Context

Plugins need durable plans, reports, logs, receipts, outcomes, and evidence without inventing private file layouts. Existing `tool-result-store` already creates `artifact://` references for oversized tool output, but its SDK is private, session-path-specific, and specialized around text tool results. Tenant identity is metadata only, content writes are not atomic, and no generic lookup/query/deletion contract exists.

Putting artifact semantics in the kernel would violate the dumb-kernel boundary. Making each capability own a separate artifact implementation would duplicate path safety, locking, bounded reads, checksums, and retention behavior.

## Decision

The `async:tool-result-store` component is the single generic artifact owner and publicly exports `tabula_artifacts`.

Artifacts are stored in tenant-local plugin state under `state/plugins/tool-result-store`. Metadata records tenant identity, owner plugin, media type, checksum, byte size, timestamps, optional name, caller metadata, and correlation links.

`ArtifactService` provides atomic write, bounded read, metadata lookup, query/list, and owner-only deletion. It binds tenant and plugin ownership to the active `PluginAPI` identity and derives the storage root from active tenant runtime context; caller input cannot select another tenant root or owner. Corrupt index/content, checksum mismatch, invalid paths, and tenant mismatch fail closed.

Artifact retention is explicit owner policy. The storage service performs no implicit expiry. Owning plugins may delete their own artifacts; operators may remove tenant-local state while the tenant is offline.

The oversized tool-result hook and `tool_result_read` remain supported as adapters over the same service and retain `artifact://` references and bounded incremental reads. Existing global session artifact files are not migrated.

Kernel protocol and persistence remain unchanged; artifact payloads remain opaque kernel data.

## Consequences

- Capability bundles share one durable, tenant-isolated artifact contract.
- Artifact content and index updates are atomic and cross-process serialized.
- Correlation metadata can link artifacts to sessions, tool calls, tasks, schedules, reviews, or transactions without importing those schemas.
- Cross-plugin reads are possible through references inside one tenant, while deletion remains owner-only.
- Existing oversized result UX remains stable, but old on-disk global session artifacts are intentionally not a compatibility surface.
