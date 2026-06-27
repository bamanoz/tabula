# Plugin-owned tool result store

Type: Bundle / Feature

Priority: P1

Status: Completed

Repos: `tabula-bundles`, `tabula-distrib`, `tabula`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Tool result storage previously existed as kernel/runtime-owned behavior. That makes a
product policy decision in the platform layer and blocks distros from swapping
different result rewrite strategies in or out.

## What to build

Move semantic tool-result storage into a bundle that subscribes to
`before_tool_result`.

The bundle should:

- read a large completed tool-result source,
- write the full result into per-session storage,
- return a bounded preview,
- attach `artifact://...` metadata,
- mark the result as truncated when the preview omits content.

Install that bundle in the distro and remove kernel-owned semantic storage.

## Acceptance criteria

- [x] A dedicated bundle handles large tool-result storage through
      `before_tool_result`.
- [x] The full result is durable and readable through `tool_result_read`.
- [x] The final driver-visible `tool.result` uses plugin-produced preview,
      `artifact`, and `truncated` fields.
- [x] Kernel/runtime no longer perform semantic storage as product policy.
- [x] Unit and installed tests cover a large result round-trip through the hook
      plugin.

## Files

- Add: `base/tool-result-store/*`
- Edit: `_lib/python/src/tabula_artifacts/__init__.py`
- Edit: `base/bundle.toml`
- Edit: `code-immune/distro.toml`
- Edit: `code/distro.toml`
- Edit: `internal/runtime/host/pool/pool.go`
- Edit: `internal/runtime/conn/conn.go`

## Verify

```bash
python3 -m unittest discover -s "base/tool-result-store" -p "test_*.py"
PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite plugin-fixture-execution --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib
go test ./internal/runtime/host/pool ./internal/runtime/conn ./internal/kernel
```

## Risk

Medium.

The risk is accidentally leaving a hidden emergency storage path in the
kernel/runtime and ending up with two competing sources of truth.

## Notes

- It is fine to keep transport guards, but not semantic storage policy, in
  the kernel/runtime.
- Keep write and read responsibilities together in `tool-result-store`; producer
  and reader are one user-facing capability.
