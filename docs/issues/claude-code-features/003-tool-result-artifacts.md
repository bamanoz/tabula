# Tool Result Artifacts

Type: AFK

Priority: P0

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

## Blocked by

- `001-session-ledger-harness-events.md`
