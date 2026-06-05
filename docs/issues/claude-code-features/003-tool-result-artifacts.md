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

- Added shared artifact helpers and platform-owned `artifact://...` references
  for deterministic artifact ids, safe filenames, preview generation, artifact
  index metadata, and readback.
- Runtime host materializes large tool results before transport, writes preview
  text plus artifact metadata to driver-visible history, and keeps full content
  in per-session artifact storage.
- Artifacts plugin exposes `artifact_read` so agents can inspect a referenced
  artifact in bounded `offset`/`limit_chars` chunks without creating recursive
  large-result artifact chains.
- Session transcript/context reconstruction preserves artifact references while
  keeping large content out of replay.
- Testbed `artifacts` suite verifies installed `artifact_read` behavior.

Verification:

```bash
python3 -m unittest base.artifacts.test_artifacts base.sessions.test_session_sdk drivers.test_driver_agents
PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite artifacts --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib
```

## Blocked by

- `001-session-ledger-harness-events.md`
