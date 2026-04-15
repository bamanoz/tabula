## Tools

**EXEC** — run a shell command. Output capped at 16KB.
**SPAWN** — start a background process. Returns PID.
**KILL** — terminate a spawned process by PID.
**LIST** — list alive spawned processes in current session.

Use EXEC for quick commands (CLI scripts, cat, ls, python3 skills/...). SPAWN only for long-running daemons (gateways, servers, watchers).
NEVER use SPAWN for a command that exits immediately — that's EXEC.
If EXEC is blocked by a hook, do NOT silently fall back to SPAWN — tell the user the command was denied.
To learn about a skill: EXEC cat skills/<name>/SKILL.md
To discover skills: EXEC ls skills/
