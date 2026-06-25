---
labels: [needs-triage, hermes-loop-parity, agent-loop]
type: AFK
---

# Repair and classify malformed tool-call input

## What to build

Add a generic driver-side tool-input repair helper that parses, repairs, and
classifies malformed model-emitted tool-call arguments before a `ToolCall` is
sent to the kernel. The provider adapters should preserve raw input, repaired
input, and repair metadata so the driver can record what happened and the model
can receive a clear recovery signal when needed.

This should live in `tabula-bundles` because it is reusable driver behavior, not
kernel or distro policy.

## Acceptance criteria

- [ ] A shared helper repairs common JSON defects: empty input, whitespace,
      Python `None`, trailing commas, unclosed braces/brackets, excess closing
      braces/brackets, and unescaped control characters.
- [ ] The helper returns structured metadata: repaired/not repaired, reason,
      raw preview, and whether fallback `{}` was used.
- [ ] OpenAI Responses, OpenAI Chat, and Anthropic provider adapters use the
      helper when building `ToolCall.input` from raw arguments.
- [ ] `ToolCall` or adjacent metadata preserves `raw_input` and repair metadata
      without changing kernel protocol shape.
- [ ] Focused tests cover repaired input and unrepairable fallback behavior.
- [ ] After implementation, compare the completed behavior against the relevant
      Hermes repair paths and add follow-up work for any missing parity.

## Blocked by

None - can start immediately.
