# Structured Edit Diff Ledger

Type: AFK

Priority: P1

Status: Completed

Repos: `tabula-bundles`, optionally `tabula`

## Parent

`docs/competitors/claude-code.md`

## What to build

Record structured file edit events whenever workspace tools create, update, or
delete files. Each event should include enough information to show per-turn diffs,
generate commit summaries, and support future rollback/checkpoint behavior.

This should start with the existing fs write/edit paths and feed at least one
consumer, such as a diff inspection tool or gateway-web metadata row.

## Acceptance criteria

- [ ] File mutation tools emit ledger events with path, operation, before/after
      checksum, line counts, and bounded patch hunks where text diffs are safe.
- [ ] Events are associated with session id and, where available, turn id and
      task id.
- [ ] A diff inspection tool or gateway view can show edits grouped by turn.
- [ ] Binary or oversized diffs degrade to metadata without huge payloads.
- [ ] Tests cover create, update, delete, binary/large file fallback, and replay.

## Blocked by

- `001-session-ledger-harness-events.md`

## Implementation Notes

Implemented in current working tree:

- `workspace/fs` now emits `edit.diff` ledger events for:
  - `fs_write`
  - `fs_edit`
  - `fs_delete`
- Each event records:
  - `path`
  - `operation`
  - `tool_name`
  - `tool_call_id`
  - optional `turn_id` / `task_id` when available from tool-call context
  - before/after file metadata with checksums and line counts when text is safe
  - bounded unified diff text when safe
  - metadata fallback for binary / oversized cases
- Shared ledger helpers were moved to bundle `_lib` so mutation tools do not
  depend on the sessions bundle implementation.
- Consumer surface is `session_edits`, which groups recent `edit.diff` events by
  `turn_id`, `task_id`, or `tool_call_id`.
- Installed fs testbed coverage now verifies mutation execution plus consumer
  visibility through `session_edits`.

## Verification

- `python3 -m unittest workspace.fs.tests.test_fs_plugin base.sessions.test_sessions base.sessions.test_session_sdk`
- `PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite fs-plugin --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib`
