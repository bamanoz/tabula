# Tool Result Artifacts

Type: AFK

Priority: P0

Status: Completed

Repos: `tabula-bundles`

## Parent

`docs/competitors/claude-code.md`

## What to build

Persist large tool outputs as session artifacts and replace prompt-visible
content with a bounded preview plus an artifact reference. The agent should be
able to read the full artifact explicitly when needed, while normal context and
history stay small.

This should work for at least one high-volume tool path, such as `exec_*`,
`fs_read`, `grep`, or test output, and it should survive driver resume.

## Acceptance criteria

- [x] Large tool results are written under session state with deterministic
      metadata and safe filenames.
- [x] The provider-visible tool result contains a bounded preview and an artifact
      reference instead of the full output.
- [x] A tool can read a referenced artifact back into the conversation on demand.
- [x] Resume preserves artifact references and does not inline the full output.
- [x] Compaction treats artifact previews as compactable while preserving the
      artifact metadata.
- [x] Tests cover large output persistence, preview truncation, artifact readback,
      and resume.

## Implementation

Implemented in `tabula-bundles` and installed testbed coverage:

- Added `tabula_session_sdk.artifacts` for deterministic artifact ids, safe
  filenames, preview generation, artifact index metadata, and readback.
- Driver runtime materializes large tool results through
  `materialize_tool_result()`, writes preview text to provider-visible history,
  preserves artifact metadata on assistant tool parts, and records a
  `tool_result.artifact` ledger event.
- Sessions plugin exposes `session_artifact_read` so agents can inspect a
  referenced artifact in bounded `offset`/`limit_chars` chunks without creating
  recursive large-result artifact chains.
- Session transcript/context reconstruction preserves artifact references while
  keeping large content out of replay.
- Testbed `sessions` suite verifies installed `session_artifact_read` behavior.

Verification:

```bash
python3 -m unittest base.sessions.test_session_sdk base.sessions.test_sessions drivers.test_driver_agents
python3 -m tabula_testbed_runner.runner --repo-root /Users/mak/src/tabula --suite sessions --source tabula-bundles=local:/Users/mak/src/tabula-bundles --source tabula-distrib=local:/Users/mak/src/tabula-distrib
```

## Blocked by

- `001-session-ledger-harness-events.md`
