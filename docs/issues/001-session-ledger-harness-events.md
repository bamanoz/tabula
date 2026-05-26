# Session Ledger Harness Events

Type: AFK

Priority: P0

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/competitors/claude-code.md`

## What to build

Add a shared session ledger event path for harness metadata that is not a chat
message: compaction boundaries, tool artifacts, file edit events, task state,
permission decisions, and usage snapshots. The first slice should define the
ledger envelope, append/read APIs, and one minimal producer and consumer so the
path is exercised end-to-end.

Keep the kernel generic. The kernel may route or store opaque metadata only if
needed, but product policy and event schemas should live in shared runtime,
driver, or session SDK code.

## Acceptance criteria

- [x] A shared ledger event envelope exists with stable fields for session id,
      tenant id, event kind, timestamp, producer, and JSON payload.
- [x] Driver or session SDK code can append and read ledger events for a session.
- [x] At least one real harness event is written and restored in tests.
- [x] Gateway or CLI replay ignores unknown ledger event kinds without failing.
- [x] Documentation explains which components own ledger schemas and why the
      kernel remains product-agnostic.

## Implementation

Implemented in `tabula-bundles`:

- Added `tabula_session_sdk.ledger` with `append_ledger_event()` and
  `read_ledger_events()`.
- Added `ledger.jsonl` next to `history.jsonl` under
  `$TABULA_HOME/data/sessions/<session>/`.
- Added `compaction.boundary` as the first real producer from shared drivers.
- Updated transcript reconstruction and gateway replay to ignore ledger events
  as harness metadata, not chat messages.
- Documented the envelope and ownership rules in `base/sessions/README.md`.

Verification:

```bash
python3 -m unittest base.sessions.test_session_sdk
python3 -m unittest drivers.test_driver_agents
python3 -m unittest gateways.test_gateway_web
```

## Blocked by

None - can start immediately.
