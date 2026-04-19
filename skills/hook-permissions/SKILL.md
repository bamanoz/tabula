---
name: hook-permissions
description: "Permission enforcement via before_tool_call hook"
---
# hook-permissions

Enforces tool permission rules defined in `~/.tabula/config/skills/hook-permissions/permissions.json`.

## How it works

Subscribes to `before_tool_call` with high priority (100). For each tool call,
evaluates rules and blocks denied operations before they execute.

## Configuration

Create `~/.tabula/config/skills/hook-permissions/permissions.json`:

```json
{
  "rules": [
    {"tool": "EXEC", "command": "rm -rf *", "effect": "deny"},
    {"tool": "EXEC", "command": "git push *--force*", "effect": "deny"},
    {"tool": "write_file", "effect": "deny"},
    {"tool": "*", "effect": "allow"}
  ]
}
```

### Rule fields

- `tool` — tool name or glob pattern (`EXEC`, `write_*`, `*`)
- `command` — glob pattern for EXEC/SPAWN command field (optional)
- `effect` — `allow` or `deny`

### Evaluation

Specificity wins: command-level rules override tool-level rules.
Within the same specificity, deny overrides allow.
No rules file or empty rules = allow all.

### EXEC allowlist example

Allow only specific commands, deny everything else:

```json
{
  "rules": [
    {"tool": "EXEC", "command": "git *", "effect": "allow"},
    {"tool": "EXEC", "command": "go *", "effect": "allow"},
    {"tool": "EXEC", "command": "python3 skills/*", "effect": "allow"},
    {"tool": "EXEC", "effect": "deny"},
    {"tool": "*", "effect": "allow"}
  ]
}
```

## Storage Layout

- Permission rules: `~/.tabula/config/skills/hook-permissions/permissions.json`

`boot.py` reads the same file to decide whether `hook-permissions` should be
spawned and to filter fully denied tools from the prompt.
