# Agent Harness Issue Backlog

Current backlog for closing the most important Claude Code harness gaps in
Tabula. Source analysis: `docs/competitors/claude-code.md`.

These issues are written as independently grabbable tracer bullets. Each issue
should produce a demoable end-to-end behavior and keep the Tabula kernel generic.

## Priority Order

1. `001-session-ledger-harness-events.md`
2. `002-context-accounting-inspect-tool.md`
3. `003-tool-result-artifacts.md`
4. `004-background-task-ledger-for-subagents.md`
5. `005-isolated-worktree-subagent-tasks.md`
6. `006-structured-edit-diff-ledger.md`
7. `007-policy-decision-ledger.md`
8. `008-deferred-tool-discovery.md`
9. `009-ide-bridge-tracer.md`
10. `010-observability-correlation.md`

## Dependency Map

| Issue | Blocked by |
|---|---|
| 001 | None |
| 002 | 001 |
| 003 | 001 |
| 004 | 001 |
| 005 | 004 |
| 006 | 001 |
| 007 | 001 |
| 008 | 001 |
| 009 | 006, 007 |
| 010 | 001 |
