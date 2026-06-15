# Deferred Tool Discovery

Type: HITL

Priority: P2

Repos: `tabula-bundles`, `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Introduce a tracer bullet for reducing model-visible tool schema bloat. A distro
should be able to expose a small stable base tool set plus a Claude Code-style
`tool_search` tool that lets the agent discover and load deferred tools on demand.

This needs a human design check because it affects prompt shape, tool visibility,
MCP exposure, permission rules, and user expectations.

## Acceptance criteria

- [x] A design note defines base tools, deferred tools, discovery semantics, and
      how discovered tools persist across turns and compaction.
- [x] One distro can install with a deferred tool enabled and at least one tool
      hidden behind discovery.
- [x] The agent can search for the hidden tool, load it, and successfully call
      it in the same session.
- [x] Permission rules still apply to deferred tools before execution.
- [x] Tests verify deterministic tool ordering and discovered-tool persistence.

## Design note

Deferred discovery is implemented as the reusable `base:deferred-tools` plugin
plus the `tabula_deferred_tools` SDK. Distro or tenant policy config defines:

- `base_tools`: fnmatch patterns that remain provider-visible before discovery.
- `deferred_tools`: fnmatch patterns hidden from provider-visible schemas until discovered.
- `search_tags`: optional per-tool terms used by `tool_search` without changing tool manifests.

The plugin exposes `tool_search` and `tool_discovery_status`. `tool_search`
follows the Claude Code model: the prompt explains deferred tools using the same
`<available-deferred-tools>` surface, and one tool call both searches and loads
matching deferred tools for the session. Newly loaded tools append a
`deferred_tools.discovered` ledger event under the active session. Discovery is
therefore session-scoped and survives subsequent turns, driver rebuilds, and
compaction because it is not stored in provider message history.

Provider-visible filtering lives in the driver seam, where provider request tool
schemas are assembled. The driver applies agent-visible tool filtering first,
then deferred-tool filtering, so an agent cannot reveal a tool outside its own
configured surface. After a successful `tool_search` that discovers at least one
new deferred tool, the driver invalidates the provider instance before
continuing the same session so the next provider call uses the newly visible
schema set.

Deferred discovery does not bypass execution policy. `tool_search` only changes
provider visibility; the discovered tool still reaches the kernel as a normal
tool call and runs through `before_tool_call` hooks such as `hook-permissions`
before execution.

## Blocked by

- `001-session-ledger-harness-events.md`
