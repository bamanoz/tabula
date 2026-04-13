---
name: driver-openai
description: "OpenAI Responses API driver with streaming, tool use, and subagent result collection"
---
# driver-openai

Driver using the OpenAI Responses API with streaming, tool use, and subagent result collection.

Connects to the kernel via WebSocket (`TABULA_URL`), receives messages,
calls the OpenAI Responses API, and translates responses back to the
kernel protocol.

## Usage

Configured automatically when `TABULA_PROVIDER=openai`:

    python3 skills/drivers/driver-openai/run.py

## Protocol

- Receives: `message`, `tool_result`, `init`
- Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`

## Environment variables

- `OPENAI_API_KEY` (required) — API key
- `OPENAI_BASE_URL` — API base URL (default: https://api.openai.com)
- `OPENAI_MODEL` — model name (default: `gpt-5`)
- `TABULA_URL` — kernel WebSocket URL

## Notes

- Uses the Responses API with streaming enabled
- Supports parallel tool calls and the same subagent collection loop as the Anthropic driver
- Intended to be behaviorally compatible with `driver-anthropic`
