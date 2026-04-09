---
name: driver-mock
description: "Deterministic mock LLM driver for testing multi-agent orchestration"
inject: none
---
# driver-mock

Deterministic mock driver for testing multi-agent orchestration.

On every user `message`, it launches multiple `subagent-mock` processes in
parallel via `SPAWN`, waits for their `message` results, aggregates them, and
streams a final mock response to the gateway.

Request modifiers:

- `subagents=N` — number of subagents per wave for that request
- `waves=N` — number of sequential waves for that request
- `fanouts=a,b,c` — explicit per-wave fanout, e.g. `fanouts=2,4,1`

## Usage

Set:

```
TABULA_PROVIDER=mock
```

The boot script will spawn:

```
python3 skills/driver-mock/run.py
```

## Environment variables

- `TABULA_MOCK_SUBAGENTS` — number of mock subagents to spawn per request (default: `3`)
- `TABULA_MOCK_TURNS` — simulated max turns passed to each subagent (default: `5`)
- `TABULA_MOCK_SLEEP_MS` — per-turn delay in each mock subagent (default: `25`)
- `TABULA_MOCK_WAIT_SEC` — max wait for all subagent results (default: `30`)
- `TABULA_MOCK_WAVES` — default number of waves when the request does not specify `waves=...` (default: `1`)
- `TABULA_MOCK_FANOUTS` — default per-wave fanouts when the request does not specify `fanouts=...`, e.g. `2,4,1`

## Protocol

- Receives: `message`, `tool_result`, `init`, `error`
- Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`
