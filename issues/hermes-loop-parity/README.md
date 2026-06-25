# Hermes Loop Parity

Goal: make Tabula's driver loop at least as resilient as `hermes-agent` while
preserving Tabula's kernel/runtime/distro boundaries.

Hermes does not appear to use pydantic as the primary runtime integrity layer
for model-emitted tool calls. Pydantic is present for web/API/MCP schemas and
Hermes prompt/trajectory text mentions a pydantic-style `FunctionCall` schema,
but the loop's robustness comes from explicit JSON repair, tool-call argument
coercion, message-sequence repair, partial-stream recovery, retry state, and
turn finalization diagnostics.

Local upstream references:

- `/Users/mak/src/hermes-agent/agent/message_sanitization.py`
- `/Users/mak/src/hermes-agent/agent/agent_runtime_helpers.py`
- `/Users/mak/src/hermes-agent/agent/tool_dispatch_helpers.py`
- `/Users/mak/src/hermes-agent/agent/tool_executor.py`
- `/Users/mak/src/hermes-agent/agent/turn_retry_state.py`
- `/Users/mak/src/hermes-agent/agent/turn_finalizer.py`
- `/Users/mak/src/hermes-agent/tests/agent/test_empty_tool_name_loop_dampening.py`
- `/Users/mak/src/hermes-agent/tests/gateway/test_stuck_loop.py`

## Issues

1. `001-tool-input-repair.md` — repair and classify malformed tool-call input.
2. `002-empty-tool-name-anti-priming.md` — dampen empty tool-name loops.
3. `003-provider-history-preflight.md` — repair provider history invariants.
4. `004-partial-tool-stream-recovery.md` — recover from partial streamed tool calls.
5. `005-turn-retry-state.md` — centralize per-turn retry/recovery state.
6. `006-turn-exit-diagnostics.md` — emit structured turn exit diagnostics.
7. `007-tool-result-budget-guard.md` — enforce driver-side tool result budgets.
8. `008-stuck-session-detector.md` — detect stuck sessions across restarts.
