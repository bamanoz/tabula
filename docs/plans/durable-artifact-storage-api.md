# Durable Artifact Storage API

## Discovery

### Ouroboros patterns worth preserving

- `ouroboros/task_results.py:200-218` validates task identity before choosing a durable result path.
- `ouroboros/task_results.py:320-360` keeps audit artifacts separate from primary task state.
- `ouroboros/task_results.py:395-760` records bounded evidence, correlation identity, hashes, and consumption state instead of embedding unbounded results.
- `ouroboros/utils.py:82-145` writes through sibling temporary files and `os.replace`; `utils.py:231-276` serializes read-modify-write updates and fails closed on lock failure.
- `ouroboros/supervisor/state.py:91-104` preserves recoverable state rather than accepting malformed current state.
- Ouroboros-specific campaign, review-obligation, model-budget, and self-modification schemas remain outside the generic artifact owner.

### Current Tabula behavior

- `async/tool-result-store` already owns `artifact://` references, oversized-result previews, bounded `tool_result_read`, checksum metadata, atomic index replacement, and cross-process index locking.
- Current storage is session-global under `$TABULA_HOME/data/sessions/<session>/artifacts`, so metadata tenant IDs do not enforce tenant isolation.
- `tabula_artifacts` is private and specialized around tool results. It lacks generic write/get/list/query/delete operations, plugin ownership, correlation links, media-generic content, atomic content writes, and explicit retention ownership.
- `tabula_session_sdk` re-exports the private artifact helpers, creating a second public-looking ownership surface.
- Kernel treats artifact metadata as opaque and requires no change.

## Design

### Ownership and location

`tool-result-store` remains the sole artifact owner and publicly exports `tabula_artifacts`.

Default storage is tenant-local:

```text
$TABULA_TENANT_DIR/state/plugins/tool-result-store/
  index.json
  index.lock
  objects/<prefix>/<artifact-id>.blob
```

`TABULA_TENANT_ID` is recorded in metadata. An explicit tenant ID that conflicts with active runtime identity is rejected. Tenant roots are never selected from caller input.

### Public service

`ArtifactService(api, ...)` binds tenant/plugin ownership to active `PluginAPI` identity and exposes:

- `write(content, media_type, name, correlations, metadata, artifact_id)`
- `read(ref, offset, limit_bytes)`
- `get(ref)`
- `list(owner_plugin, media_type, correlation, limit)`
- `delete(ref)`

Writes stamp tenant and owner plugin, calculate SHA-256 and byte size, write content atomically, then update the locked index atomically. IDs and filenames are validated. Existing IDs are idempotent only when owner, checksum, and media type match.

Reads are bounded to a fixed maximum. Text media returns UTF-8 content; other media returns base64. Reads verify tenant metadata, path shape, byte size, and checksum. Corrupt or missing state fails closed.

Deletion is owner-only. Retention is caller policy: the store does not silently expire artifacts. Owning plugins may delete their artifacts explicitly; distro/operator cleanup may remove the tenant-local store while that tenant is offline.

### Tool-result compatibility

The oversized `before_tool_result` hook becomes a client of `ArtifactService`:

- `artifact://<id>` remains the reference format.
- previews retain the documented `tool_result_read` instruction;
- `tool_result_read(session, ref, offset, limit_chars)` remains available;
- tool-result metadata includes session/tool correlations;
- old global session artifact files are not migrated or read.

This preserves the current external tool-result contract without preserving the old storage implementation.

### Installed verification

An `artifact-sdk-fixture` warm plugin writes through the installed public SDK, exits its worker after persisting, and exposes a second read tool. The installed testbed waits for worker restart, reads the same reference, verifies metadata/query/correlation behavior, then deletes it as the owner.
