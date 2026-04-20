---
name: memory-save
description: "Save a verbatim memory into the local MemPalace (ChromaDB + SQLite, no API key)."
tools:
  [
    {
      "name": "memory_save",
      "description": "File a verbatim memory ('drawer') into a wing/room of the local MemPalace. Wing = project/topic bucket (e.g. 'tabula', 'personal', 'people'). Room = sub-topic (e.g. 'decisions', 'preferences', 'facts'). Duplicate content (same wing+room+text) is auto-deduped. Use when the user shares a preference, decision, fact, or asks to remember something.",
      "params": {
        "wing": { "type": "string", "description": "Project/topic bucket (e.g. tabula, personal, people)" },
        "room": { "type": "string", "description": "Sub-topic within the wing (e.g. decisions, preferences, facts)" },
        "content": { "type": "string", "description": "Verbatim text to remember" },
        "source": { "type": "string", "description": "Optional source file/path tag" }
      },
      "required": ["wing", "room", "content"]
    }
  ]
---
# memory-save

File a verbatim memory ("drawer") into a wing/room of the local MemPalace.

## Storage

`$TABULA_HOME/data/memory/palace/` — ChromaDB + SQLite knowledge graph. All data stays local, no API key.

## Wing/room conventions

Pick names that group related memories. These are conventions, not schema.

| Wing | Typical rooms |
|---|---|
| `<project>` (e.g. `tabula`) | `decisions`, `facts`, `architecture`, `todos` |
| `personal` | `preferences`, `identity`, `schedule` |
| `people` | one room per person, named by handle |

## When to save

- User shares a preference, decision, fact, or asks you to remember something.
- A non-trivial discovery is made about the project that future sessions should know.
- After completing a task, store a brief outcome summary in `<project>/decisions`.

## Output

```json
{"success": true, "drawer_id": "drawer_<wing>_<room>_<hash>", "wing": "...", "room": "..."}
```

If the same content already exists in the same wing/room:

```json
{"success": true, "reason": "already_exists", "drawer_id": "..."}
```
