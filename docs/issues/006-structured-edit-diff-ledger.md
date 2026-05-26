# Structured Edit Diff Ledger

Type: AFK

Priority: P1

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
