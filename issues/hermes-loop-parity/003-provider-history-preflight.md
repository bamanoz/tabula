---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Repair provider history invariants before requests

## What to build

Add a provider-history preflight pass before every provider request. It should
ensure the provider message history is structurally valid and safe to send: no
stray tool results, no malformed assistant tool-call arguments, and no invalid
consecutive user-message sequences for providers that require alternation.

The repair should be conservative, observable, and provider-aware where needed.

## Acceptance criteria

- [ ] Preflight runs before provider API requests in the main driver loop.
- [ ] Stray tool-result messages without a matching preceding assistant tool
      call are removed or converted according to provider requirements.
- [ ] Consecutive plain-text user messages are merged where safe.
- [ ] Assistant tool calls with corrupted argument JSON are repaired via the
      shared tool-input repair helper.
- [ ] Every repair emits a history/ledger event with counts and reason codes.
- [ ] Tests cover restored session history, steer insertion, and malformed tool
      history cases.
- [ ] After implementation, compare the completed behavior against Hermes'
      message-sequence repair paths and add follow-up work for any missing
      parity.

## Blocked by

- `001-tool-input-repair.md`
