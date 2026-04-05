# Memory

Persistent memory system. Save facts, preferences, decisions, and notes across sessions.

## Commands

All commands are invoked via EXEC:

### Save a memory

```
EXEC python3 skills/memory/run.py save --category <category> [--tags "tag1,tag2"] [--long-term] --title "Title" "Content of the memory"
```

Categories: `fact`, `preference`, `decision`, `entity`, `note`

- Default: saves to today's daily file (short-term)
- `--long-term`: saves to MEMORY.md (persistent across days)

### Search memories

```
EXEC python3 skills/memory/run.py search "query text"
```

Returns top matches ranked by relevance. Uses semantic search when available, falls back to keyword matching.

### List memories

```
EXEC python3 skills/memory/run.py list [--category fact] [--date 2026-04-05] [--limit 20]
```

### Get a specific memory

```
EXEC python3 skills/memory/run.py get <id>
```

### Delete a memory

```
EXEC python3 skills/memory/run.py delete <id>
```

## When to use

- **Save** when the user shares preferences, makes decisions, states facts about themselves or the project, or explicitly asks you to remember something.
- **Search** before answering questions about past conversations, user preferences, or project history.
- **Cite** the memory entry when using recalled information in your response.

## Categories guide

| Category | Use for |
|----------|---------|
| `fact` | Objective information: "project uses Zig", "API key is in vault" |
| `preference` | User preferences: "prefers tabs over spaces", "likes concise answers" |
| `decision` | Architectural/design decisions: "chose PostgreSQL over SQLite" |
| `entity` | People, teams, services: "Alice is the tech lead" |
| `note` | General notes, session summaries, TODOs |
