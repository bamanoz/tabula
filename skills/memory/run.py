#!/usr/bin/env python3
"""
Tabula Memory Skill — persistent memory with semantic search.

Commands: save, search, list, get, delete
Storage: Markdown files + JSON index with optional embeddings.
"""

import argparse
import datetime
import fcntl
import json
import os
import re
import sys
import uuid

SKILL_DIR = os.path.dirname(os.path.abspath(__file__))
TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
DATA_DIR = os.path.join(TABULA_HOME, "memory")

# Ensure sibling modules are importable regardless of cwd
if SKILL_DIR not in sys.path:
    sys.path.insert(0, SKILL_DIR)
INDEX_PATH = os.path.join(DATA_DIR, "index.json")
MEMORY_PATH = os.path.join(DATA_DIR, "MEMORY.md")

CATEGORIES = {"fact", "preference", "decision", "entity", "note"}


# --- Index ---

def load_index() -> dict:
    os.makedirs(DATA_DIR, exist_ok=True)
    if os.path.exists(INDEX_PATH):
        with open(INDEX_PATH, "r") as f:
            return json.load(f)
    return {"version": 1, "entries": []}


def save_index(index: dict):
    os.makedirs(DATA_DIR, exist_ok=True)
    tmp = INDEX_PATH + ".tmp"
    with open(tmp, "w") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        json.dump(index, f, indent=2, ensure_ascii=False)
        f.flush()
        os.fsync(f.fileno())
        fcntl.flock(f, fcntl.LOCK_UN)
    os.replace(tmp, INDEX_PATH)


def find_entry(index: dict, entry_id: str) -> dict | None:
    for e in index["entries"]:
        if e["id"] == entry_id:
            return e
    return None


# --- Markdown I/O ---

def append_to_markdown(filepath: str, entry_id: str, category: str, title: str,
                       tags: list[str], content: str, created: str):
    """Append a memory entry to a markdown file."""
    os.makedirs(os.path.dirname(filepath), exist_ok=True)

    tag_str = ", ".join(tags) if tags else ""
    meta = f"<!-- id: {entry_id}, created: {created}"
    if tag_str:
        meta += f", tags: {tag_str}"
    meta += " -->"

    block = f"\n## [{category}] {title}\n{meta}\n\n{content}\n"

    with open(filepath, "a") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        f.write(block)
        fcntl.flock(f, fcntl.LOCK_UN)


def read_entry_from_file(filepath: str, entry_id: str) -> str | None:
    """Read a single entry's content from a markdown file by its id."""
    if not os.path.exists(filepath):
        return None

    with open(filepath, "r") as f:
        text = f.read()

    # Split by ## headers
    pattern = rf"(## \[.*?\].*?\n<!-- id: {re.escape(entry_id)}.*?-->.*?)(?=\n## |\Z)"
    match = re.search(pattern, text, re.DOTALL)
    if match:
        return match.group(1).strip()
    return None


def delete_entry_from_file(filepath: str, entry_id: str) -> bool:
    """Remove an entry from a markdown file. Returns True if found and removed."""
    if not os.path.exists(filepath):
        return False

    with open(filepath, "r") as f:
        text = f.read()

    pattern = rf"\n?## \[.*?\].*?\n<!-- id: {re.escape(entry_id)}.*?-->.*?(?=\n## |\Z)"
    new_text, count = re.subn(pattern, "", text, flags=re.DOTALL)

    if count == 0:
        return False

    with open(filepath, "w") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        f.write(new_text)
        fcntl.flock(f, fcntl.LOCK_UN)
    return True


def daily_file() -> str:
    """Path to today's daily markdown file."""
    date = datetime.date.today().isoformat()
    return os.path.join(DATA_DIR, f"{date}.md")


def all_markdown_files() -> list[str]:
    """List all markdown files in data directory."""
    os.makedirs(DATA_DIR, exist_ok=True)
    return [
        os.path.join(DATA_DIR, f)
        for f in os.listdir(DATA_DIR)
        if f.endswith(".md")
    ]


# --- Keyword search ---

def keyword_search(query: str, entries: list[dict]) -> list[tuple[dict, float]]:
    """Simple keyword matching. Returns (entry, score) pairs."""
    words = query.lower().split()
    if not words:
        return []

    results = []
    for entry in entries:
        text = f"{entry['title']} {' '.join(entry.get('tags', []))} {entry.get('category', '')}".lower()

        # Also read content from file for deeper matching
        filepath = os.path.join(DATA_DIR, entry["file"])
        content = read_entry_from_file(filepath, entry["id"])
        if content:
            text += " " + content.lower()

        matches = sum(1 for w in words if w in text)
        if matches > 0:
            score = matches / len(words)
            results.append((entry, score))

    results.sort(key=lambda x: x[1], reverse=True)
    return results


# --- Commands ---

def cmd_save(args):
    import embeddings  # lazy import (same directory)

    category = args.category
    if category not in CATEGORIES:
        print(json.dumps({"error": f"invalid category: {category}. Must be one of: {', '.join(sorted(CATEGORIES))}"}))
        sys.exit(1)

    title = args.title
    content = args.content
    tags = [t.strip() for t in args.tags.split(",") if t.strip()] if args.tags else []
    long_term = args.long_term

    entry_id = str(uuid.uuid4())[:8]
    created = datetime.datetime.now(datetime.timezone.utc).isoformat()

    # Choose file
    if long_term:
        filepath = MEMORY_PATH
        filename = "MEMORY.md"
    else:
        filepath = daily_file()
        filename = os.path.basename(filepath)

    # Write markdown
    append_to_markdown(filepath, entry_id, category, title, tags, content, created)

    # Update index
    index = load_index()
    entry = {
        "id": entry_id,
        "title": title,
        "category": category,
        "tags": tags,
        "file": filename,
        "created": created,
    }

    # Embed if available
    vec = embeddings.embed_one(f"{title}\n{content}")
    if vec:
        entry["embedding"] = vec

    index["entries"].append(entry)
    save_index(index)

    print(json.dumps({"ok": True, "id": entry_id, "file": filename}))


def cmd_search(args):
    import embeddings  # lazy import (same directory)

    query = args.query
    limit = args.limit or 10

    index = load_index()
    entries = index["entries"]

    if not entries:
        print(json.dumps({"results": []}))
        return

    results = []

    # Semantic search
    has_embeddings = any("embedding" in e for e in entries)
    if embeddings.available() and has_embeddings:
        query_vec = embeddings.embed_one(query)
        if query_vec:
            for entry in entries:
                if "embedding" in entry:
                    score = embeddings.cosine_similarity(query_vec, entry["embedding"])
                    results.append((entry, score, "semantic"))

    # Keyword search
    kw_results = keyword_search(query, entries)

    # Merge: if we have semantic results, blend 70/30
    if results:
        semantic_map = {r[0]["id"]: r[1] for r in results}
        keyword_map = {r[0]["id"]: r[1] for r in kw_results}

        all_ids = set(semantic_map.keys()) | set(keyword_map.keys())
        merged = []
        for eid in all_ids:
            sem = semantic_map.get(eid, 0.0)
            kw = keyword_map.get(eid, 0.0)
            combined = 0.7 * sem + 0.3 * kw
            entry = find_entry(index, eid)
            if entry:
                merged.append((entry, combined))
        merged.sort(key=lambda x: x[1], reverse=True)
        final = merged[:limit]
    else:
        final = kw_results[:limit]

    output = []
    for entry, score in final:
        filepath = os.path.join(DATA_DIR, entry["file"])
        content = read_entry_from_file(filepath, entry["id"])
        output.append({
            "id": entry["id"],
            "title": entry["title"],
            "category": entry.get("category", ""),
            "tags": entry.get("tags", []),
            "score": round(score, 3),
            "snippet": (content[:300] + "...") if content and len(content) > 300 else content,
        })

    print(json.dumps({"results": output}, ensure_ascii=False))


def cmd_list(args):
    index = load_index()
    entries = index["entries"]

    if args.category:
        entries = [e for e in entries if e.get("category") == args.category]

    if args.date:
        filename = f"{args.date}.md"
        entries = [e for e in entries if e.get("file") == filename]

    limit = args.limit or 50
    entries = entries[-limit:]

    output = []
    for entry in entries:
        output.append({
            "id": entry["id"],
            "title": entry["title"],
            "category": entry.get("category", ""),
            "tags": entry.get("tags", []),
            "file": entry.get("file", ""),
            "created": entry.get("created", ""),
        })

    print(json.dumps({"entries": output}, ensure_ascii=False))


def cmd_get(args):
    index = load_index()
    entry = find_entry(index, args.id)

    if not entry:
        print(json.dumps({"error": f"not found: {args.id}"}))
        sys.exit(1)

    filepath = os.path.join(DATA_DIR, entry["file"])
    content = read_entry_from_file(filepath, entry["id"])

    print(json.dumps({
        "id": entry["id"],
        "title": entry["title"],
        "category": entry.get("category", ""),
        "tags": entry.get("tags", []),
        "file": entry.get("file", ""),
        "created": entry.get("created", ""),
        "content": content,
    }, ensure_ascii=False))


def cmd_delete(args):
    index = load_index()
    entry = find_entry(index, args.id)

    if not entry:
        print(json.dumps({"error": f"not found: {args.id}"}))
        sys.exit(1)

    # Remove from file
    filepath = os.path.join(DATA_DIR, entry["file"])
    delete_entry_from_file(filepath, entry["id"])

    # Remove from index
    index["entries"] = [e for e in index["entries"] if e["id"] != args.id]
    save_index(index)

    print(json.dumps({"ok": True, "deleted": args.id}))


def main():
    parser = argparse.ArgumentParser(prog="memory", description="Tabula memory skill")
    sub = parser.add_subparsers(dest="command", required=True)

    # save
    p_save = sub.add_parser("save")
    p_save.add_argument("--category", required=True, choices=sorted(CATEGORIES))
    p_save.add_argument("--tags", default="")
    p_save.add_argument("--title", required=True)
    p_save.add_argument("--long-term", action="store_true")
    p_save.add_argument("content")

    # search
    p_search = sub.add_parser("search")
    p_search.add_argument("query")
    p_search.add_argument("--limit", type=int, default=10)

    # list
    p_list = sub.add_parser("list")
    p_list.add_argument("--category", choices=sorted(CATEGORIES))
    p_list.add_argument("--date")
    p_list.add_argument("--limit", type=int, default=50)

    # get
    p_get = sub.add_parser("get")
    p_get.add_argument("id")

    # delete
    p_delete = sub.add_parser("delete")
    p_delete.add_argument("id")

    args = parser.parse_args()

    if args.command == "save":
        cmd_save(args)
    elif args.command == "search":
        cmd_search(args)
    elif args.command == "list":
        cmd_list(args)
    elif args.command == "get":
        cmd_get(args)
    elif args.command == "delete":
        cmd_delete(args)


if __name__ == "__main__":
    main()
