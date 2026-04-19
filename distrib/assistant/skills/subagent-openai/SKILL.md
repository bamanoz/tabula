---
name: subagent-openai
description: "OpenAI-backed sub-agent for parallel tasks. Usage: `SPAWN python3 skills/subagent-openai/run.py --id <unique_id> --parent-session <your_session> --task \"<task description>\"`. Optional: `--timeout N`. Full docs: `EXEC cat skills/subagent-openai/SKILL.md`"
requires-kernel-tools: ["process_spawn"]
---
# Subagent (OpenAI)

Spawn a long-running subagent powered by the OpenAI Responses API. The subagent
runs in its own session, executes the task using kernel tools, and sends the
result back to the parent session. It can stay alive for follow-up messages.

## Run

```bash
SPAWN python3 skills/subagent-openai/run.py --id <id> --parent-session <session> --task "<task>"
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

The loader checks `subagent-openai.api_key` first, then shared
`driver-openai.api_key`.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `api_key` | `string` | -- | yes | `TABULA_SKILL_SUBAGENT_OPENAI_API_KEY` | `OPENAI_API_KEY` | Store fallback order: `subagent-openai.api_key`, then `driver-openai.api_key` |
| `base_url` | `string` | `https://api.openai.com/v1` | no | `TABULA_SKILL_SUBAGENT_OPENAI_BASE_URL` | `OPENAI_BASE_URL` | OpenAI-compatible base URL |
| `model` | `string` | `gpt-5.4` | no | `TABULA_SKILL_SUBAGENT_OPENAI_MODEL` | `OPENAI_MODEL` | `--model` can still override at process start |

## Runtime Environment

| Variable | Required | Description |
|---|---|---|
| `TABULA_URL` | yes | Kernel WebSocket URL |
| `TABULA_SPAWN_TOKEN` | no | Spawn token passed by kernel when subagent auth is enabled |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/global.toml`
3. `~/.tabula/secrets.json` for `api_key`
4. schema defaults
5. `--model` overrides the resolved model for the current process

## Arguments

- `--id` (required) — unique identifier for correlation
- `--parent-session` (required) — session where the result should be delivered
- `--task` (required) — initial task for the subagent
- `--model` (optional) — override configured model for this process
- `--timeout` (optional) — idle timeout in seconds; `0` means oneshot mode
- `--max-turns` (optional) — max LLM turns for the task

## Notes

- Uses the same subagent runtime as `subagent-anthropic`
- Returns results as `message` events with `id=<subagent-id>`
- Supports follow-up turns while the process stays alive
