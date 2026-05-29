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

- [ ] A turn correlation id is generated or propagated consistently through
      kernel, driver, tool dispatch, and plugin calls.
- [ ] Structured logs include the correlation id without leaking prompt or secret
      contents.
- [ ] At least one diagnostic command or endpoint can show recent events for a
      correlation id.
- [ ] Tests cover correlation propagation across a tool call and a compaction
      event.
- [ ] Documentation explains how to use the correlation id during debugging.

## Blocked by

- `001-session-ledger-harness-events.md`
