---
name: subagent-anthropic
description: "Autonomous LLM sub-agent for parallel tasks. Usage: `SPAWN python3 skills/subagent-anthropic/run.py --id <unique_id> --parent-session <your_session> --task \"<task description>\"`. Optional: `--timeout N` (stay alive for follow-ups, default: 0=oneshot). Full docs: `EXEC cat skills/subagent-anthropic/SKILL.md`"
requires-kernel-tools: ["process_spawn"]
---
# Subagent (Anthropic)

Spawn a long-running subagent powered by Anthropic Claude. The subagent runs in its own session with its own LLM context, executes the task using available tools, and sends the result back to your session. It stays alive for follow-up messages until idle timeout.

## When to use subagents

Use a subagent when:
- The task requires running a command and waiting for its output (e.g. network requests, file searches, system checks)
- The task is independent and self-contained — can be described in one sentence
- You want to run multiple tasks in parallel (spawn several subagents at once)
- The task involves exploration or research that may require multiple tool calls

Do NOT use a subagent when:
- You can answer directly from your own knowledge
- A single EXEC command is enough — just run it yourself
- The task requires back-and-forth with the user
- The task is trivial (e.g. "what time is it")

## Usage

```bash
SPAWN python3 skills/subagent-anthropic/run.py --id <id> --parent-session <session> --task "<task description>"
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

The loader checks `subagent-anthropic.api_key` first, then shared
`driver-anthropic.api_key`.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `api_key` | `string` | -- | yes | `TABULA_SKILL_SUBAGENT_ANTHROPIC_API_KEY` | `ANTHROPIC_API_KEY` | Store fallback order: `subagent-anthropic.api_key`, then `driver-anthropic.api_key` |
| `base_url` | `string` | `https://api.anthropic.com` | no | `TABULA_SKILL_SUBAGENT_ANTHROPIC_BASE_URL` | `ANTHROPIC_BASE_URL` | Anthropic-compatible base URL |
| `model` | `string` | `claude-sonnet-4-6` | no | `TABULA_SKILL_SUBAGENT_ANTHROPIC_MODEL` | `ANTHROPIC_MODEL` | `--model` can still override at process start |

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

- `--id` (required) — unique identifier for correlation. Results arrive as messages with this id.
- `--parent-session` (required) — your session name, where results will be delivered. Use the session name from your system prompt.
- `--task` (required) — what the subagent should do. Be specific — subagents don't see your conversation history.
- `--model` (optional) — override configured model for the current process.
- `--timeout` (optional) — idle timeout in seconds (default: 0 = oneshot). With the default, the subagent exits immediately after completing the task. Set `--timeout 120` to keep it alive for follow-up messages.

## How it works

1. You SPAWN the subagent with a task and a unique id.
2. The subagent joins its own session (`subagent-<id>`), receives tools and system prompt from the kernel.
3. It works independently — calls tools, thinks, iterates.
4. When done, it sends the result as a message to your session with the id you provided.
5. By default (oneshot mode), the subagent exits immediately after delivering the result.
6. To enable follow-ups, pass `--timeout N` (e.g. `--timeout 120`). The subagent stays alive, waiting for messages in its session.
7. You can send follow-up messages to the subagent's session (`subagent-<id>`) for multi-turn interaction.

## Correlation

Always provide a unique `--id` per subagent. When you receive a message containing that id, it's the result of that subagent. This is how you match results when running multiple subagents in parallel.

## Follow-up messages

After receiving the initial result, you can send follow-up messages to continue the conversation with the subagent:

```
{"type": "message", "session": "subagent-<id>", "text": "Now also check for error handling"}
```

The subagent maintains full conversation history, so follow-ups have full context of previous work.

## Examples

Single subagent:
```
SPAWN python3 skills/subagent-anthropic/run.py --id search_1 --parent-session <your_session> --task "Find all Python files that import socket"
```

Parallel subagents:
```
SPAWN python3 skills/subagent-anthropic/run.py --id research --parent-session <your_session> --task "Research how WebSocket protocols work"
SPAWN python3 skills/subagent-anthropic/run.py --id code --parent-session <your_session> --task "Write a simple HTTP server in Python"
```

With follow-up support (stays alive 10 minutes):
```
SPAWN python3 skills/subagent-anthropic/run.py --id long_task --parent-session <your_session> --task "Refactor the auth module" --timeout 600
```

## Notes

- By default, subagents are oneshot — they complete the task, send the result, and exit.
- To keep a subagent alive for follow-ups, pass `--timeout N`.
- Each subagent uses its own LLM conversation loop with batched tool-result handling.
- Subagents have access to the same kernel tools (EXEC, SPAWN, KILL, LIST).
- Subagents do not see your conversation history — provide full context in the task.
- Results are delivered as messages, not streamed.
- For simple tasks, the subagent will finish quickly. For complex multi-step work, it may take longer but still exits after completing.
