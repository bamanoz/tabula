# M2-04b — `gateway-telegram` plugin SDK migration

Status: done
Phase: M2
Type: AFK
Repo: tabula-distrib
Labels: needs-triage, area/sdk, area/distro, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
Companion to: `M2-04` (which migrates plugins in `tabula-bundles`)
Amendment: AMENDMENTS.md C3

## What to build

Migrate `tabula-distrib/claw/plugins/gateway-telegram-plugin/`
to the new worker-protocol SDK from M2-04. Without this slice,
M2-07's atomic cutover breaks the gateway in production.

This is the distro-side analogue of M2-04. Same protocol,
same SDK contract, different repo.

Components:

- Update `gateway-telegram-plugin/` to import the new
  `tabula_plugin_sdk` (M2-04 version).
- Delete every reference to legacy SDK helpers / kernel-stdio
  protocol code.
- Adjust plugin.toml if any field shape changed in M2-04.
- Update plugin tests / smoke tests.
- Verify against the new runtime daemon by spawning a local
  kernel + runtime and exercising at least one telegram tool
  call (use a mock telegram backend if real credentials
  aren't in CI).

## Acceptance criteria

- [ ] gateway-telegram-plugin imports the new SDK exclusively;
      no legacy symbol survives (grep clean).
- [ ] Plugin smoke test passes against M2 runtime (mocked
      telegram API).
- [ ] tabula-distrib/claw distro tests pass.
- [ ] Bundle's plugin.toml validates against current schema.

## Blocked by

- M2-04 (SDK exists)

## Blocks

- M2-07 (cutover deletes the kernel-stdio path; without this
  slice, gateway-telegram is silently broken after M2-07
  ships)

## Notes

- Coordinate landing: M2-04 first, then M2-04b, then M2-07.
- If `tabula-distrib` carries a CHANGELOG, document the
  SDK bump.
