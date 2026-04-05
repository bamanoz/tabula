# Task: Tabula Microkernel — Kernel Hardening + Bootstrap

## Status: COMPLETE

## Description
Added three kernel primitives (waitpid, LIST, QUERY) and replaced static config with dynamic bootstrap.

## What Was Built

### Kernel (src/kernel.zig)
- **reapZombies()** — non-blocking waitpid(WNOHANG) each loop iteration, notifies LLM with PROCESS_DIED
- **LIST** — enumerates all processes: `PID <n> <cmd> alive=<bool>`
- **QUERY <pid> <msg>** — send message, read response with 5s poll timeout. Only for non-piped processes (StdoutPiped error otherwise)
- **piped: bool** field in Process struct, set by pipe()
- 28 tests passing

### Bootstrap (bootstrap.py)
- Scans skills/ directory, reads SKILL.md from each subdirectory
- Assembles system prompt with skill descriptions + startup instructions
- Outputs `{"prompt": "..."}` JSON to stdout
- Can be written in any language — contract is just JSON on stdout

### Config (tabula.yaml)
```yaml
llm: python3 llms/llm-anthropic/run.py
bootstrap: python3 bootstrap.py skills/
```

### Directory Structure
```
tabula/
├── src/kernel.zig        # Microkernel (6 commands)
├── src/kernel.tools.json # Tool definitions (embedded at compile time)
├── src/main.zig          # Boot: config → bootstrap → LLM → main loop
├── bootstrap.py          # Skill discovery + prompt assembly
├── tabula.yaml           # Config: llm + bootstrap commands
├── llms/
│   └── llm-anthropic/    # LLM driver (pluggable, not a skill)
├── skills/
│   ├── gateway-cli/      # Interactive CLI gateway
│   ├── gateway-test/     # Automated test gateway
│   ├── read-file/        # File reading skill
│   └── skill/            # Skill format documentation
└── docs/
    └── bootstrap-architecture.md
```

## Design Principles
- If it can be a skill, it must be a skill
- LLM is a pluggable kernel component, not a skill
- Bootstrap is the config — it owns skill discovery and startup behavior
- Kernel stays micro — only process management and message routing
