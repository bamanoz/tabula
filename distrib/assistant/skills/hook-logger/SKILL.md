---
name: hook-logger
description: "Audit logger — logs all hook events to JSONL file"
---
# hook-logger

Subscribes to kernel hook events and writes them to a JSONL audit log.

## Run

```bash
.venv/bin/python3 skills/hook-logger/run.py [--log-file PATH]
```

## Config File

Path:

    ~/.tabula/config/global.toml

Example:

```toml
[hook.logger]
log_file = "/Users/you/.tabula/logs/hook-logger/hooks.jsonl"
```

## Secrets

This skill has no schema-defined secrets.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `log_file` | `string` | -- | no | `TABULA_SKILL_HOOK_LOGGER_LOG_FILE` | `TABULA_LOG_FILE` | Falls back to `{TABULA_HOME}/logs/hook-logger/hooks.jsonl` when unset |

## Runtime Environment

| Variable | Required | Description |
|---|---|---|
| `TABULA_URL` | yes | Kernel WebSocket URL |
| `TABULA_HOME` | yes | Tabula home used to derive the default log path |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/global.toml`
3. schema defaults

## Protocol

- Sends: (nothing — void hooks only)
- Receives: `hook`
- Hooks: `after_message` (void), `after_tool_call` (void), `session_start` (void)

## Environment variables

## Notes

- `--log-file` still overrides the resolved config value for the current process.
