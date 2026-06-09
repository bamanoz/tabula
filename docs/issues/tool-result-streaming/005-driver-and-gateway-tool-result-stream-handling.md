# Driver and gateway tool result stream handling

Type: End-to-end / Feature

Priority: P2

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Once tool results can stream and stall independently of assistant text streams,
the driver and browser UI need explicit handling for pending, interrupted, and
late-arriving result events. Otherwise a broken stream can leave infinite
spinners, leaked browser memory, or invalid tool state merges.

## What to build

Add bounded live tool-result stream handling on the driver and gateway side.

This slice should cover:

- optional live preview events for tool-result streams,
- bounded buffering in browser state,
- timeout/interrupted UI states for non-terminating streams,
- reconciliation when the final bounded `tool.result` arrives,
- safe ignore behavior for late deltas after terminal completion.

## Acceptance criteria

- [x] Driver and gateway can represent a pending streamed tool result without
      blocking the rest of the turn forever.
- [x] Browser state caps live tool-result preview memory.
- [x] Interrupted or stalled tool-result streams become explicit failed or
      interrupted states rather than infinite pending UI.
- [x] A final `tool.result` closes any matching pending stream state cleanly.
- [x] Tests cover replay, reconnect, and late-delta edge cases.

## Files

- Edit: `internal/kernel/protocol.go`
- Edit: `tabula-bundles/_lib/python/src/tabula_drivers/driver_runtime.py`
- Edit: `tabula-bundles/gateways/gateway-web/daemon.py`
- Edit: `tabula-bundles/gateways/gateway-web/web/src/main.tsx`
- Edit: `tabula-bundles/gateways/gateway-web/web/src/toolTimeline.ts`
- Edit: `tabula-bundles/gateways/gateway-web/web/src/toolTimeline.test.ts`
- Edit: `tabula-bundles/gateways/test_gateway_web.py`

## Verify

```bash
PYTHONPATH="/Users/mak/src/tabula-bundles/_lib/python/src" python3 -m unittest gateways.test_gateway_web
npm test
```

## Notes

- The driver intentionally consumes only the final bounded `tool.result`; live
  preview events are gateway/browser-facing optional UX and are not sent to the
  model provider.

## Risk

Medium.

The main risk is adding another pending stream state that conflicts with the
existing assistant text/reasoning stream handling.

## Notes

- Keep live preview optional and bounded. The final model-facing `tool.result`
  remains the source of truth.
- Reuse existing `start/delta/end` patterns where possible, but do not force the
  client to replay unbounded raw chunks.
