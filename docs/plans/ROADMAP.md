# Tabula Roadmap

Feature ideas based on competitive analysis of Claude Code, OpenClaw, OpenCode, and Codex.

## High priority

### Permission system
Rules allow/deny/ask for tools in config (AGENTS.md or tabula.yaml).
Example: `allow: read *`, `deny: EXEC rm *`, `ask: EXEC git push *`.
Infrastructure already exists — before_tool_call hook. Need config format and enforcement.

### Compaction hooks
Add `before_compaction` hook event so memory skill can flush context before summarization.
Add configurable max token threshold for auto-compaction trigger.

### Plan mode
`/plan` slash command. Temporarily deny write/edit/multiedit/apply_patch/EXEC(write), allow only read tools.
Agent produces a markdown plan. After user approve — switch back to normal mode.

### Session resume/fork
Continue a previous conversation or branch from a point in history.
Sessions skill already handles cross-session messaging — extend with resume/fork.

### Skill install from URL
`tabula install https://github.com/user/skill` — clone into skills/, validate SKILL.md.
Support git repos and local paths.

### Headless / non-interactive mode
`tabula run "do something"` — execute a prompt without interactive CLI.
Useful for scripts, CI/CD, automation. Output to stdout.

## Medium priority

### LSP integration
Tool-skill `lsp` — starts language servers, exposes tools: goto_definition, find_references,
document_symbols, diagnostics. Useful for coding tasks.

### Python REPL
Persistent IPython kernel with state between calls. Tools accessible via `tabula.tool()`.
Useful for data exploration, quick scripting, iterative development.

### Discord gateway
Another channel alongside Telegram, CLI, API. Discord bot with slash commands.

### Agent routing
Config-based routing: which agent handles which channel/chat/user.
Enables multi-personality setups on one Tabula instance.

### Auto-compaction triggers
Trigger compaction automatically when approaching token limit.
Currently manual — should be automatic with configurable threshold.

### File change undo/revert
Track file modifications made during a session. `/rewind` to roll back changes.
Git-based snapshots or simple before/after copies.

### Custom slash commands
User-defined slash commands as markdown templates in workspace skills.
`/review`, `/commit`, `/simplify` — each is a SKILL.md with `user-invocable: true`.

## Low priority

### Browser automation
CDP via Playwright — browser_open, browser_click, browser_screenshot, browser_read.
Useful for web scraping and testing.

### Voice in Telegram
STT for incoming voice messages, TTS for responses.
Telegram already supports voice — just need transcription/synthesis.

### Skill marketplace
Registry and search for community skills. `tabula search weather`.

### AI guardian
Separate LLM reviewer evaluates risk of each tool_use before execution.
Codex uses gpt-5.4 as guardian with risk levels (low/medium/high/critical).

### Desktop app
Electron or Tauri wrapper around gateway-api + web UI.

### Webhooks
Inbound HTTP POST triggers agent with custom payload. Useful for CI/CD notifications,
GitHub events, external service integration.

### Multi-provider model switching
`/model` command to switch LLM mid-session. Support model failover chains.
