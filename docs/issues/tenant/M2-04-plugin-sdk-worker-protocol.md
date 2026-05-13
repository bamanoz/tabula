# M2-04 — Plugin SDK switches to worker protocol

Status: done
Phase: M2
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4, §10)

## What to build

Update the Python plugin SDK to speak the **worker protocol**
(M1-02) instead of the legacy kernel-side stdio NDJSON. Migrate
every existing plugin in `tabula-bundles` to the new SDK.

Per ADR §10, no backwards compatibility: legacy SDK helpers are
deleted, not deprecated.

SDK changes (`tabula-bundles/_lib/python/src/tabula_plugin_sdk/`):

- New entrypoint: read `WorkerInit` from stdin (one NDJSON
  line), send `WorkerInitAck`.
- Loop: read `WorkerCall`, dispatch to plugin's tools registry
  by tool name, send `WorkerResult` with matching `call_id`.
- Handle `WorkerShutdown`: clean up, exit 0.
- On uncaught exception in tool handler: send `WorkerResult`
  with `ok: false, error: {...}`, do NOT crash.
- On protocol error (malformed input, unknown op): send
  `WorkerError`, exit non-zero.
- Bump SDK `__version__` to next major.
- Delete old kernel-stdio-protocol code paths.

Plugin migrations (each plugin updated to import / use new
SDK):

- `subagents/subagents/`
- `code/hook-workspace-boundary/`
- `code/hook-approvals/`
- `caveman/hook-caveman/`
- `base/observer/`
- `base/mcp/`
- `base/hook-permissions/`
- `base/sessions/`
- `base/hook-logger/`
- `base/cron/`
- Test fixtures: `test-fixtures/testbed-hook-blocker/`,
  `test-fixtures/testbed-hook-recorder/`,
  `test-fixtures/testbed-hook-mutator/`,
  `test-fixtures/testbed-dynamic-tools/`

Distro plugin in tabula-distrib (parallel scope, NOT part of
this issue but tracked here for visibility):

- `tabula-distrib/claw/plugins/gateway-telegram-plugin/` —
  migrated in a separate distro-side change blocked by this
  one.

SDK tests:

- Unit tests using in-process pipes that simulate runtime
  ↔ SDK worker exchange.
- Each handler scenario (success, exception, malformed call)
  asserted.

## Acceptance criteria

- [ ] SDK rewritten to worker protocol; old kernel-stdio code
      paths physically deleted.
- [ ] SDK unit tests cover Init/Call/Shutdown/Error paths.
- [ ] Every listed plugin migrated; each one imports the new
      SDK, no `from tabula_plugin_sdk import <legacy>` survives.
- [ ] Plugin smoke tests (`scripts/run.py --self-check` or
      similar convention if exists) pass.
- [ ] No reference to legacy SDK names anywhere in
      `tabula-bundles` (grep clean).
- [ ] SDK version bumped; CHANGELOG (if any) updated.
- [ ] All plugin `plugin.toml` files validated against current
      schema (do not change schema in this slice).

## Blocked by

- M1-02 (worker protocol types are the contract this SDK
  implements; types may need to be ported to Python — duplicate
  the schema in Python, do not depend on Go package directly)

## Notes

- The Go-side `internal/runtime/worker/wire/` package is the
  source of truth for the protocol shape. Python SDK
  reimplements the same JSON shape independently. Keep them in
  sync via tests on the Go side that validate sample messages
  produced by the Python SDK round-trip cleanly.
- Until M2-06 cuts over, these migrated plugins cannot actually
  run in production (the kernel still uses stdio path). They
  can be unit-tested with the new SDK in isolation. Dual-state
  is intentional: M2-04 lands first, then M2-06 atomically
  cuts over.
- Actually no — M2-06 is blocked by M2-04, so plugins are
  migrated before cutover, kernel cuts over, plugins
  immediately work via runtime. There is no on-trunk window
  where plugins are broken.

## Out of scope

- Skill harnesses (bash, python, node) — those are M3.
- Removing `process_manager.go` skill exec — also M3.
- `gateway-telegram-plugin` migration in `tabula-distrib` —
  separate change blocked by this one.
