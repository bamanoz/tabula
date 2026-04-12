---
name: files
description: "Read, write, and edit files. Prefer str_replace for targeted edits, write_file for new files or full rewrites, read_file to inspect contents."
tools:
  [
    {
      "name": "read_file",
      "description": "Read file contents with line numbers. Use offset/limit for large files.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "offset": { "type": "integer", "description": "Start line (1-based). Default: 1" },
        "limit": { "type": "integer", "description": "Max lines to return. Default: 2000" }
      },
      "required": ["path"]
    },
    {
      "name": "write_file",
      "description": "Create or overwrite a file with the given content. Creates parent directories if needed.",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "content": { "type": "string", "description": "Complete file content" }
      },
      "required": ["path", "content"]
    },
    {
      "name": "str_replace",
      "description": "Replace an exact string in a file. Fails if old_string is not found or is ambiguous (found more than once without replace_all).",
      "params": {
        "path": { "type": "string", "description": "Absolute or relative file path" },
        "old_string": { "type": "string", "description": "Exact text to find" },
        "new_string": { "type": "string", "description": "Replacement text" },
        "replace_all": { "type": "boolean", "description": "Replace all occurrences. Default: false" }
      },
      "required": ["path", "old_string", "new_string"]
    }
  ]
---

# Files Skill

Read, write, and edit files on the filesystem.

## Tools

### read_file
Read file contents with line numbers. Supports pagination via `offset` and `limit` for large files.

### write_file
Create a new file or overwrite an existing one. Creates parent directories automatically.

### str_replace
Replace an exact substring in a file. Use for targeted edits without rewriting the whole file.
- Fails if `old_string` is not found
- Fails if `old_string` appears more than once (unless `replace_all: true`)
