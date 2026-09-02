# Remove command deduplication from aggregate projection

**Type:** AFK  
**Status:** proposed

## What to build

Remove `State.AppliedCommands` and make repository command storage the sole command-idempotency authority.

`Decide` must describe domain transitions only. Duplicate command detection, digest conflicts, and original acceptance boundaries remain repository responsibilities under ADR 0031.

## Evidence

- `internal/agent/types.go`: every projection serializes `AppliedCommands`.
- `internal/agent/decide.go` and `apply.go`: domain logic checks and mutates the map.
- `internal/agent/sqlite_repository.go`: the `commands` table already stores command digest and boundary.
- ADR 0031 defines compact repository-owned command deduplication.

## Acceptance criteria

- [ ] `State` no longer contains command IDs or command digests.
- [ ] `Decide` and event application do not implement repository idempotency.
- [ ] Same-ID/same-digest retries return the current projection and original command boundary.
- [ ] Same-ID/different-digest retries fail with `ErrCommandConflict`.
- [ ] Memory and SQLite conformance remains identical.
- [ ] A long-session test proves projection size is independent of command count except for domain state.
- [ ] Existing persisted projection handling follows the repository's no-legacy policy; no compatibility alias is added.

## Blocked by

None - can start immediately.
