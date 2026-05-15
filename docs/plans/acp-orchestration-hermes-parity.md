# ACP Orchestration Hermes Parity Plan

Date: 2026-05-16
Status: Draft
Owner: current ACP orchestration work

## Goal

Bring Tabula ACP-backed subagent orchestration up to Hermes-level maturity
without copying Hermes' full ACP runtime architecture.

Target shape:

- ACP remains a backend of the existing `subagents` plugin.
- The public control surface stays `subagent_spawn/send/steer/wait/list/kill`.
- Tabula reaches Hermes-level robustness for multi-turn ACP orchestration,
  cancellation, permissions, event visibility, and failure handling.

## Non-goals

- Do not build a parallel ACP-native subagent system outside `subagents`.
- Do not turn Tabula subagent orchestration into a full editor-facing ACP
  server/runtime like `hermes-agent/acp_adapter/*`.
- Do not add OpenAI-compatible ACP shims like
  `hermes-agent/agent/copilot_acp_client.py`.

## Current State

Today Tabula has:

- a minimal ACP subprocess client in
  `tabula-bundles/_lib/python/src/tabula_plugin_sdk/acp_client.py`
- a persistent ACP-backed worker in
  `tabula-bundles/subagents/subagent-acp/run.py`
- `subagents` integration in
  `tabula-bundles/subagents/subagents/run.py`
- passing installed testbed coverage for `spawn -> wait -> send -> wait`

Current gaps vs Hermes-level maturity:

- no graceful ACP `session/cancel` path before process kill
- no structured event bridge for thinking/tool progress/tool completion
- narrow permission mapping and fallback semantics
- weak capability validation and protocol diagnostics
- limited failure-path integration coverage
- no explicit restart/recovery story for running ACP workers

## Must-Have Before Claiming Hermes-Level Orchestration

These items are required before we should describe Tabula ACP orchestration as
Hermes-level.

### 1. Graceful cancel before hard kill

Implement a real ACP cancel path for running turns.

Required behavior:

- `subagent_kill` should first request ACP `session/cancel` when the worker is
  alive and currently executing a turn.
- The worker should distinguish:
  - cancelled turn
  - normal completion
  - process kill fallback
- If graceful cancel fails or times out, keep the current SIGTERM/SIGKILL path.

Why:

- Hermes has real cancellation semantics; process kill alone is not parity.

### 2. Explicit single-flight turn execution per ACP session

Make the one-turn-at-a-time invariant explicit in code and tests.

Required behavior:

- only one active `session/prompt` per ACP worker session
- concurrent `subagent_send` attempts fail fast with a clear error
- registry state records whether the worker is idle or busy
- `subagent_wait` and `subagent_send` do not infer turn state only from result
  file timing

Why:

- Hermes session handling is explicit about runtime ownership and concurrency.

### 3. Structured ACP event bridge for orchestration visibility

Add an internal event bridge from ACP `session/update` into Tabula-owned worker
state and parent-visible diagnostics.

Required behavior:

- capture and persist update kinds, not only final text
- at minimum track:
  - `agent_message_chunk`
  - `agent_thought_chunk` when present
  - tool-start/tool-complete style updates when present from the ACP agent
- expose enough metadata that debugging does not require reading raw stderr
- keep parent delivery concise; do not spam the user session with every chunk by
  default

Why:

- Hermes has a proper event bridge; parity requires more than final-text only.

### 4. Permission bridge semantics closer to Hermes

Harden ACP permission handling.

Required behavior:

- preserve current policies `allow_once`, `allow_always`, `reject`
- add a session-scoped policy if the ACP agent advertises it, or document the
  exact fallback when it does not exist
- deny on timeout/bridge failure explicitly and log why
- record permission decisions in worker history/logs

Why:

- Hermes treats approvals as a first-class bridge with clear fallback rules.

### 5. Capability validation during ACP session setup

Validate that the target ACP agent supports the subset Tabula orchestration
depends on.

Required behavior:

- validate `initialize` result before first prompt
- fail early with a specific error if required session methods/capabilities are
  missing
- record the negotiated capabilities in registry or logs
- distinguish protocol mismatch from normal remote failure

Why:

- Hermes does more explicit ACP runtime wiring; parity needs clearer setup
  guarantees.

### 6. Stronger error surfaces and worker diagnostics

Improve failure reporting from `ACPSubprocessClient` and the ACP worker.

Required behavior:

- include ACP stderr tail in relevant failure paths
- classify timeout vs protocol error vs remote error vs local orchestration error
- persist the last terminal error in registry state
- make `subagent_wait` return useful failure context without requiring log file
  spelunking first

Why:

- Hermes has a much better debug surface today.

### 7. Hermes-grade installed integration coverage

Extend installed tests beyond the happy path.

Required coverage:

- ACP agent exits before first result
- ACP agent exits between turns
- malformed JSON-RPC notification
- permission request timeout/failure
- cancel during long-running prompt
- send while busy
- repeated follow-up turns beyond two turns

Why:

- installed runtime bugs are the most likely regressions in this design.

## Should-Have Soon After

These items are useful and close to Hermes quality, but not required for the
first parity claim.

### 1. Restart/recovery contract

- decide whether running ACP-backed subagents are recoverable across plugin or
  kernel restart
- if not recoverable, detect stale registry state quickly and mark it terminal
- if recoverable, define the exact persisted handshake and ownership model

### 2. Better parent-session observability

- add optional verbose diagnostics mode for ACP-backed subagents
- surface a concise progress snapshot in `subagent_list`
- expose recent update kinds or last activity timestamp

### 3. Richer turn history model

- store prompts, permission decisions, stop reasons, and result boundaries as
  structured records
- make history easier to correlate with registry state and logs

### 4. Capability-aware session features

- support optional ACP features like mode/model switching only when advertised
- degrade cleanly when a target ACP agent lacks those features

## Ignore For Now

These are real Hermes features, but they are not required for Tabula's chosen
architecture.

### 1. Full ACP session manager in Tabula core

Do not copy `hermes-agent/acp_adapter/session.py` into Tabula orchestration.
Tabula should keep ACP state inside the `subagents` backend, not create a new
global ACP runtime subsystem.

### 2. Editor-facing ACP tool rendering

Do not build Hermes-style UI rendering of every tool event unless a real Tabula
ACP editor surface needs it.

### 3. OpenAI-compatible ACP adapter layer

Do not replicate `hermes-agent/agent/copilot_acp_client.py` style shims just to
fit ACP behind another chat API.

### 4. ACP file-system method emulation beyond real need

Do not add generic `fs/read_text_file` or `fs/write_text_file` bridges unless a
target ACP agent required by Tabula orchestration actually depends on them.

### 5. Full parity with Hermes ACP server APIs

`load/resume/fork/list` parity matters on Tabula's ACP server side. It is not a
requirement for ACP-backed subagent orchestration by itself.

## Suggested Execution Order

1. Add explicit worker turn state and graceful cancel plumbing.
2. Harden `ACPSubprocessClient` error reporting and capability validation.
3. Add the internal ACP event bridge and structured history/state updates.
4. Expand permission semantics and logging.
5. Add failure-path unit tests and installed testbed coverage.
6. Decide and document restart/recovery semantics.

## Verification

Minimum verification before closing this plan:

```bash
python3 -m unittest subagents.tests.test_subagents_plugin
python3 -m unittest subagents.subagents.test_subagents
python3 -m tabula_testbed_runner.cli run --tabula-root /Users/mak/src/tabula --testbed-dir /Users/mak/src/tabula-distrib/testbed --suite subagents --source tabula-bundles=/Users/mak/src/tabula-bundles
```

Plus new tests covering cancel, busy-send rejection, remote crash, malformed ACP
frames, and permission failure behavior.

## References

- `tabula-bundles/_lib/python/src/tabula_plugin_sdk/acp_client.py`
- `tabula-bundles/subagents/subagent-acp/run.py`
- `tabula-bundles/subagents/subagents/run.py`
- `hermes-agent/agent/copilot_acp_client.py`
- `hermes-agent/acp_adapter/session.py`
- `hermes-agent/acp_adapter/events.py`
- `hermes-agent/acp_adapter/permissions.py`
