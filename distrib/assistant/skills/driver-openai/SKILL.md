---
name: driver-openai
description: "OpenAI Responses API driver with streaming, tool use, and subagent result collection"
---
# driver-openai

Driver using the OpenAI Responses API with streaming, tool use, and subagent result collection.

Connects to the kernel via WebSocket (`TABULA_URL`), receives messages,
calls the OpenAI Responses API, and translates responses back to the
kernel protocol.

## Run

Configured automatically when `TABULA_PROVIDER=openai`:

```bash
python3 skills/driver-openai/run.py
```

## Config File

Path:

    ~/.tabula/config/global.toml

Example:

```toml
[openai]
model = "gpt-5.4"
base_url = "https://api.openai.com/v1"
api_key = { source = "store", id = "driver-openai.api_key" }
```

## Secrets

Path:

    ~/.tabula/secrets.json

Example:

```json
{
  "driver-openai.api_key": "sk-..."
}
```

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `api_key` | `string` | -- | yes | `TABULA_SKILL_DRIVER_OPENAI_API_KEY` | `OPENAI_API_KEY` | Accepts `store`, `env`, or `file` secret refs |
| `base_url` | `string` | `https://api.openai.com/v1` | no | `TABULA_SKILL_DRIVER_OPENAI_BASE_URL` | `OPENAI_BASE_URL` | OpenAI-compatible base URL |
| `model` | `string` | `gpt-5.4` | no | `TABULA_SKILL_DRIVER_OPENAI_MODEL` | `OPENAI_MODEL` | Responses API model name |

## Runtime Environment

| Variable | Required | Description |
|---|---|---|
| `TABULA_URL` | yes | Kernel WebSocket URL |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/global.toml`
3. `~/.tabula/secrets.json` for `api_key`
4. schema defaults

## Protocol

- Receives: `message`, `tool_result`, `init`
- Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`

## Notes

- Uses the Responses API with streaming enabled
- Supports parallel tool calls and the same subagent collection loop as the Anthropic driver
- Intended to be behaviorally compatible with `driver-anthropic`
