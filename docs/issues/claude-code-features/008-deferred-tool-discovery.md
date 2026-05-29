# Deferred Tool Discovery

Type: HITL

Priority: P2

Repos: `tabula-bundles`, `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Introduce a tracer bullet for reducing model-visible tool schema bloat. A distro
should be able to expose a small stable base tool set plus a `tool_search` style
tool that lets the agent discover and enable deferred tools on demand.

This needs a human design check because it affects prompt shape, tool visibility,
MCP exposure, permission rules, and user expectations.

## Acceptance criteria

- [ ] A design note defines base tools, deferred tools, selection semantics, and
      how selected tools persist across turns and compaction.
- [ ] One distro can install with a deferred tool enabled and at least one tool
      hidden behind discovery.
- [ ] The agent can search for the hidden tool, select it, and successfully call
      it in the same session.
- [ ] Permission rules still apply to deferred tools before execution.
- [ ] Tests verify deterministic tool ordering and selected-tool persistence.

## Blocked by

- `001-session-ledger-harness-events.md`
