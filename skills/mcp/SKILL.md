---
name: mcp
description: "MCP bridge — connects to external MCP servers (filesystem, APIs, databases). Call: `EXEC python3 skills/mcp/run.py call <server> <tool> '<json_args>'`. List: `EXEC python3 skills/mcp/run.py list <server>`. Config: ~/.tabula/mcp/servers.json"
---
# MCP Bridge

Connects Tabula to [Model Context Protocol](https://modelcontextprotocol.io) servers.

## Configuration

Create `~/.tabula/mcp/servers.json`:

```json
{
  "servers": {
    "filesystem": {
      "transport": "stdio",
      "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "fetch": {
      "transport": "stdio",
      "command": ["uvx", "mcp-server-fetch"]
    },
    "remote-api": {
      "transport": "http",
      "url": "https://api.example.com/mcp"
    }
  }
}
```

## Usage

### Discover all tools

```bash
python3 skills/mcp/run.py discover
```

### List tools for a server

```bash
python3 skills/mcp/run.py list filesystem
```

### Call a tool

```bash
python3 skills/mcp/run.py call filesystem read_file '{"path": "/etc/hosts"}'
```
