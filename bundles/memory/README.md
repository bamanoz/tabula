# Memory skills for Tabula

Persistent memory backed by [MemPalace](https://github.com/mempalace/mempalace) — local ChromaDB + SQLite knowledge graph, no API key required.

## Skills

| Directory | Type | What it does |
|-----------|------|--------------|
| `memory-save/` | tool | File a verbatim memory into a wing/room |
| `memory-search/` | tool | Semantic + BM25 search, plus `wake-up` context summary |
| `memory-admin/` | tool | `list` / `get` / `delete` drawers, browse wings/rooms, `status` |

All three wrap `mempalace.mcp_server.tool_*` via `_lib.py`. Storage lives at `$TABULA_HOME/data/memory/palace/`.

## Why three skills, not one

MemPalace exposes 9+ operations. Splitting them by intent (write / read / admin) keeps each `SKILL.md` frontmatter focused and gives the agent a clear signal about which operation to invoke without having to page through a monolithic SKILL.md.

## Upstream

MemPalace is consumed as a `pip` dependency (see `scripts/requirements-runtime.txt`). This bundle is thin — no upstream files are vendored.
