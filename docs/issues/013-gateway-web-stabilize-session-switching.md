# Gateway Web Stabilize Session Switching

Priority: High

Repos: `tabula-bundles`

## Problem

Gateway-web session switching is not currently a reliable atomic operation. A
recent attempted fix made each browser-side session switch recreate the kernel
client, producing `client connected` / `client disconnected` churn and visible
latency. Switch failures can leave the UI on the fallback `Ready` empty state.

## Evidence

- Kernel logs during session switch showed a new `gateway-web` client connecting,
  an older one disconnecting, then the new client joining the target session.
- `gateway-web` logs showed `Bad file descriptor` in the kernel pump after the
  reconnect-per-switch approach closed the old socket while its reader thread was
  still polling it.
- The UI can show `Ready / Session is ready` even when the target session has
  history.

## Impact

Switching sessions feels slow and can strand the browser on an empty state. The
kernel receives unnecessary client lifecycle churn, which also makes reconnect
and session ownership bugs harder to diagnose.

## Proposed Fix

- Keep a stable kernel connection for each browser WebSocket; do not reconnect
  the kernel client for ordinary session switches.
- Treat closed kernel sockets in reader threads as normal shutdown when the
  browser connection is closing.
- Add regression coverage proving that switching sessions does not create a new
  kernel connection.
- Ensure switch failures surface as explicit loading/error UI, not the generic
  empty `Ready` state.

## Acceptance Criteria

- Repeated UI switches between two sessions do not emit kernel
  `client connected` / `client disconnected` pairs.
- Gateway-web logs do not contain `Bad file descriptor` during normal switches.
- A failed replay or join shows an explicit error/retry state.
- Unit or testbed coverage exercises session switching without kernel reconnect.
