# Before tool result hook contract

Type: Runtime / Feature

Priority: P1

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Today the kernel decides large-result artifacting itself. We need a generic seam
where a distro-installed bundle can rewrite a completed tool result before the
driver sees it, without baking artifact policy into the runtime host or kernel.

## What to build

Add a modifying `before_tool_result` hook.

The hook should receive either:

- a small inline result payload, or
- a kernel-managed source/spool reference for large completed results.

The hook may return rewritten `output`, `artifact`, and `truncated` fields. Hook
failure behavior must stay explicit:

- deliverable small results may fail open,
- undeliverable large results must fail explicitly if no rewrite makes them
  bounded.

## Acceptance criteria

- [x] Kernel hook metadata includes a new `before_tool_result` event.
- [x] Hook payloads support both inline small results and large source/spool
      references.
- [x] Hook replies can rewrite final `output`, `artifact`, and `truncated`
      fields before `tool.result` is emitted.
- [x] Hook timeout or failure does not silently drop or truncate a large result.
- [x] Tests cover small-result fail-open behavior and large-result explicit
      failure behavior.

## Files

- Edit: `internal/kernel/hooks.go`
- Edit: `internal/kernel/tool_service.go`
- Edit: `internal/kernel/tool_hook_test.go`
- Edit: `internal/kernel/kernel_test.go`
- Edit: `internal/kernel/message.go`
- Edit: `internal/kernel/broadcast.go`
- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/protocol.py`
- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/api.py`

## Verify

```bash
go test ./internal/kernel
PYTHONPATH="/Users/mak/src/tabula-bundles/_lib/python/src" python3 -m unittest _lib.python.tests.test_contract
```

## Risk

Medium.

The main risk is creating an ambiguous hook contract that leaves large-result
failure semantics undefined.

## Notes

- Keep this hook non-permission-oriented. It rewrites delivery, not authorization.
- Decide and document whether error tool results are also eligible for rewrite;
  default should be yes.
- Keep the final driver-facing result schema backward-compatible where possible:
  `output`, `artifact`, `truncated`.
