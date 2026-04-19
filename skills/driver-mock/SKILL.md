---
name: driver-mock
description: "Deterministic mock LLM driver for testing multi-agent orchestration"
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

## Run

Set:

```bash
TABULA_PROVIDER=mock
```

The boot script will spawn:

```bash
python3 skills/driver-mock/run.py
```

## Config File

Path:

    ~/.tabula/config/skills/driver-mock.toml

Example:

```toml
subagent_count = 3
max_turns = 5
sleep_ms = 25
default_waves = 1
# default_fanouts = [2, 4, 1]
```

## Secrets

This skill has no dedicated secrets.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `subagent_count` | `int` | `3` | no | `TABULA_SKILL_DRIVER_MOCK_SUBAGENT_COUNT` | `TABULA_MOCK_SUBAGENTS` | Default number of subagents per wave |
| `max_turns` | `int` | `5` | no | `TABULA_SKILL_DRIVER_MOCK_MAX_TURNS` | `TABULA_MOCK_TURNS` | Passed through to each `subagent-mock` |
| `sleep_ms` | `int` | `25` | no | `TABULA_SKILL_DRIVER_MOCK_SLEEP_MS` | `TABULA_MOCK_SLEEP_MS` | Per-turn mock subagent delay |
| `default_waves` | `int` | `1` | no | `TABULA_SKILL_DRIVER_MOCK_DEFAULT_WAVES` | `TABULA_MOCK_WAVES` | Used when request omits `waves=...` |
| `default_fanouts` | `int_list` | -- | no | `TABULA_SKILL_DRIVER_MOCK_DEFAULT_FANOUTS` | `TABULA_MOCK_FANOUTS` | Example: `2,4,1` |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/skills/driver-mock.toml`
3. schema defaults

## Protocol

- Receives: `message`, `tool_result`, `init`, `error`
- Sends: `stream_start`, `stream_delta`, `stream_end`, `tool_use`, `done`
