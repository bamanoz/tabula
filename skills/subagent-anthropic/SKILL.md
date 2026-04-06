# Subagent (Anthropic)

Spawn a long-running subagent powered by Anthropic Claude. The subagent runs in its own session with its own LLM context, executes the task using available tools, and sends the result back to your session. It stays alive for follow-up messages until idle timeout.

## Usage

```
SPAWN python3 skills/subagent-anthropic/run.py --id <id> --parent-session <session> --task "<task description>"
```

## Arguments

- `--id` (required) — unique identifier for correlation. Results arrive as messages with this id.
- `--parent-session` (required) — your session name, where results will be delivered (usually `main`).
- `--task` (required) — what the subagent should do. Be specific — subagents don't see your conversation history.
- `--model` (optional) — override LLM model (default: from ANTHROPIC_MODEL env, or claude-sonnet-4-6).
- `--timeout` (optional) — idle timeout in seconds (default: 120). The subagent exits if it receives no messages for this duration.

## How it works

1. You SPAWN the subagent with a task and a unique id.
2. The subagent joins its own session (`subagent-<id>`), receives tools and system prompt from the kernel.
3. It works independently — calls tools, thinks, iterates.
4. When done, it sends the result as a message to your session with the id you provided.
5. The subagent stays alive, waiting for follow-up messages in its session.
6. You can send follow-up messages to the subagent's session (`subagent-<id>`) for multi-turn interaction.
7. If no messages arrive within the idle timeout, the subagent exits automatically.

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
SPAWN python3 skills/subagent-anthropic/run.py --id search_1 --parent-session main --task "Find all Python files that import socket"
```

Parallel subagents:
```
SPAWN python3 skills/subagent-anthropic/run.py --id research --parent-session main --task "Research how WebSocket protocols work"
SPAWN python3 skills/subagent-anthropic/run.py --id code --parent-session main --task "Write a simple HTTP server in Python"
```

With custom timeout (10 minutes):
```
SPAWN python3 skills/subagent-anthropic/run.py --id long_task --parent-session main --task "Refactor the auth module" --timeout 600
```

## Notes

- Each subagent uses its own LLM conversation loop (may use multiple turns for tool use).
- Subagents have access to the same kernel tools (EXEC, SPAWN, KILL, LIST).
- Subagents do not see your conversation history — provide full context in the task.
- Results are delivered as messages, not streamed.
- The subagent exits on idle timeout (default 120s) or kernel disconnect.
- For simple tasks, the subagent will finish quickly and idle out. For complex multi-step work, use a longer `--timeout`.
