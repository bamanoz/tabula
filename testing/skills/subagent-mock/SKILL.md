---
name: subagent-mock
description: "Deterministic mock subagent for testing"
---
# subagent-mock

Deterministic mock subagent for testing driver/subagent orchestration without
calling an external LLM provider.

This is a test/dev runtime skill. In the repository it lives under
`testing/skills/`, but installed/runtime examples may expose it through a flat
`skills/` command surface in test homes.

It joins `subagent-<id>`, simulates work for a configurable number of turns,
and sends a mock result back to the parent session.

## Usage

```
SPAWN python3 skills/subagent-mock/run.py --id <id> --parent-session <your_session> --task "task" --max-turns 5
```

## Arguments

- `--id` — correlation id returned in the result message
- `--parent-session` — session to send the result to
- `--task` — task text to include in the mock result
- `--index` — optional numeric index for deterministic ordering
- `--max-turns` — number of simulated turns
- `--sleep-ms` — per-turn delay
- `--timeout` — optional idle timeout for follow-up messages
