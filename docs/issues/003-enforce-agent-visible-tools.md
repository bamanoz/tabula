# Enforce Agent Visible Tools

Priority: High

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

Agent `visible_tools` filters what is shown to the provider, but the driver does
not enforce the same list when provider output contains tool calls. Primary
agents can therefore send a hidden tool call if the provider emits it.

## Evidence

- `_lib/python/src/tabula_drivers/driver_runtime.py`: `_ensure_provider` passes
  `active_tools` to the provider.
- `_lib/python/src/tabula_drivers/driver_runtime.py`: generated tool calls are
  forwarded as `MSG_TOOL_USE` without checking against `active_tools`.
- `code-immune/_lib/python/src/code_immune_agents/registry.py`: plan agent is
  documented as read-only and hides write/exec/subagent tools.

## Impact

`visible_tools` is only prompt/model guidance, not a policy boundary. The
`code-immune` plan agent can still attempt hidden write or exec tools if the
provider returns those calls and distro permissions allow them.

## Proposed Fix

- Before sending `MSG_TOOL_USE`, check the tool name against the current active
  tool catalog.
- For disallowed tools, write an audit log entry and feed a tool error back to
  the provider rather than forwarding to the kernel.
- Keep hook-permissions as the system-level boundary, but make driver-level
  visible-tool enforcement explicit.
- Add distro permissions for read-only agents where needed, instead of relying
  on prompt text alone.

## Acceptance Criteria

- A provider-emitted tool call not present in active tools is blocked by the
  driver.
- The provider receives a structured error for the blocked call.
- Existing allowed tool calls still work.
- Tests cover `code-immune` plan agent attempting `fs_write`, `fs_edit`,
  `exec_run`, and a visible read tool.
