# Remove legacy Hub session persistence and lifecycle authority

**Type:** AFK  
**Status:** proposed

## What to build

Remove `DiskSessionStore` and retire the legacy `kernel.Session` lifecycle as a persisted session model. SQLite `SessionRepository` remains the only durable session authority.

Replace remaining legitimate live concerns with narrow owners: connection membership, transient exchange routing, preferred runtime attachment, prompt-session cache where still required, and tool-result spool configuration. Do not preserve a second object called session with independent lifecycle states.

## Evidence

- Production configures both `SetSessionStore(NewDiskSessionStore(...))` and `SetAgentSessionRepository(...)`.
- Legacy states are `active`, `idle`, `closing`, and `suspended_stuck`; v4 states are `open`, `suspended`, and `closed`.
- ADR 0028 declares kernel SQLite the sole session persistence authority.
- `TestLegacySessionJSONCannotCreateV4Session` proves the two stores are intentionally separate, but the duplicate model remains active.

## Acceptance criteria

- [ ] Production no longer writes or hydrates tenant `state/sessions/*.json` files.
- [ ] Durable session lifecycle reads and writes use only `SessionRepository`.
- [ ] Client membership and other live routing state have names that do not imply durable session authority.
- [ ] Tool-result spool location is configured independently of session persistence.
- [ ] Join, disconnect, archive, delete, restart, and preferred-runtime behavior remain explicitly covered.
- [ ] No legacy lifecycle aliases or migration shims remain.
- [ ] Architecture docs and `tabula-guide` describe one authoritative session model.

## Blocked by

None - can start immediately.
