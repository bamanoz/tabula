---
name: memory-admin
description: "Inspect and manage MemPalace contents: list/get/delete drawers, browse wings and rooms. For adding memories use memory-save; for search use memory-search."
tools:
  [
    {
      "name": "memory_list",
      "description": "List drawers, optionally filtered by wing and/or room. Use to find a specific drawer_id for delete or to audit what's stored.",
      "params": {
        "wing": { "type": "string", "description": "Optional wing filter" },
        "room": { "type": "string", "description": "Optional room filter" },
        "limit": { "type": "integer", "description": "Max drawers (default: 20)" },
        "offset": { "type": "integer", "description": "Pagination offset (default: 0)" }
      },
      "required": []
    },
    {
      "name": "memory_get",
      "description": "Fetch a single drawer by id with full content and metadata.",
      "params": {
        "drawer_id": { "type": "string", "description": "Drawer id from list/search" }
      },
      "required": ["drawer_id"]
    },
    {
      "name": "memory_delete",
      "description": "Delete a drawer by id. Irreversible — confirm content with memory_get first.",
      "params": {
        "drawer_id": { "type": "string", "description": "Drawer id to delete" }
      },
      "required": ["drawer_id"]
    },
    {
      "name": "memory_wings",
      "description": "List all wings with drawer counts. Cheap overview of top-level memory structure.",
      "params": {},
      "required": []
    },
    {
      "name": "memory_rooms",
      "description": "List rooms (with counts) under a wing. Helps decide naming conventions when saving.",
      "params": {
        "wing": { "type": "string", "description": "Wing to list rooms for" }
      },
      "required": []
    },
    {
      "name": "memory_status",
      "description": "Palace stats (total drawers, wing/room tallies, palace path) plus the MemPalace memory protocol hint.",
      "params": {},
      "required": []
    }
  ]
---
# memory-admin

Browse and manage the local MemPalace. Read-only tools are safe; `memory_delete` is irreversible.

## When to use

- User asks to forget / remove a memory → `memory_list` to find it, then `memory_delete`.
- Debugging "why doesn't memory recall X" → `memory_list --wing <w>` to inspect what's actually stored.
- Periodic hygiene: `memory_wings` + `memory_rooms` to see if naming is drifting.
- Session start observability: `memory_status` for a one-shot palace summary.
