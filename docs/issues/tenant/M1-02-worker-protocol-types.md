# M1-02 — Worker protocol types (runtime ↔ worker)

Status: done
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M1, §2.4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Define the Go types for the **worker protocol** — the inner
channel between the runtime daemon and a worker process it
spawned. Per ADR §10, this layer stays on stdio NDJSON; per Q2e
in the plan, it is intentionally distinct from the Runtime API.

This slice defines the types and the NDJSON framing helper. No
worker is actually spawned yet; that comes in M2.

Types:

- `WorkerInit` (runtime → worker, env-equivalent boot info:
  kernel_id, tenant_id, target_id, manifest snippet).
- `WorkerInitAck` (worker → runtime: ready or error).
- `WorkerCall` (runtime → worker: call_id, tool, args).
- `WorkerResult` (worker → runtime: call_id, ok, data | error).
- `WorkerShutdown` (runtime → worker, no payload, request clean
  exit).
- `WorkerError` (worker → runtime, async unsolicited: protocol
  errors, panic notifications).

NDJSON framing helpers:

- `WriteFrame(w io.Writer, msg any) error` — marshals to JSON,
  appends `\n`, writes atomically.
- `ReadFrame(r *bufio.Reader, into any) error` — reads to `\n`,
  unmarshals, handles EOF distinctly.

Location: new package `internal/runtime/worker/wire/`.

## Acceptance criteria

- [ ] All worker protocol types declared with field docstrings.
- [ ] JSON tags consistent with M1-01's conventions.
- [ ] Round-trip test for every type via `bytes.Buffer`.
- [ ] NDJSON splitter test: write three messages, read three back
      in order.
- [ ] EOF returns a distinguishable error from `ReadFrame` (so
      callers can tell "channel closed" from "decode error").
- [ ] Test for malformed JSON in a frame: returns protocol error,
      does not advance past the bad line.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/runtime/worker/wire/...` green.

## Blocked by

- M1-01 (shares envelope conventions and error-code constants)

## Notes

- Worker protocol is intentionally simpler than Runtime API: no
  Cancel (worker is killed via OS signal, not protocol), no
  Health (worker liveness checked via process state), no
  ListCapabilities (worker exists for one target).
- `WorkerInit` is sent as the first frame after the worker
  process starts; the worker must respond with `WorkerInitAck`
  before any `WorkerCall` is sent.
- Cold workers (skill default) receive exactly one `WorkerCall`,
  produce one `WorkerResult`, then exit. The protocol does not
  care; the runtime daemon decides when to shut down a worker.
