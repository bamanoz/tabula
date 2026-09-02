# Generalize the bounded attempt-output contract

**Type:** HITL  
**Status:** proposed

## What to build

Define and approve an architecture change that removes provider/presentation vocabulary from the authoritative aggregate while preserving bounded, sequenced, fenced attempt output.

Kernel should validate an opaque bounded output kind and payload. Driver SDKs own producer vocabulary; gateway adapters own client-facing topic and presentation mapping. Unknown kinds must never be silently relabeled as `stream.delta`.

## Evidence

- `internal/agent/output_service.go` hardcodes `reasoning`, `usage`, `provider.retry`, `provider.error`, `compaction`, and `tool.result`.
- `internal/kernel/client_v4.go` repeats the list and maps unknown output to `stream.delta`.
- The same list is duplicated in the Python driver SDK.

## Required design work

- Add a new ADR that supersedes the output-taxonomy portions of ADR 0025/protocol v4.
- Define bounds, identifier syntax, reserved kernel kinds if any, and gateway capability negotiation.
- Decide whether client replay exposes producer kind directly or an adapter-owned mapped event type.

## Acceptance criteria

- [ ] Maintainer approves the new output ownership boundary.
- [ ] Aggregate/output service contains no provider-specific names.
- [ ] Driver and client adapters have one explicit mapping contract.
- [ ] Unknown valid producer kinds remain identifiable and are never converted to another kind.
- [ ] Sequence, duplicate, conflict, retention, and cursor guarantees remain unchanged.
- [ ] Driver, gateway, kernel, SDK, and installed replay tests cover a custom third-party output kind.

## Blocked by

None. This is an architecture gate for output-contract changes.
