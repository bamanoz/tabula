# MCP Admin Permissions And Timeouts

Priority: High

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

`mcp_add_server` can persist a stdio server with an arbitrary command or an HTTP
server with arbitrary URL/headers. Distro defaults allow `mcp_*`, while shell
execution tools require approval. MCP stdio calls can also block indefinitely on
`stdout.readline()`.

## Evidence

- `base/mcp/run.py`: `_server_spec_from_args` accepts arbitrary stdio command,
  env, HTTP URL, and headers.
- `base/mcp/run.py`: `mcp_add_server` writes config and republishes tools.
- `base/mcp/client.py`: `StdioTransport.send` waits on `stdout.readline()`
  without a request timeout.
- `tabula-distrib/code/application/apply.py`,
  `tabula-distrib/code-immune/application/apply.py`, and
  `tabula-distrib/claw/application/apply.py`: default rules allow `mcp_*`.

## Impact

MCP server administration can bypass `exec_run` approval by installing a stdio
server that runs arbitrary commands later. A hung MCP server can block plugin
workers and degrade sessions.

## Proposed Fix

- Split MCP permissions into admin and call categories.
- Default `mcp_add_server`, `mcp_remove_server`, and `mcp_reload` to `ask` or
  `deny`, not blanket allow.
- Add per-server trust policy for first-class `mcp__server__tool` calls.
- Add request timeout handling to stdio transport and restart/close hung server
  processes.
- Pin distro-provided MCP commands where applicable.

## Acceptance Criteria

- Distro defaults no longer blanket-allow MCP admin tools.
- Adding/removing/reloading MCP servers requires approval or explicit policy.
- Hung stdio MCP server calls time out and do not poison subsequent calls.
- Tests cover policy decisions and stdio timeout recovery.
