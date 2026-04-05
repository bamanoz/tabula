# Progress

## Overall Status
Phase: DONE
Progress: 100%

## Completed Steps
- [x] Codebase analysis — kernel.zig (627 lines, 5 primitives)
- [x] Plan simplified to Level 2 (3 targeted kernel additions)
- [x] Bootstrap architecture designed and documented
- [x] waitpid health detection (reapZombies in run loop)
- [x] LIST primitive
- [x] QUERY primitive with 5s timeout (poll-based, non-piped only)
- [x] piped field added to Process struct
- [x] kernel.tools.json updated (6 tools)
- [x] Bootstrap script (bootstrap.py)
- [x] main.zig rewritten: reads tabula.yaml (llm + bootstrap), runs bootstrap, parses JSON
- [x] LLM moved from skills/ to llms/
- [x] yaml.zig removed
- [x] tabula.yaml simplified to two fields
- [x] 28 tests passing (including LIST and QUERY tests)
- [x] E2E test: full cycle with gateway-test, SEND, EXEC read-file
- [x] docs/bootstrap-architecture.md updated
- [x] No zombie processes after shutdown

## File Changes
| File | Change |
|------|--------|
| src/kernel.zig | +reapZombies, +list(), +query(), +piped field, +8 tests |
| src/kernel.tools.json | +LIST, +QUERY definitions (now 6 tools) |
| src/main.zig | Rewritten: yaml→bootstrap, Config struct with llm+bootstrap |
| src/yaml.zig | Deleted |
| bootstrap.py | New: skill discovery, prompt assembly, startup instructions |
| tabula.yaml | Simplified to llm + bootstrap |
| llms/llm-anthropic/ | Moved from skills/llm-anthropic/ |
| docs/bootstrap-architecture.md | Updated with current architecture |
