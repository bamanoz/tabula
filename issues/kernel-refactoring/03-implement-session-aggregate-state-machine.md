# Implement the pure session aggregate state machine

**Type:** AFK  
**Status:** completed

## What to build

Implement a transport-free, storage-free domain package for the protocol v4 session aggregate. It must model session, immutable inputs, FIFO turns, attempts, driver desired/observed state, cancellation intent, and recovery state through deterministic commands and events.

The package must not import WebSocket, runtime transport, SQLite, gateway, provider, plugin, or distro code.

## Acceptance criteria

- [x] Domain types and state names match the approved ADR and protocol specification.
- [x] Commands are validated against actor authority, driver generation/fence, attempt state, and terminal-state rules; repository version checks remain issue 04 ownership.
- [x] Duplicate commands are deterministic and do not create duplicate transitions.
- [x] One input creates at most one turn and FIFO ordering is explicit.
- [x] Safe retry before execution permit and uncertain recovery after permit are distinct.
- [x] Cancellation/completion races resolve to one committed terminal transition.
- [x] Scenario tests cover lifecycle transitions and invalid/stale transitions.
- [x] Generated command-sequence tests validate invariants after every committed event.
- [x] Focused tests pass with `-race -count=1` (13 tests); full core race suite passes (609 tests).

## Blocked by

Issue 01.
