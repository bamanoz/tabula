---
name: hook-logger
description: "Audit logger — logs all hook events to JSONL file"
inject: none
---
# hook-logger

Subscribes to kernel hook events and writes them to a JSONL audit log.

## Usage

    .venv/bin/python3 skills/hook-logger/run.py [--log-file PATH]

## Protocol

- Sends: (nothing — void hooks only)
- Receives: `hook`
- Hooks: `after_message` (void), `after_tool_call` (void), `session_start` (void)

## Environment variables

- `TABULA_URL` — kernel WebSocket URL (default: `ws://localhost:8089/ws`)
- `TABULA_LOG_FILE` — log file path (default: `~/.tabula/logs/hooks.jsonl`)
