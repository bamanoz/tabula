# Gateway Web Observability And Switch Stress Tests

Priority: Medium

Repos: `tabula-bundles`, `tabula`

## Problem

Gateway-web failures are currently hard to diagnose. Access logs show WebSocket
upgrades, but not why a switch loaded, failed, was superseded, or ignored stale
events.

## Evidence

- Reconnect storms were visible only as repeated `GET /api/ws ... 101` lines.
- Bootstrap failures surfaced as empty HTTP responses until the traceback was
  inspected manually.
- Session switch delays had to be inferred from kernel client lifecycle logs.

## Impact

Operators and tests cannot easily distinguish provider latency, bootstrap
failure, replay failure, stale event drops, and multi-tab ownership changes.

## Proposed Fix

- Add structured gateway-web logs for bootstrap, tenant resolution, switch start,
  replay done, stale event ignored, switch ready, superseded, and replay errors.
- Include `client_id`, `tenant_id`, `session`, `switch_id`, and event type.
- Add a focused switch stress test that repeatedly switches A/B/A and asserts no
  stuck empty state.
- Add a multi-tab stress test for same-session ownership.

## Acceptance Criteria

- Logs can explain a failed switch without reading tracebacks.
- Stress tests exercise fast switching and same-session multi-tab behavior.
- Testbed coverage runs against installed gateway-web, not only source imports.
- Kernel logs no longer need to be the primary way to diagnose web gateway
  session switch behavior.
