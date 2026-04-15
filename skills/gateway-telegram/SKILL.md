---
name: gateway-telegram
description: Telegram Bot gateway. Bridges Telegram chats to Tabula sessions. Each chat_id gets its own session + driver. Access control via pairing tokens. Before running: check TELEGRAM_BOT_TOKENS is set (cat ~/.tabula/.env), if missing ask user to add their bot token. Run: `python3 skills/gateway-telegram/run.py`. Install as service: `bash skills/gateway-telegram/install-service.sh`
---

# gateway-telegram

Telegram bot gateway for Tabula. Users go through a pairing flow before they can chat.
Supports multiple bot tokens and streaming responses via sendMessageDraft.

## Setup

1. Create a bot via @BotFather, get token
2. Add `TELEGRAM_BOT_TOKENS=xxx` to `~/.tabula/.env`
3. Install as service (see below)

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `TELEGRAM_BOT_TOKENS` | yes | -- | Comma-separated bot tokens |
| `TABULA_URL` | -- | `ws://localhost:8089/ws` | Kernel WebSocket URL |
| `TABULA_PROVIDER` | -- | `anthropic` | LLM provider |
| `TABULA_HOME` | -- | `~/.tabula` | Tabula home directory |

## Run

```bash
python3 skills/gateway-telegram/run.py
```

Or install as a persistent service (recommended):

## Service setup

### macOS (launchd)

```bash
bash skills/gateway-telegram/install-service.sh
```

### Linux (systemd)

```bash
bash skills/gateway-telegram/install-service.sh
```

### Windows (Task Scheduler)

```powershell
powershell -ExecutionPolicy Bypass -File skills/gateway-telegram/install-service.ps1
```

## Pairing flow

1. User writes `/start` to the bot
2. Bot generates a token (`PRX-XXXXXX-YYYYYY`) and sends it to the user
3. Admin approves: `python3 skills/pair/run.py telegram approve PRX-XXXXXX-YYYYYY`
4. User can now chat with the bot

Auth state is stored in `{TABULA_HOME}/auth/telegram.json`.

## pair.py -- access management

```bash
# List authorized users and pending requests
python3 skills/pair/run.py telegram list

# Approve a pairing request
python3 skills/pair/run.py telegram approve PRX-XXXXXX-YYYYYY

# Revoke access
python3 skills/pair/run.py telegram revoke <chat_id>
```

## Sessions

Each `chat_id` gets a dedicated Tabula session (`tg-<chat_id>`) with its own driver.
Conversation history is preserved across messages within the same session.
Sessions are not persisted across gateway restarts (in-memory).

## Streaming

Responses are streamed to Telegram via `sendMessageDraft`:
- While the LLM generates text, the user sees incremental updates with typing indicators
- Draft messages are not saved to chat history
- Once the full response is ready, it's sent as a final `sendMessage`
- Draft updates are throttled at ~100ms to avoid rate limiting

## Limitations

- Messages > 4096 chars are split automatically
- Sessions reset on gateway restart

