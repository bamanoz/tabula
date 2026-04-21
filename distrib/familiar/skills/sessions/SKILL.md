---
name: sessions
description: "Cross-session tools. List: `EXEC python3 skills/sessions/run.py list`. Send: `EXEC python3 skills/sessions/run.py send <target> \"<text>\" --from <your_session>` (always pass --from). History: `EXEC python3 skills/sessions/run.py history <session> [--last N] [--summary]`. Incoming cross-session messages arrive as `<cross_session from=\"...\">` — this is from another session's agent, not from your user."
requires-kernel-tools: ["shell_exec"]
---
# Sessions

Manage and communicate across sessions.

## Run

This skill is usually invoked directly from `EXEC` or spawned as a daemon:

```bash
python3 skills/sessions/run.py list
python3 skills/sessions/run.py daemon
```

## Config File

Path:

    ~/.tabula/config/global.toml

Example:

```toml
[sessions]
idle_timeout = 300
poll_interval = 2
```

## Secrets

This skill has no schema-defined secrets.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `idle_timeout` | `int` | `300` | no | `TABULA_SKILL_SESSIONS_IDLE_TIMEOUT` | `TABULA_SESSION_IDLE_SEC` | Idle threshold in seconds for daemon lifecycle events |
| `poll_interval` | `float` | `2` | no | `TABULA_SKILL_SESSIONS_POLL_INTERVAL` | `TABULA_SESSION_POLL_SEC` | Poll interval in seconds for daemon mode |

## Runtime Environment

| Variable | Required | Description |
|---|---|---|
| `TABULA_URL` | yes | Kernel WebSocket / HTTP base source |
| `TABULA_HOME` | yes | Tabula home used for history and daemon log files |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/global.toml`
3. schema defaults

## Commands

- `EXEC python3 skills/sessions/run.py list` — list all active sessions
- `EXEC python3 skills/sessions/run.py info <session>` — show details for a session
- `EXEC python3 skills/sessions/run.py history <session> [--last N] [--summary]` — read conversation history of a session
- `EXEC python3 skills/sessions/run.py send <target_session> "<message>" --from <your_session>` — send a message to another session. **Always include `--from`.**

## Storage Layout

- Session histories: `~/.tabula/data/sessions/<session>/history.jsonl`
- Daemon log: `~/.tabula/logs/sessions/daemon.log`

## Cross-session message format

Messages from other sessions arrive wrapped in XML tags — these are injected by the system, not by the user:

- `<cross_session from="sess-xxx">text</cross_session>` — a message sent from another session. Respond to the sender session if needed.
- `<subagent_result id="xxx">text</subagent_result>` — result from a subagent.
- `<system_error>text</system_error>` — a system error.

When you see `<cross_session>`, the user of your session did NOT write it — it came from another session's agent.
