---
name: files
description: "Read, search, and edit text files. Use read before mutating existing files. Use glob and grep to find targets, edit or multiedit for exact replacements, write for full rewrites, and apply_patch for multi-file changes."
tools:
  [
    {
      "name": "read",
      "description": "Read a UTF-8 text file with line numbers. Supports pagination with offset and limit.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "offset": { "type": "integer", "description": "Start line (1-based). Default: 1" },
        "limit": { "type": "integer", "description": "Max lines to return. Default: 2000" }
      },
      "required": ["path"]
    },
    {
      "name": "list_dir",
      "description": "List directory entries. Supports pagination and bounded recursion depth.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative directory path" },
        "offset": { "type": "integer", "description": "Start entry (1-based). Default: 1" },
        "limit": { "type": "integer", "description": "Max entries to return. Default: 200" },
        "depth": { "type": "integer", "description": "Traversal depth. Default: 1" }
      },
      "required": ["path"]
    },
    {
      "name": "glob",
      "description": "Find files by glob pattern using ripgrep's file walker.",
      "params": {
        "pattern": { "type": "string", "description": "Glob pattern to match" },
        "path": { "type": "string", "description": "Directory to search in. Default: current working directory" },
        "offset": { "type": "integer", "description": "Start result (1-based). Default: 1" },
        "limit": { "type": "integer", "description": "Max paths to return. Default: 100" }
      },
      "required": ["pattern"]
    },
    {
      "name": "grep",
      "description": "Search file contents with ripgrep. Returns matching lines, matching files, or per-file counts.",
      "params": {
        "pattern": { "type": "string", "description": "Regex pattern to search for" },
        "path": { "type": "string", "description": "Directory or file to search. Default: current working directory" },
        "include": { "type": "string", "description": "Optional glob filter such as *.py or *.{ts,tsx}" },
        "mode": { "type": "string", "description": "One of: lines, files, count. Default: lines" },
        "offset": { "type": "integer", "description": "Start result (1-based). Default: 1" },
        "limit": { "type": "integer", "description": "Max results to return. Default: 100" },
        "ignore_case": { "type": "boolean", "description": "Case-insensitive search. Default: false" },
        "multiline": { "type": "boolean", "description": "Enable multiline search. Default: false" }
      },
      "required": ["pattern"]
    },
    {
      "name": "write",
      "description": "Create a new file or fully overwrite an existing text file. Existing files must be read first in the current session.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "content": { "type": "string", "description": "Complete file content" }
      },
      "required": ["path", "content"]
    },
    {
      "name": "edit",
      "description": "Replace an exact string in an existing file. Fails if the target text is missing or ambiguous without replace_all.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "old_string": { "type": "string", "description": "Exact text to find" },
        "new_string": { "type": "string", "description": "Replacement text" },
        "replace_all": { "type": "boolean", "description": "Replace all occurrences. Default: false" }
      },
      "required": ["path", "old_string", "new_string"]
    },
    {
      "name": "multiedit",
      "description": "Apply multiple exact string replacements to one file atomically.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "edits": {
          "type": "array",
          "description": "Sequential edit operations to apply",
          "items": {
            "type": "object",
            "properties": {
              "old_string": { "type": "string", "description": "Exact text to find" },
              "new_string": { "type": "string", "description": "Replacement text" },
              "replace_all": { "type": "boolean", "description": "Replace all occurrences. Default: false" }
            },
            "required": ["old_string", "new_string"]
          }
        }
      },
      "required": ["path", "edits"]
    },
    {
      "name": "apply_patch",
      "description": "Apply a multi-file patch using the *** Begin Patch / *** End Patch format.",
      "params": {
        "patch_text": { "type": "string", "description": "Full patch text to apply" }
      },
      "required": ["patch_text"]
    }
  ]
---

# Files Skill

Read, search, and edit text files on the filesystem.

## Tools

### read
Read a UTF-8 text file with line numbers.

- Supports pagination via `offset` and `limit`
- Reading the full file records a freshness snapshot for later `write`, `edit`, and `multiedit`
- Returns an error for directories; use `list_dir` instead

### list_dir
List directory entries with optional bounded recursion.

- Supports pagination via `offset` and `limit`
- `depth` controls recursive traversal depth
- Appends `/` to directories and `@` to symlinks

### glob
Find files by glob pattern.

- Uses `rg --files`
- Search root defaults to the current working directory
- Returns absolute paths sorted by modification time

### grep
Search file contents with ripgrep.

- Requires `rg` to be installed and available in `PATH`
- `mode: "lines"` returns matching lines
- `mode: "files"` returns matching files only
- `mode: "count"` returns per-file match counts

### write
Create a new file or fully overwrite an existing text file.

- New files do not require a prior read
- Existing files must be fully read first in the current session
- Uses atomic write semantics

### edit
Replace an exact substring in an existing file.

- Requires the file to be fully read first in the current session
- Fails if `old_string` is not found
- Fails if `old_string` is ambiguous unless `replace_all: true`
- Fails if the file changed after it was read

### multiedit
Apply multiple exact replacements to one file atomically.

- Requires the file to be fully read first in the current session
- Applies edits sequentially to the evolving file content
- Fails the whole operation if any edit is invalid

### apply_patch
Apply a multi-file patch.

- Supports add, update, delete, and move operations
- Uses the `*** Begin Patch` / `*** End Patch` format
- Verifies patch context before writing changes
