# Remove confirmed dead protocol v3 surface

**Type:** AFK  
**Status:** completed

## What to build

Remove protocol v3 declarations and envelope fields that survived the v3 migration but have no active first-party producer or consumer. This is a narrow cleanup independent of the larger v4 refactoring.

Start from `protocol-audit.md` and repeat the producer/consumer search against current source before editing.

## Scope

- Remove the unused `MsgStatus` transport type.
- Remove legacy top-level handshake fields from `Message`: `Sends`, `Receives`, `ReceivesGlobal`, `Token`, `AuthToken`, and `Hooks`.
- Keep `State`, `Context`, and `Tools`: implementation verification confirmed active `session.init` and tool-result users; migrate them only in protocol v4.
- Simplify `cloneMessage` accordingly.
- Remove stale current-protocol documentation and clearly mark historical v2/v3 migration examples.

Do not remove active tool/hook fields or active topics listed in the audit.

## Acceptance criteria

- [x] Cross-repository search classifies every removed field or type as source, test, docs, or historical reference.
- [x] Current hello continues to use only `hello.data` fields.
- [x] Active hooks, tools, exchanges, sessions, gateways, drivers, and testbed clients remain compatible with protocol v3.
- [x] Current protocol docs do not present `status` as an active transport type; historical migration examples remain explicitly labeled old.
- [x] Focused kernel tests pass with `-race -count=1` (222 tests).
- [x] No bundle SDK/gateway code consumes the removed top-level fields or `status` transport type.
- [x] A final reference search finds no dangling active references.

## Blocked by

None. This cleanup may land before protocol v4.
