# ADR 0007 — `before_prompt_build` may rewrite session init tools

Date: 2026-07-03
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

The kernel sends `session.init` to prompt-building clients with two pieces of
per-join prompt state: `context` and `tools`. Plugins could already contribute
context through the `before_prompt_build` hook, but tool visibility policy had no
equivalent hook-owned surface. Permission plugins could deny calls at
`before_tool_call` time, yet denied tools could still appear in the model-visible
tool catalog.

Driver-local tool filtering would couple drivers to concrete permission bundles.
Distro-local `visible_tools` policy duplicates permission config and makes tool
surface shaping a product-specific workaround.

## Decision

Keep `before_prompt_build` as the single pre-`session.init` modifying hook and
allow its `modify` payload to include a replacement `tools` array as well as
`context`.

The rewritten `tools` array applies only to the current `session.init` delivery.
It does not mutate the kernel's runtime catalog, plugin capabilities, dispatch
tables, or persisted session state. Tool invocation remains protected by
`before_tool_call`; catalog filtering is a prompt-surface concern, not an
execution authorization boundary.

## Consequences

- Permission plugins can use their own config to hide fully denied tools before
  the driver builds provider prompts.
- Drivers do not need to import or understand permission bundles.
- Dynamic runtime catalog updates continue to rebuild `session.init`; each init
  pass reruns `before_prompt_build` and receives a fresh filtered view.
- A broken or unavailable domain hook still fails open for prompt building, so
  security-sensitive denial must remain in `before_tool_call`.
