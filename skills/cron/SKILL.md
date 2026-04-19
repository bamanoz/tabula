---
name: cron
description: "Scheduled tasks. Add: `EXEC python3 skills/cron/run.py add --cron \"<expr>\" --task \"<prompt>\"`. One-shot: add `--once`. List: `EXEC python3 skills/cron/run.py list`. Remove: `EXEC python3 skills/cron/run.py remove <id>`. Cron: 5-field (minute hour dom month dow)."
---
# Cron

Schedule recurring tasks. Uses OS crontab when available, falls back to a built-in daemon.

## Commands

### Add a job

```
EXEC python3 skills/cron/run.py add --cron "<expr>" --task "<prompt>" [--id <id>] [--once]
```

- `--cron` — standard 5-field cron expression: `minute hour dom month dow`
- `--task` — the prompt the LLM will receive when the job fires
- `--id` — optional job ID (auto-generated if omitted)
- `--once` — fire once then auto-remove the job

### List jobs

```
EXEC python3 skills/cron/run.py list
```

Returns all scheduled jobs.

### Remove a job

```
EXEC python3 skills/cron/run.py remove <id>
```

## Cron expression examples

| Expression | Meaning |
|------------|---------|
| `* * * * *` | Every minute |
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour |
| `0 9 * * *` | Daily at 9:00 |
| `0 9 * * 1-5` | Weekdays at 9:00 |
| `30 8 1 * *` | 1st of every month at 8:30 |

## How it works

Jobs are stored in `jobs.json` (source of truth).

**With crontab (macOS/Linux):** Each job is also synced to the OS crontab. When cron fires, it calls `run.py fire` which connects to the kernel and sends the task as a message.

**Without crontab (Windows, containers):** A built-in daemon is boot-spawned. It checks jobs every 30 seconds and sends matching tasks to the kernel directly.

In both cases, the LLM receives the task prompt as a message in the `main` session with the job ID in the `id` field.

## Storage Layout

- Jobs file: `~/.tabula/data/cron/jobs.json`

## Notes

- Jobs persist across Tabula restarts (stored in `jobs.json`)
- With crontab, jobs fire even if Tabula isn't running (but the message is lost if kernel is down)
- The daemon mode only runs while Tabula is running
