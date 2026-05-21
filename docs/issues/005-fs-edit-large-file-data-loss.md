# fs_edit Large File Data Loss

Priority: High

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

`fs_edit` reads only `max_read_bytes`, ignores truncation, performs the replace,
and writes the truncated content back. Editing a large file can delete the tail
of the file.

## Evidence

- `workspace/fs/run.py`: `_read_text_limited` returns `(content, truncated)`.
- `workspace/fs/run.py`: `fs_edit` ignores `truncated` and writes `updated` via
  `_atomic_write`.
- Existing tests exercise normal edit behavior but not large-file truncation.

## Impact

Data loss in user workspaces when editing files larger than configured
`max_read_bytes`.

## Proposed Fix

- Make `fs_edit` fail if the file is truncated by the read limit.
- Optionally add a distinct `max_edit_bytes` config if edit limits should differ
  from read limits.
- Return a structured error explaining that the file is too large to edit safely.

## Acceptance Criteria

- Editing a file larger than `max_read_bytes` fails without modifying the file.
- Editing a file within the limit still works.
- Testbed or focused bundle test covers large-file tail preservation.
