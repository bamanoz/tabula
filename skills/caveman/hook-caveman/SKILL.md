---
name: hook-caveman
description: "Caveman mode hook — auto-activates on session start, tracks /caveman commands"
---
# hook-caveman

Subscribes to kernel hooks to manage caveman mode:

- **session_start**: injects caveman rules into system prompt (if mode is active)
- **before_message**: detects `/caveman` commands and switches mode

State stored in `~/.tabula/caveman-mode.json` (per-session modes).

## Configuration

Default mode via environment variable:
```
CAVEMAN_DEFAULT_MODE=full  # or: lite, ultra, off
```

Or config file `~/.config/caveman/config.json`:
```json
{ "defaultMode": "full" }
```

Set `off` to disable auto-activation.
