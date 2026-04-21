---
name: pair
description: "Manage gateway access: approve/revoke pairing. Usage: /pair <gateway> approve <token> | revoke <user_id> | list"
user-invocable: true
---

# pair

Universal access management for Tabula gateways (Telegram, Discord, etc.).

## Actions

**List authorized users and pending requests:**
```
/pair telegram list
```
Run: `python3 skills/pair/run.py telegram list`

**Approve a pairing request:**
```
/pair telegram approve PRX-XXXXXX-YYYYYY
```
Run: `python3 skills/pair/run.py telegram approve PRX-XXXXXX-YYYYYY`

**Revoke access:**
```
/pair discord revoke 123456789
```
Run: `python3 skills/pair/run.py discord revoke 123456789`

## Instructions

1. Parse gateway name and action from "User request"
2. Execute the corresponding command via EXEC tool (not SPAWN — this is a quick CLI, not a daemon)
3. Show the result to the user

## Storage Layout

- Pairing state: `~/.tabula/data/pair/<gateway>.json`
