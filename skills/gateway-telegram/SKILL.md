---
name: gateway-telegram
description: Telegram Bot gateway. Bridges Telegram chats to Tabula sessions. Each chat_id gets its own session + driver. Access control via pairing tokens.
---

# gateway-telegram

Telegram bot gateway for Tabula. Users go through a pairing flow before they can chat.

## Setup

1. Create a bot via @BotFather, get token
2. Add `TELEGRAM_BOT_TOKEN=xxx` to `~/.tabula/.env`
3. Run the gateway (see below)

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | yes | -- | Bot token from @BotFather |
| `TABULA_URL` | -- | `ws://localhost:8089/ws` | Kernel WebSocket URL |
| `TABULA_PROVIDER` | -- | `anthropic` | LLM provider |
| `TABULA_HOME` | -- | `~/.tabula` | Tabula home directory |

## Run

```bash
python3 skills/gateway-telegram/run.py
```

Or via service (see service files in this directory).

## Pairing flow

1. User writes `/start` to the bot
2. Bot generates a token (`PRX-XXXXXX-YYYYYY`) and sends it to the user
3. Admin approves: `python3 skills/gateway-telegram/pair.py approve PRX-XXXXXX-YYYYYY`
4. User can now chat with the bot

Auth state is stored in `~/.tabula/telegram_auth.json`.

## pair.py -- access management

```bash
# List authorized users and pending requests
python3 skills/gateway-telegram/pair.py list

# Approve a pairing request
python3 skills/gateway-telegram/pair.py approve PRX-XXXXXX-YYYYYY

# Revoke access
python3 skills/gateway-telegram/pair.py revoke <chat_id>
```

## Sessions

Each `chat_id` gets a dedicated Tabula session (`tg-<chat_id>`) with its own driver.
Conversation history is preserved across messages within the same session.
Sessions are not persisted across gateway restarts (in-memory).

## Limitations

- No streaming to Telegram (messages sent after full response)
- Messages > 4096 chars are split automatically
- Sessions reset on gateway restart

## Service setup

### macOS (launchd)

```bash
sed "s|__TABULA_HOME__|$HOME/.tabula|g" skills/gateway-telegram/com.tabula.gateway-telegram.plist \
  > ~/Library/LaunchAgents/com.tabula.gateway-telegram.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.tabula.gateway-telegram.plist
```

### Linux (systemd)

```bash
sed "s|__TABULA_HOME__|$HOME/.tabula|g; s|__USER__|$(whoami)|g" \
  skills/gateway-telegram/gateway-telegram.service \
  > ~/.config/systemd/user/gateway-telegram.service
systemctl --user daemon-reload
systemctl --user enable --now gateway-telegram
```
