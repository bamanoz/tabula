---
inject: none
summary: "Claude API driver with streaming, tool use, and subagent result collection"
---
# llm-anthropic

LLM driver using the Anthropic Claude API with streaming, tool use, and subagent result collection.

Connects to the kernel via WebSocket (`TABULA_URL`), receives
messages, calls the Claude streaming API, and translates responses back to
kernel protocol.

## Usage

Configured in `tabula.yaml` under `spawn`:

    python3 skills/llm-anthropic/run.py

## Protocol

- Receives: `message`, `tool_result`, `init`
- Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`

## Environment variables

- `ANTHROPIC_API_KEY` (required) — API key
- `ANTHROPIC_BASE_URL` — API base URL (default: https://api.anthropic.com)
- `ANTHROPIC_MODEL` — model name (default: claude-sonnet-4-6)
- `TABULA_URL` — kernel WebSocket URL

## Notes

- On connect, joins session "main" and waits for `init` (system prompt + tools)
- Streams text deltas token-by-token to gateway
- Tool calls are sent to kernel, results fed back to API for continuation
- Collects subagent results in the main turn loop so streaming and multi-agent execution stay in sync
- SIGINT aborts the current HTTP stream gracefully
