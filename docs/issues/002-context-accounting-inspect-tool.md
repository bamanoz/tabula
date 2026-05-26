# Context Accounting Inspect Tool

Type: AFK

Priority: P0

Status: Completed

Repos: `tabula-bundles`, optionally `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Add a generic context diagnostics capability that can explain what is occupying
the next provider request for a session. It should report estimated context use
by source, top tool-result contributors, duplicate file reads where available,
last compaction boundary, and whether the session is near a compaction threshold.

This should be available to the agent as a tool and usable by humans through an
installed gateway or testbed command.

## Acceptance criteria

- [x] A context inspection tool returns a bounded JSON summary for the current
      session.
- [x] The summary separates at least user messages, assistant messages, tool
      calls, tool results, compaction summary, and prompt/context inserts.
- [x] The tool reports the most expensive tool results by estimated token count.
- [x] A test creates a session with mixed messages/tool results and verifies the
      reported breakdown.
- [x] User-facing docs explain that this is approximate accounting, not provider
      billing truth.

## Implementation

Implemented in `tabula-bundles`:

- Added `tabula_session_sdk.context.inspect_session_context()` with approximate
  token accounting from persisted history and ledger events.
- Added `session_context` tool to the sessions plugin.
- Added `python3 plugins/sessions/run.py context <session>` CLI command.
- Reports source buckets for user messages, assistant messages, tool calls, tool
  results, compaction summaries, prompt/context inserts, and other entries.
- Reports top tool results, duplicate file reads, last compaction boundary,
  latest provider usage block, and compaction-threshold estimate when enough
  metadata is available.
- Documented that counts are approximate local debugging estimates, not provider
  billing truth.

Verification:

```bash
python3 -m unittest base.sessions.test_session_sdk
python3 -m unittest drivers.test_driver_agents
python3 -m unittest gateways.test_gateway_web
TABULA_HOME=<tmp> python3 base/sessions/run.py context main
```

## Blocked by

- `001-session-ledger-harness-events.md`
