# IDE Bridge Tracer

Type: HITL

Priority: P3

Repos: `tabula-bundles`, optionally `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Create a minimal IDE bridge tracer bullet that proves Tabula can share harness
state with an editor. The first version should expose file edit notifications,
inline diff metadata, and editor-side permission prompts through a narrow local
transport or ACP-adjacent gateway.

This needs human design review because it defines an integration contract with
external editor extensions.

## Acceptance criteria

- [ ] A design note chooses the initial editor transport and message envelope.
- [ ] File edit ledger events can be forwarded to an IDE client stub.
- [ ] A permission request can be surfaced to the IDE client stub and answered.
- [ ] The bridge degrades safely when no IDE client is connected.
- [ ] Tests cover file notification, permission response, disconnect, and unknown
      message handling.

## Blocked by

- `006-structured-edit-diff-ledger.md`
- `007-policy-decision-ledger.md`
