# WebSocket Hardening

Priority: High

Repos: `tabula`, `tabula-bundles`

## Problem

Kernel client WebSockets and gateway browser WebSockets lack consistent frame
limits, read deadlines, and protocol validation. Runtime API WebSockets already
set a frame limit; kernel clients do not.

## Evidence

- `tabula/internal/kernel/client.go`: `readPump` calls gorilla
  `ReadMessage()` without `SetReadLimit`, read deadlines, ping/pong handling,
  or close handling policy.
- `tabula/internal/runtime/codec/codec.go`: Runtime API sets
  `MaxFrameBytes = 1 << 20`, showing a stronger pattern exists.
- `tabula-bundles/gateways/gateway-web/daemon.py`: manual WebSocket parser
  accepts unmasked frames, does not check `Sec-WebSocket-Version`, has no frame
  size limit, and reads full payload lengths.

## Impact

Malicious local or remote clients can consume memory or hold connections open.
The risk increases if the kernel or gateway is exposed beyond loopback.

## Proposed Fix

- Add kernel client frame size limit aligned with Runtime API or a documented
  separate limit.
- Add read deadline and ping/pong heartbeat handling.
- Validate close frames and fail oversized/bad frames cleanly.
- Replace the gateway manual WebSocket code with a maintained library, or add
  mandatory masking, version check, size limits, and socket timeouts.

## Acceptance Criteria

- Oversized kernel client frames are rejected without high memory usage.
- Idle/dead WebSocket clients are cleaned up.
- Gateway rejects unmasked client frames and unsupported WebSocket versions.
- Tests cover kernel and gateway frame limits.
