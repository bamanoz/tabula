# Expose durable artifact storage API

**Type:** AFK  
**Status:** completed

## What to build

Evolve `tool-result-store` into a reusable artifact owner while preserving oversized tool-result behavior. Plugins must be able to persist plans, reports, logs, receipts, and outcomes with bounded reads and correlation metadata.

## Required discovery and design

- Inspect how Ouroboros stores task artifacts, verification evidence, reviews, campaign state, and result references.
- Compare with Tabula tool-result artifacts, session SDK artifacts, state layout, and retention behavior.
- Design a generic artifact schema and SDK; Ouroboros-specific receipt schemas remain with their capability bundles.

## Acceptance criteria

- [x] SDK supports write, bounded read, metadata lookup, list/query, and correlation links.
- [x] Artifacts record tenant/plugin ownership, media type, checksum, size, timestamps, and optional correlation IDs.
- [x] Writes are atomic and path-safe; bounded reads remain enforced.
- [x] Existing oversized tool-result references remain compatible with documented behavior.
- [x] Retention/deletion ownership is documented and cross-tenant access is blocked.
- [x] An installed fixture plugin writes and reads a durable artifact after worker restart.

- [x] Canonical testbed and generated testbed template are updated; the suite writes and reads an artifact through the installed component after restart.

## Blocked by

None - can start immediately.
