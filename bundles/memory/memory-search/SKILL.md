---
name: memory-search
description: "Search and recall from the local MemPalace. Use BEFORE answering questions about past context, user preferences, project history, or people."
tools:
  [
    {
      "name": "memory_search",
      "description": "Find memories by hybrid semantic + BM25 search. Optionally filter by wing and/or room. Call this BEFORE answering questions about the user, past decisions, or project history.",
      "params": {
        "query": { "type": "string", "description": "Natural-language query" },
        "wing": { "type": "string", "description": "Optional wing filter" },
        "room": { "type": "string", "description": "Optional room filter" },
        "limit": { "type": "integer", "description": "Max results (default: 5)" }
      },
      "required": ["query"]
    },
    {
      "name": "memory_wake_up",
      "description": "Compact context summary of the palace: total drawers, wing list with counts, and (optionally) rooms within a single wing. Use at session start to know what memory topics exist without paging through everything.",
      "params": {
        "wing": { "type": "string", "description": "Optional wing to expand with its rooms" }
      },
      "required": []
    }
  ]
---
# memory-search

Read-side tools for the local MemPalace.

## memory_search

Hybrid semantic + BM25 search across all drawers. Results include the verbatim `text`, `wing`, `room`, `similarity` (0–1), and `created_at`. Use before answering any question that might depend on prior context — `similarity` < 0.2 is usually noise, > 0.5 is a strong match.

## memory_wake_up

Cheap overview of the palace shape. Call at the start of a session (or when you're unsure what memory exists) to decide which wing/room to search next. Passing `wing` expands rooms under that wing.

## When to use

- Before answering questions about user identity, preferences, or past decisions.
- When the user references something from a previous session.
- When you need project history or architectural context.
- Cite the drawer (wing/room + brief content) when using recalled information.
