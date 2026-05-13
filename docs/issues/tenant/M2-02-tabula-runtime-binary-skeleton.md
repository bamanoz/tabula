# M2-02 — `tabula-runtime` binary skeleton

Status: done
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§1)

## What to build

Create the `tabula-runtime` binary as a runnable daemon that
dials the kernel, completes the Hello handshake, and answers
admin ops (`Health`, `ListCapabilities`, `Reload`). Worker
spawn is deferred to M2-03; `Invoke` returns `internal_error`
"not implemented" for now.

Components:

- `cmd/tabula-runtime/main.go`: argparsing (cobra/urfave/stdlib —
  pick stdlib `flag` if no existing convention; check
  `cmd/tabula/`), signal handling, graceful shutdown.
- Subcommand `tabula-runtime start` (default).
- Placeholder subcommand `tabula-runtime stdio` that prints
  "not implemented in M2" and exits non-zero (real
  implementation in M6 per Q7a).
- `internal/runtime/host/config/`: `runtime.toml` loader. Schema:
  ```toml
  [[kernel]]
  id    = "local"
  url   = "unix:///$TABULA_HOME/run/runtime.sock"
  token = "${TABULA_RUNTIME_TOKEN_FILE}"   # path, contents read at dial
  ```
- `internal/runtime/host/dialer/`: dial kernel via M2-01's transport
  layer; on connect, send `Hello` with token; await `HelloAck`.
- Frame loop: receive admin ops from kernel, respond.
- `Invoke` handler returns `internal_error` with message
  "worker spawn not implemented".
- Logging to stderr in structured (JSON or k=v) format —
  pick whatever the kernel uses today for consistency.
- Reconnect logic: exponential backoff 1s → 60s on disconnect
  (Q6a).

## Acceptance criteria

- [ ] `go build ./cmd/tabula-runtime` produces a binary.
- [ ] Manual smoke: a fake kernel using M2-01's server harness
      accepts `Hello`, runtime appears in connected state.
- [ ] `Health` and `ListCapabilities` (returning empty) round-
      trip.
- [ ] `Reload` is accepted as no-op (M2-03 makes it functional).
- [ ] `Invoke` returns structured `internal_error`.
- [ ] SIGTERM gracefully closes the connection (sends close
      frame), exits 0 within 5s.
- [ ] Disconnect → reconnect backoff is observable (test with
      fake server that drops connection; runtime keeps trying
      with growing delays).
- [ ] `runtime.toml` validation errors are clear (missing
      kernel url, malformed entry).
- [ ] Race-detector clean.

## Blocked by

- M2-01 (transport)
- M1-04 (PluginExecPolicy stub for placeholder Invoke handler)

## Notes

- Keep main thin; logic in subpackages so it stays testable.
- `runtime.toml` lives at `$TABULA_HOME/config/runtime.toml`
  (parallels `config/global.toml`).
- N:M support: `[[kernel]]` is a list (Q11). M2-02 supports the
  case `len(kernels) == 1`; multi-kernel concurrency is not in
  scope for this slice but the data shape must already be a
  list, not a singleton, so future work doesn't break the
  config schema.
