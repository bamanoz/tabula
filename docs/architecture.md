# Tabula v2 — Socket Architecture

## Overview

Kernel = Unix domain socket server + tool executor + session router.
All clients (LLM drivers, gateways, services like cron) connect via the same socket.
No roles — clients declare `sends[]` and `receives[]` arrays (pub/sub on message types).
Sessions group clients for routing.

```
┌──────────────┐
│   Gateway     │──┐  sends: [message, cancel]
│  prompt_toolkit  │  receives: [stream_start, stream_delta, stream_end, error]
└──────────────┘  │
                  │
┌──────────────┐  │         ┌─────────────────────┐
│  LLM Driver   │──┼── sock ──►│   Zig Kernel          │
│  (anthropic)  │  │         │  • socket server      │
└──────────────┘  │         │  • pub/sub router     │
                  │         │  • tool executor      │
┌──────────────┐  │         │  • session manager    │
│  Cron / etc   │──┘         └─────────────────────┘
└──────────────┘
```

## Config (`tabula.yaml`)

```yaml
socket: /tmp/tabula.sock
system_prompt: python3 system_prompt.py skills/
spawn:
  - python3 llms/llm-anthropic/run.py
  - python3 gateways/cli/gateway.py
```

## Boot Sequence

1. Kernel reads `tabula.yaml`
2. Runs `system_prompt.py` (one-shot) → `{"prompt":"..."}`, stores it
3. Opens Unix socket at configured path
4. Spawns processes from `spawn[]` with `TABULA_SOCKET` env var
5. LLM driver connects, joins session "main", receives `init` (prompt + tools)
6. Gateway connects, joins session "main"
7. Ready

---

## Protocol (JSON lines over Unix socket)

### Connection Handshake

Client → Kernel:
```json
{"type":"connect","name":"anthropic","sends":["stream_start","stream_delta","stream_end","tool_use","done"],"receives":["message","tool_result","init"]}
```

Kernel → Client:
```json
{"type":"connected","id":"c1"}
```

### Session

Client → Kernel:
```json
{"type":"join","session":"main"}
```

Kernel → Client:
```json
{"type":"joined","session":"main"}
```

### Message Types

| Type | Description | Sender | Receiver |
|------|-------------|--------|----------|
| `message` | User/service text for LLM | gateway, cron | driver |
| `stream_start` | Response started | driver | gateway |
| `stream_delta` | Token/chunk | driver | gateway |
| `stream_end` | Response finished | driver | gateway |
| `tool_use` | Tool call (kernel intercepts) | driver | **kernel** |
| `tool_result` | Tool execution result | kernel | driver |
| `done` | Turn complete | driver | (informational) |
| `cancel` | Abort generation | gateway | **kernel → SIGINT** |
| `init` | System prompt + tools | kernel | driver |
| `error` | Error notification | kernel | any subscriber |

### Message Format Examples

```json
{"type":"message","text":"прочитай файл tabula.yaml"}
{"type":"stream_start"}
{"type":"stream_delta","text":"Вот содержимое:"}
{"type":"stream_end"}
{"type":"tool_use","id":"1","name":"EXEC","input":{"command":"cat tabula.yaml"}}
{"type":"tool_result","id":"1","output":"socket: /tmp/tabula.sock\n..."}
{"type":"done"}
{"type":"cancel"}
{"type":"init","prompt":"You are Tabula...","tools":[...]}
{"type":"error","text":"process died"}
```

---

## Routing Logic

```
Kernel receives message from client in session S:
  1. Verify type ∈ client's sends[] (else reject)
  2. type == "tool_use" → kernel executes tool, sends tool_result to session
  3. type == "cancel" → kernel sends SIGINT to driver process
  4. Else → broadcast to all clients in session S where type ∈ their receives[]
```

Pure router. No roles, no special cases except `tool_use` and `cancel`.

---

## Data Flow Example

```
User types: "прочитай tabula.yaml"

 1. Gateway → Kernel:  {"type":"message","text":"прочитай tabula.yaml"}
 2. Kernel → Driver:   (routes "message" to subscribers)
 3. Driver → Kernel:   {"type":"stream_start"}
 4. Driver → Kernel:   {"type":"stream_delta","text":"Сейчас прочитаю."}
 5. Kernel → Gateway:  (routes stream_delta to subscribers)
 6. Driver → Kernel:   {"type":"stream_end"}
 7. Driver → Kernel:   {"type":"tool_use","id":"1","name":"EXEC","input":{"command":"cat tabula.yaml"}}
 8. Kernel:            executes EXEC, gets result
 9. Kernel → Driver:   {"type":"tool_result","id":"1","output":"socket: /tmp/..."}
10. Driver:            calls Claude API again with tool_result
11. Driver → Kernel:   {"type":"stream_start"}
12. Driver → Kernel:   {"type":"stream_delta","text":"```yaml\nsocket: ..."}
13. Driver → Kernel:   {"type":"stream_end"}
14. Driver → Kernel:   {"type":"done"}
```

## Ctrl+C Flow

```
Ctrl+C → prompt_toolkit → KeyboardInterrupt
  → Gateway sends: {"type":"cancel"}
  → Kernel sends SIGINT to driver process
  → Driver aborts HTTP stream, sends stream_end + done
  → Gateway shows prompt
```

## Cron Example

```bash
# crontab
0 3 * * * python3 /path/to/send_message.py "main" "сделай бэкап"
```

```python
# send_message.py (~15 lines):
# 1. Connect to TABULA_SOCKET
# 2. {"type":"connect","name":"cron","sends":["message"],"receives":[]}
# 3. {"type":"join","session":"main"}
# 4. {"type":"message","text":"сделай бэкап"}
# 5. Disconnect
```

---

## LLM Driver (Adapter Pattern)

```
Anthropic Streaming API → Kernel protocol:
  content_block_delta(text_delta)  → {"type":"stream_delta","text":"..."}
  (first text in response)        → {"type":"stream_start"} before first delta
  content_block_start(tool_use)    → accumulate
  content_block_delta(input_json)  → accumulate
  content_block_stop(tool block)   → {"type":"tool_use","id":"...","name":"...","input":{}}
  message_stop                     → {"type":"stream_end"} + {"type":"done"}

Kernel protocol → Anthropic API:
  message       → user message, call API
  tool_result   → tool_result content block, call API again
  init          → store as system prompt for API calls
```

---

## Kernel Tools

| Tool | Description |
|------|-------------|
| `EXEC` | Sync command execution, return stdout |
| `SPAWN` | Start process (with TABULA_SOCKET env) |
| `KILL` | Kill process by PID |
| `LIST` | List all processes |
| `QUERY` | Sync request-response to a process (5s timeout) |

---

## Implementation Phases

| Phase | Files | Description |
|-------|-------|-------------|
| 1. Kernel | `src/kernel.zig`, `src/main.zig` | Socket server, client registry, sessions, pub/sub, tool execution, poll loop |
| 2. Driver | `llms/llm-anthropic/run.py` | Socket client, Anthropic streaming API adapter |
| 3. Gateway | `gateways/cli/gateway.py` | Socket client, prompt_toolkit + rich |
| 4. Cleanup | `system_prompt.py`, `tabula.yaml` | Delete bootstrap.py, skills/gateway-cli/, examples/ |

## File Changes

| File | Action |
|------|--------|
| `src/kernel.zig` | Major rewrite |
| `src/main.zig` | Rewrite |
| `src/kernel.tools.json` | Update to Anthropic tool format |
| `llms/llm-anthropic/run.py` | Rewrite |
| `gateways/cli/gateway.py` | New |
| `gateways/cli/requirements.txt` | New |
| `system_prompt.py` | Rename from bootstrap.py |
| `tabula.yaml` | Rewrite |
| `bootstrap.py` | Delete |
| `skills/gateway-cli/` | Delete |
| `examples/simple_agent.py` | Delete |

## Design Decisions

- **History:** stored in driver (Python `messages[]` array). Future: move to kernel for persistence.
- **Memory:** files via EXEC for MVP.
- **No roles:** driver doesn't know about cron, gateway doesn't know about driver. Pure pub/sub.
- **System prompt:** kernel config from `system_prompt.py`, NOT settable by drivers (security).
- **Runtime extensibility:** agent can SPAWN new drivers/gateways that connect via `TABULA_SOCKET`.
