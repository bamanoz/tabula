# Observability Correlation

Type: AFK

Priority: P2

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/competitors/claude-code.md`

## What to build

Add correlation ids and structured diagnostics across a single agent turn. A
developer should be able to trace a user prompt through kernel session routing,
driver processing, tool dispatch, permission hooks, plugin execution, usage
updates, and compaction events.

This should extend existing status/snapshot/log capabilities without requiring a
central telemetry service.

## Acceptance criteria

- [x] A turn correlation id is generated or propagated consistently through
      kernel, driver, tool dispatch, and plugin calls.
- [x] Structured logs include the correlation id without leaking prompt or secret
      contents.
- [x] At least one diagnostic command or endpoint can show recent events for a
      correlation id.
- [x] Tests cover correlation propagation across a tool call and a compaction
      event.
- [x] Documentation explains how to use the correlation id during debugging.

## Blocked by

- `001-session-ledger-harness-events.md`

## Implementation note

Tabula now uses `turn_correlation_id` as the canonical observability key for one
logical user turn.

- The kernel stamps `turn_correlation_id` onto inbound `message.user` metadata
  when the sender did not already provide one.
- The driver preserves or adopts that id, propagates it through tool calls,
  usage/session-status/provider-retry/provider-error/compaction events, and
  records `turn.lifecycle`, `turn.tool_call`, `turn.usage`, and
  `compaction.boundary` ledger events with the same id.
- Runtime invoke and worker call envelopes now carry `turn_correlation_id`, so
  plugin tools can surface it in their execution context.
- `workspace/fs` includes `turn_correlation_id` in `edit.diff` ledger payloads.
- Hook dispatch audit summaries include `turn_correlation_id` when the original
  tool-call metadata carried it.
- `turn_correlation_trace` in `base:sessions` merges matching history and ledger
  events for one session/correlation pair so a developer can inspect the turn
  without a central telemetry service.
