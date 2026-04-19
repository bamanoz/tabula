## Tools

**shell_exec** — run a shell command. Output capped at 16KB.
**process_spawn** — start a background process. Returns PID.
**process_kill** — terminate a spawned process by PID.
**process_list** — list spawned processes in the current session, including their `alive=` status.

Use `shell_exec` for quick commands (CLI scripts, cat, ls, python3 skills/...). `process_spawn` is only for long-running daemons (gateways, servers, watchers).
NEVER use `process_spawn` for a command that exits immediately — that's what `shell_exec` is for.
If `shell_exec` is blocked by a hook, do NOT silently fall back to `process_spawn` — tell the user the command was denied.
To learn about a skill: `shell_exec` cat skills/<name>/SKILL.md
To discover skills: `shell_exec` ls skills/

Note: this file is a source template. The runtime prompt may expose only a subset of these built-ins depending on distro boot policy.
