# Memory Tool Permissions

Priority: Low

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

Memory tools are normal model-callable tools. Admin/delete operations are
irreversible, and status/wake-up responses expose the absolute palace path.
Distro default permissions do not clearly separate memory read/search/save/admin
operations.

## Evidence

- `_lib/python/src/_memory/lib.py`: config resolves and creates `PALACE_PATH`.
- `memory/memory-search/scripts/run.py`: `memory_wake_up` returns
  `palace_path`.
- `memory/memory-admin/scripts/run.py`: `memory_status` returns `palace_path`;
  `memory_delete` deletes by drawer id.
- Hook permissions default is allow-biased unless distro rules override it.

## Impact

Models can read/search/delete local persistent memory according to broad default
tool policy. Absolute local paths may leak runtime layout details.

## Proposed Fix

- Split default distro permissions for memory read/search/save/admin/delete.
- Make `memory_delete` ask or deny by default.
- Consider hiding absolute `palace_path` from model-facing responses unless
  diagnostics are explicitly requested.
- Document memory trust model and retention/deletion behavior.

## Acceptance Criteria

- Distro defaults require approval for `memory_delete`.
- Read/search/save/admin policies are explicit.
- Tests cover memory delete permission behavior.
- Model-facing status does not expose unnecessary absolute paths by default.
