---
name: lib
description: "Shared Python runtime library (`from skills.lib import ...`). Use `EXEC cat skills/lib/SKILL.md` for API docs."
---
# skills/lib — shared runtime library

Reusable building blocks for writing Tabula skills (LLM drivers, subagents, gateways). Import from `skills.lib.*`.

## Modules

### kernel_client — `skills.lib.kernel_client`

`KernelConnection` — thread-safe WebSocket wrapper for the kernel protocol.

```python
from skills.lib.kernel_client import KernelConnection

conn = KernelConnection("ws://localhost:8089/ws")
conn.send({"type": "connect", "name": "my-skill", ...})
msg = conn.recv(timeout=5.0)       # returns dict or None on close
conn.close()
```

### providers — `skills.lib.providers`

Abstract `ProviderSession` base class and concrete implementations for LLM APIs.

**Data types:**

| Type | Fields | Description |
|------|--------|-------------|
| `ToolCall` | `id`, `name`, `input` | A tool call from the LLM |
| `ToolResult` | `tool_use_id`, `output` | Result to feed back after tool execution |
| `TurnOutcome` | `final_text`, `tool_calls` | What one `generate()` call produced |

**ProviderSession interface:**

```python
class ProviderSession(ABC):
    def add_user_text(self, text: str): ...
    def add_tool_results(self, results: list[ToolResult]): ...
    def generate(self, on_text_delta: Callable[[str], None]) -> TurnOutcome: ...
    def abort(self): ...
    def record_aborted_turn(self): ...
```

**Implementations:**

- `AnthropicSession` — Claude streaming API (`/v1/messages`, SSE)
- `OpenAISession` — OpenAI Responses API (`/v1/responses`, SSE)
- `MockProvider` — deterministic, no LLM calls. For testing subagent orchestration (waves, fanouts, spawn failure handling). Configured via `MockConfig`.

**Creating a new provider:** subclass `ProviderSession`, implement `add_user_text`, `add_tool_results`, `generate`. The runtime calls `generate(on_text_delta)` — stream text via the callback, return `TurnOutcome` with tool calls or final text.

### driver_runtime — `skills.lib.driver_runtime`

`DriverRuntime` — orchestration loop for main LLM drivers. Handles the full lifecycle: init, message routing, tool execution, subagent spawning (SPAWN), result collection, streaming, and turn suppression.

```python
from skills.lib.driver_runtime import DriverConfig, DriverRuntime
from skills.lib.providers import AnthropicSession

runtime = DriverRuntime(
    config=DriverConfig(name="anthropic", url=TABULA_URL),
    provider_factory=lambda prompt, tools: AnthropicSession(
        system_prompt=prompt, model=MODEL, api_key=KEY, base_url=URL, tools=tools,
    ),
    logger=log,
)
runtime.connect()
runtime.run()
```

**What it handles automatically:**

- Connecting to kernel, joining session `main`, receiving `init`
- Streaming text deltas to gateway via `stream_start`/`stream_delta`/`stream_end`
- Sending `tool_use` to kernel, batching `tool_result` responses
- Extracting `--id` from SPAWN commands for subagent tracking
- **Collection mode**: after SPAWNs, waits for all subagent results before triggering next LLM turn
- **Early results buffer**: catches subagent results that arrive before collection mode starts
- **Spawn failure detection**: non-PID SPAWN results recorded as failures
- **Suppress/retroactive streaming**: intermediate turns (tool loops, collection flushes) are suppressed; final text is retroactively streamed
- Crash detection via kernel `error` messages
- Timeout after `MAX_WAIT_SEC` (300s) with partial result aggregation

**`DriverConfig` fields:** `name` (client name), `url` (kernel WebSocket URL)

### subagent_runtime — `skills.lib.subagent_runtime`

`SubagentRuntime` — orchestration loop for spawned subagents. Simpler than DriverRuntime: no SPAWN handling, no collection mode. Runs a task, sends result to parent session, optionally stays alive for follow-ups.

```python
from skills.lib.subagent_runtime import SubagentConfig, SubagentRuntime
from skills.lib.providers import AnthropicSession

runtime = SubagentRuntime(
    config=SubagentConfig(
        url=TABULA_URL, agent_id="research_1",
        parent_session="main", task="Find info about X",
    ),
    provider_factory=lambda prompt, tools: AnthropicSession(...),
    logger=log,
)
runtime.connect()
runtime.run()
```

**`SubagentConfig` fields:** `url`, `agent_id`, `parent_session`, `task`, `idle_timeout` (0=oneshot), `max_turns` (default 25), `spawn_token` (optional auth)

## Writing a new LLM driver

1. Create `skills/llm-<name>/run.py` (~60 lines)
2. Implement `ProviderSession` subclass (or use existing one)
3. Wire up: `DriverRuntime(config, provider_factory, logger)` → `.connect()` → `.run()`
4. Create `skills/llm-<name>/SKILL.md`
5. Add provider to `boot.py` aliases if needed

See `skills/llm-anthropic/run.py` and `skills/llm-mock/run.py` as examples.

## Writing a new subagent

1. Create `skills/subagent-<name>/run.py` (~80 lines)
2. Wire up: `SubagentRuntime(config, provider_factory, logger)` → `.connect()` → `.run()`
3. Create `skills/subagent-<name>/SKILL.md`

See `skills/subagent-anthropic/run.py` as example.
