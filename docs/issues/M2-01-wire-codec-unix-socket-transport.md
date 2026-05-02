# M2-01 — Wire codec + unix socket transport

Status: open
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§2)

## What to build

Promote the in-memory codec from `M1-06` into a real,
production-ready package and add a unix socket transport. The
result is a usable `RuntimeConn` over a real `net.Conn`,
independently testable, not yet wired into the kernel.

Components:

- `internal/runtime/codec/`: WebSocket-framed JSON encoder /
  decoder over any `net.Conn`. Pick one WS library (`coder/
  websocket` recommended for active maintenance and minimal API).
- `internal/runtime/transport/unixsock/`: `Listener` and
  `Dialer` for `unix:///path/to/sock`. Permissions: parent dir
  `0700`, socket `0600` (matches Q2a).
- `internal/runtime/conn/`: concrete `wsConn` type implementing
  `RuntimeConn` (M1-03) on top of codec. Owns one connection,
  multiplexes Invoke/Cancel/Health calls by `call_id`,
  background reader goroutine routes responses to waiting
  callers.
- Server-side accept loop with per-connection goroutine and a
  pluggable `Handler` interface that backends will implement.

## Acceptance criteria

- [ ] Codec round-trip tests over real `net.Pipe()`.
- [ ] Unix socket integration test: server listens, client dials,
      every Runtime API op from M1-06 round-trips successfully
      over the real socket.
- [ ] Concurrent Invokes from 50 goroutines on one connection
      complete correctly (race-detector clean).
- [ ] Disconnect mid-call surfaces `runtime_unavailable` with
      `retryable: true` (Q6 fail-fast).
- [ ] Socket permissions enforced and asserted in tests.
- [ ] Graceful close: `Close()` drains pending callers with
      structured error, then returns.
- [ ] No production code calls this package yet — verified by
      grep.
- [ ] `go build ./...` clean.
- [ ] `go test -race ./internal/runtime/...` green.

## Blocked by

- M1-06 (in-memory codec test is the basis for promotion)

## Notes

- Pick the WS library with care: it lives forever in the
  dependency graph. `coder/websocket` (formerly `nhooyr.io/
  websocket`) preferred over `gorilla/websocket` (unmaintained).
- WSS (TLS) transport is M6 concern; M2 ships unix socket only.
- Frame size limits and read deadlines: pick sensible defaults
  (1 MiB max frame, 60s idle read deadline with ping-keepalive),
  document them.
