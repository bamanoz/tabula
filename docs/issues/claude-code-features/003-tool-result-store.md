# Tool Result Store

Type: AFK

Priority: P0

Status: Completed

Repos: `tabula-bundles`

## Parent

`docs/competitors/claude-code.md`

## What to build

Persist large tool outputs as session-scoped stored results and replace
prompt-visible content with a bounded preview plus a reference. The agent should
be able to read the full result explicitly when needed, while normal context and
history stay small.

This should work for at least one high-volume tool path, such as `exec_*`,
`fs_read`, `grep`, or test output, and it should survive driver resume.

## Acceptance Criteria

- [x] Large tool results are written under session state with deterministic
      metadata and safe filenames.
- [x] The provider-visible tool result contains a bounded preview and a stored
      result reference instead of the full output.
- [x] A tool can read a referenced result back into the conversation on demand.
- [x] Resume preserves stored result references and does not inline the full output.
- [x] Compaction treats previews as compactable while preserving metadata.
- [x] Tests cover large output persistence, preview truncation, readback, and resume.

## Implementation

Implemented in `tabula-bundles` and installed testbed coverage:

- Added shared storage helpers and platform-owned `artifact://...` references
  for deterministic ids, safe filenames, preview generation, index metadata, and
  readback.
- Runtime hook materializes large tool results before driver delivery, writes
  preview text plus metadata to driver-visible history, and keeps full content in
  per-session storage.
- `tool-result-store` exposes `tool_result_read` so agents can inspect a
  referenced result in bounded `offset`/`limit_chars` chunks without creating
  recursive large-result chains.
- Session transcript/context reconstruction preserves stored result references
  while keeping large content out of replay.
- Testbed `tool-result-store` suite verifies installed `tool_result_read`
  behavior.

Verification:

```bash
python3 -m unittest discover -s base/tool-result-store -p 'test_*.py'
python3 -m unittest base.sessions.test_session_sdk drivers.test_driver_agents
PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite tool-result-store --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib
```

## Blocked By

- `001-session-ledger-harness-events.md`
