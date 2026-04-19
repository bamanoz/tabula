---
name: driver-anthropic
description: "Claude API driver with streaming, tool use, and subagent result collection"
---
# driver-anthropic

Driver using the Anthropic Claude API with streaming, tool use, and subagent result collection.

Connects to the kernel via WebSocket (`TABULA_URL`), receives
messages, calls the Claude streaming API, and translates responses back to
kernel protocol.

## Run

Configured automatically when `TABULA_PROVIDER=anthropic`:

```bash
python3 skills/driver-anthropic/run.py
```

## Config File

Path:

    ~/.tabula/config/global.toml

Example:

```toml
[anthropic]
model = "claude-sonnet-4-6"
base_url = "https://api.anthropic.com"
api_key = { source = "store", id = "driver-anthropic.api_key" }
```

## Secrets

Path:

    ~/.tabula/secrets.json

Example:

```json
{
  "driver-anthropic.api_key": "sk-ant-..."
}
```

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `api_key` | `string` | -- | yes | `TABULA_SKILL_DRIVER_ANTHROPIC_API_KEY` | `ANTHROPIC_API_KEY` | Accepts `store`, `env`, or `file` secret refs |
| `base_url` | `string` | `https://api.anthropic.com` | no | `TABULA_SKILL_DRIVER_ANTHROPIC_BASE_URL` | `ANTHROPIC_BASE_URL` | Anthropic-compatible base URL |
| `model` | `string` | `claude-sonnet-4-6` | no | `TABULA_SKILL_DRIVER_ANTHROPIC_MODEL` | `ANTHROPIC_MODEL` | Claude model name |

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

- On connect, joins session "main" and waits for `init` (system prompt + tools)
- Streams text deltas token-by-token to gateway
- Tool calls are sent to kernel, results fed back to API for continuation
- Collects subagent results in the main turn loop so streaming and multi-agent execution stay in sync
- SIGINT aborts the current HTTP stream gracefully
