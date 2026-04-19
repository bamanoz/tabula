---
name: timer
description: "Timer/reminder. Usage: `SPAWN python3 skills/timer/run.py -s <seconds> -m \"<message>\"`. Sends message to chat after delay. Lightweight (no LLM), connects to kernel directly."
requires-kernel-tools: ["process_spawn"]
---
# Timer

Send a message to the chat session after a delay. Lightweight alternative to
cron for short timers (seconds/minutes). Connects directly to the kernel via
WebSocket — no LLM or subagent needed.

## Usage

    SPAWN python3 skills/timer/run.py -s <seconds> -m "<message>"

## Arguments

| Arg | Required | Description |
|-----|----------|-------------|
| `--seconds` / `-s` | Yes | Delay in seconds before sending |
| `--message` / `-m` | Yes | Message text to send |
| `--session` | No | Target session (default: `main`) |

## Examples

Remind in 30 seconds:

    SPAWN python3 skills/timer/run.py -s 30 -m "🍽️ Пора убрать тарелки!"

Remind in 5 minutes:

    SPAWN python3 skills/timer/run.py -s 300 -m "☕ Перерыв окончен!"

## How it works

1. `sleep(seconds)` — waits the specified delay
2. Connects to kernel via WebSocket (`TABULA_URL`)
3. Joins the target session
4. Sends the message
5. Exits

No LLM calls, no tokens spent. Just a direct message to the kernel.

## When to use what

| Need | Tool |
|------|------|
| Seconds/minutes, one-shot | **timer** ✅ |
| Recurring schedule (daily, hourly) | **cron** |
| Complex reminder with AI reasoning | **subagent** |
