---
name: memory
description: "Persistent memory. Save: `EXEC python3 skills/memory/run.py save --category <cat> --title \"<title>\" \"<text>\"`. Add `--long-term` to inject into system prompt. Search: `EXEC python3 skills/memory/run.py search \"<query>\"`. Also: `list --category <cat>`, `get <id>`, `delete <id>`. Full docs: `EXEC cat skills/memory/SKILL.md`"
---
# Memory

Persistent memory system. Save facts, preferences, decisions, and notes across sessions.

## Run

This skill is usually invoked directly from `EXEC`:

```bash
python3 skills/memory/run.py save --category fact --title "Title" "Content"
python3 skills/memory/run.py search "query"
```

## Config File

Path:

    ~/.tabula/config/global.toml

Example:

```toml
[memory.embedding]
api_key = { source = "store", id = "driver-openai.api_key" }
model = "text-embedding-3-small"
base_url = "https://api.openai.com"
```

## Secrets

Path:

    ~/.tabula/secrets.json

The embedding loader checks `memory.embedding_api_key` first, then shared
`driver-openai.api_key`.

## Configuration

| Key | Type | Default | Secret | Canonical env | Aliases | Notes |
|---|---|---|---|---|---|---|
| `embedding.api_key` | `string` | `""` | yes | `TABULA_SKILL_MEMORY_EMBEDDING_API_KEY` | `TABULA_EMBEDDING_API_KEY`, `OPENAI_API_KEY` | Store fallback order: `memory.embedding_api_key`, then `driver-openai.api_key` |
| `embedding.model` | `string` | `text-embedding-3-small` | no | `TABULA_SKILL_MEMORY_EMBEDDING_MODEL` | `TABULA_EMBEDDING_MODEL` | Embedding model used for semantic search |
| `embedding.base_url` | `string` | `https://api.openai.com` | no | `TABULA_SKILL_MEMORY_EMBEDDING_BASE_URL` | `OPENAI_BASE_URL` | OpenAI-compatible embeddings base URL |

## Runtime Environment

| Variable | Required | Description |
|---|---|---|
| `TABULA_HOME` | yes | Tabula home used for `memory/` storage |

## Precedence

1. env (`TABULA_SKILL_*`, then legacy alias)
2. `~/.tabula/config/global.toml`
3. `~/.tabula/secrets.json` for `embedding.api_key`
4. schema defaults

## Storage Layout

- Long-term prompt memory: `~/.tabula/data/memory/MEMORY.md`
- Daily memory files: `~/.tabula/data/memory/<date>.md`
- Search index: `~/.tabula/state/memory/index.json`

## Commands

All commands are invoked via EXEC:

### Save a memory

```
EXEC python3 skills/memory/run.py save --category <category> [--tags "tag1,tag2"] [--long-term] --title "Title" "Content of the memory"
```

Categories: `fact`, `preference`, `decision`, `entity`, `note`

- Default: saves to today's daily file (short-term)
- `--long-term`: saves to `data/memory/MEMORY.md` (persistent across days)

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
