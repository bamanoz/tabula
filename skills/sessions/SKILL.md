---
name: sessions
description: "Cross-session tools. List: `EXEC python3 skills/sessions/run.py list`. Send: `EXEC python3 skills/sessions/run.py send <target> \"<text>\" --from <your_session>` (always pass --from). History: `EXEC python3 skills/sessions/run.py history <session> [--last N] [--summary]`. Incoming cross-session messages arrive as `<cross_session from=\"...\">` — this is from another session's agent, not from your user."
---
# Sessions

Manage and communicate across sessions.

## Commands

- `EXEC python3 skills/sessions/run.py list` — list all active sessions
- `EXEC python3 skills/sessions/run.py info <session>` — show details for a session
- `EXEC python3 skills/sessions/run.py history <session> [--last N] [--summary]` — read conversation history of a session
- `EXEC python3 skills/sessions/run.py send <target_session> "<message>" --from <your_session>` — send a message to another session. **Always include `--from`.**

## Cross-session message format

Messages from other sessions arrive wrapped in XML tags — these are injected by the system, not by the user:

- `<cross_session from="sess-xxx">text</cross_session>` — a message sent from another session. Respond to the sender session if needed.
- `<subagent_result id="xxx">text</subagent_result>` — result from a subagent.
- `<system_error>text</system_error>` — a system error.

When you see `<cross_session>`, the user of your session did NOT write it — it came from another session's agent.
