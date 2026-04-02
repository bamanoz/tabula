# Tabula Roadmap

## Current state

Zig microkernel AI agent. Working MVP — agent bootstraps from genesis, discovers
skills via SKILL.md, spawns gateway, chats with user, creates new skills at runtime.

Kernel: ~360 lines, 5 commands (SPAWN, EXEC, KILL, SEND + auto-pipe).
Skills: skill (meta), llm-anthropic, gateway-cli, gateway-test, read-file.
Config: `llm` + `genesis` in tabula.yaml.
Tests: 20 kernel tests.

## Phase 1 — Stability

### Process health monitoring
Kernel detects when a spawned process crashes (exit, signal) and notifies LLM.
Currently if a skill dies, LLM keeps sending to a dead PID with no feedback.

- Kernel polls/waits for child exit status
- On crash: send notification to LLM (e.g. "CRASHED PID 2 exit=1")
- LLM can decide to restart or report error to user

### Write skill
Currently file writing is a hack: `EXEC sh -c 'cat > file << EOF'` via base64
encoding in LLM skill. Need a proper write-file skill that accepts path + content.

### Error propagation
EXEC captures stderr but only when stdout is empty. Should always include stderr
so LLM sees warnings alongside output.

## Phase 2 — Persistence

### Memory skill
Persistent memory between sessions. Agent forgets everything on restart.

- File-based or SQLite storage in `./data/` or `~/.tabula/`
- Skills: memory-store, memory-recall, memory-search
- Genesis instructs LLM to save important context and recall on startup
- Key-value or document-based, searchable

### Cron skill
Long-running skill that sends timed triggers to LLM via stdout (auto-pipe).

- LLM or user defines schedules: "every 5 min check URL", "daily at 9am send digest"
- Cron skill stores schedule in a file, survives restarts
- On trigger: sends message to LLM (e.g. "CRON: check-health"), LLM decides what to do
- LLM can add/remove cron entries via SEND to the cron skill

### Session state
Save/restore conversation history so agent can resume after restart.
Separate from memory — this is raw LLM message history.

## Phase 3 — Real interfaces

### Telegram gateway
Replace CLI with a real messaging interface.

- `skills/gateway-telegram/` — long-running, connects to Telegram Bot API
- Receives messages via polling or webhook, sends to LLM via stdout
- LLM responds via SEND
- Support text, images (as descriptions), inline keyboards
- Token via env var `TELEGRAM_BOT_TOKEN`

### Web gateway
Simple HTTP/WebSocket interface for browser-based chat.

## Phase 4 — Ecosystem

### Skill registry
`tabula install <name>` — download and install skills from a registry.

- Git-based: each skill is a repo or a directory in a monorepo
- Manifest in SKILL.md is sufficient (no separate package.json)
- `tabula list` — show installed skills
- `tabula search` — search registry
- Helm-like approach: skills are charts, genesis is values

### Multi-LLM
Multiple LLM contexts for different tasks. Router skill dispatches messages
to the right LLM based on content/skill type.

### Sandbox
Restrict skill permissions. A skill that reads files shouldn't be able to
make network requests. Docker/namespaces for isolation.

## Non-goals (for now)

- GUI / desktop app
- Cloud hosting / SaaS
- Agent-to-agent communication (can be added as a skill later)
- MCP compatibility (our skill protocol is simpler; MCP bridge can be a skill)
