# llm-anthropic

LLM skill using the Anthropic Claude API with tool use support.

## Usage

This skill is not spawned manually. It is configured in `tabula.yaml` as the
LLM provider:

```yaml
llm:
  skill: python3 skills/llm-anthropic/run.py
```

## Boot protocol

1. Reads kernel tools definition (JSON) from first stdin line
2. Reads system prompt (genesis) until empty line delimiter
3. Enters main loop: calls Claude API, executes tool calls, waits for input

## Environment variables

- `ANTHROPIC_API_KEY` (required) — API key
- `ANTHROPIC_BASE_URL` — API base URL (default: https://api.anthropic.com)
- `ANTHROPIC_MODEL` — model name (default: claude-sonnet-4-6)

## Notes

- Converts kernel tool definitions to Anthropic tool use format
- SEND commands are fire-and-forget (no kernel response expected)
- All other tool calls wait for kernel response before continuing
