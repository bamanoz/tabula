# Active Context

## Current Focus
Socket architecture v2 — IMPLEMENTED. Ready for E2E testing.

## Current Phase
Socket server implementation complete, needs terminal E2E test.

## What Was Completed
- kernel.zig: Full rewrite — Unix socket server, client registry, sessions, pub/sub routing, JSON lines protocol, tool execution (EXEC/SPAWN/KILL/LIST), cancel handling (SIGINT)
- main.zig: Full rewrite — new config parser (socket, system_prompt, spawn[]), system_prompt script runner, socket init, process spawning with TABULA_SOCKET env
- llm-anthropic/run.py: Full rewrite — socket client, Anthropic streaming API adapter, handles init/message/tool_result
- gateways/cli/gateway.py: New — socket client, prompt_toolkit + rich, streaming display, Ctrl+C cancel
- kernel.tools.json: Anthropic tool_use format (EXEC, SPAWN, KILL, LIST)
- system_prompt.py: Extracted from bootstrap.py
- tabula.yaml: New format with socket, system_prompt, spawn[]

## Architecture

### Config: `tabula.yaml`
```yaml
socket: /tmp/tabula.sock
system_prompt: python3 system_prompt.py skills/
spawn:
  - python3 llms/llm-anthropic/run.py
  - .venv/bin/python3 gateways/cli/gateway.py
```

### Protocol: JSON lines over Unix socket
- connect → connected (handshake with sends/receives)
- join → joined (session membership)
- message, stream_start/delta/end, tool_use, tool_result, done, cancel, init, error

### Boot sequence
1. Kernel reads tabula.yaml
2. Runs system_prompt.py → {"prompt":"..."}, stores it
3. Opens Unix socket
4. Spawns processes from spawn[] with TABULA_SOCKET env
5. Clients connect, join session, driver receives init
6. Ready

### Kernel: 4 tool commands
EXEC, SPAWN, KILL, LIST (via Anthropic tool_use format)

### Key decisions
1. **Heap-allocated Kernel** — Client read buffers make struct too large for stack
2. **Pure pub/sub routing** — no roles, clients declare sends/receives
3. **tool_use intercepted** — kernel executes tools, sends tool_result to session
4. **cancel → SIGINT** — kernel SIGINTs all spawned processes
5. **JSON lines** — one JSON object per line, newline delimited

## Verification Status
- zig build test: PASS (8/8 tests)
- Socket creation + client connect/join: PASS
- Init message delivery to driver: PASS (prompt + 4 tools)
- Terminal E2E (interactive): NOT TESTED YET
