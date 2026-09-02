# Split protocol-v4 client command and query adapters

**Type:** AFK  
**Status:** proposed

## What to build

Replace the `client_v4.go` megaswitch with explicit protocol adapters grouped by command/query domain. Each adapter owns strict payload decoding, route requirements, authorization, service invocation, and response encoding for its operations.

Keep transport authentication and envelope framing centralized. Do not move aggregate semantics or commit loops into adapters.

## Evidence

`internal/kernel/client_v4.go` combines connection authentication, route validation, operation whitelists, session commands, queries, subscriptions, records, error translation, UI state projection, and event mapping in one file of roughly one thousand lines.

## Acceptance criteria

- [ ] Connection, session lifecycle, input/turn, subscription, records, extensions, and tool operations have narrow adapters.
- [ ] Route and actor authorization rules have one owner per operation.
- [ ] Command adapters call authoritative services rather than implementing `Decide/Apply/Commit` directly.
- [ ] Query adapters do not mutate Hub/session state.
- [ ] Error-code translation remains one transport-level module.
- [ ] Unknown operation/kind combinations fail strictly.
- [ ] Protocol conformance tests cover every operation through the public envelope handler.

## Blocked by

Issue 04.
