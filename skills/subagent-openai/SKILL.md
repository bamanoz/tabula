---
name: subagent-openai
description: "OpenAI-backed sub-agent for parallel tasks. Usage: `SPAWN python3 skills/subagent-openai/run.py --id <unique_id> --parent-session <your_session> --task \"<task description>\"`. Optional: `--timeout N`. Full docs: `EXEC cat skills/subagent-openai/SKILL.md`"
---
# Subagent (OpenAI)

Spawn a long-running subagent powered by the OpenAI Responses API. The subagent
runs in its own session, executes the task using kernel tools, and sends the
result back to the parent session. It can stay alive for follow-up messages.

## Usage

```
SPAWN python3 skills/subagent-openai/run.py --id <id> --parent-session <session> --task "<task>"
```

## Arguments

- `--id` (required) — unique identifier for correlation
- `--parent-session` (required) — session where the result should be delivered
- `--task` (required) — initial task for the subagent
- `--model` (optional) — override `OPENAI_MODEL`
- `--timeout` (optional) — idle timeout in seconds; `0` means oneshot mode
- `--max-turns` (optional) — max LLM turns for the task

## Environment variables

- `OPENAI_API_KEY` (required)
- `OPENAI_BASE_URL` (optional)
- `OPENAI_MODEL` (optional)
- `TABULA_URL` (required)

## Notes

- Uses the same subagent runtime as `subagent-anthropic`
- Returns results as `message` events with `id=<subagent-id>`
- Supports follow-up turns while the process stays alive
