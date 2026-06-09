# Plugin-owned tool result artifacting

Type: Bundle / Feature

Priority: P1

Status: Completed

Repos: `tabula-bundles`, `tabula-distrib`, `tabula`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Artifacting currently exists as kernel/runtime-owned behavior. That makes a
product policy decision in the platform layer and blocks distros from swapping
different result rewrite strategies in or out.

## What to build

Move semantic artifacting into a bundle that subscribes to
`before_tool_result`.

The bundle should:

- read a large completed tool-result source,
- write the full result into artifact storage,
- return a bounded preview,
- attach `artifact://...` metadata,
- mark the result as truncated when the preview omits content.

Install that bundle in the distro and remove kernel-owned semantic artifacting.

## Acceptance criteria

- [x] A dedicated bundle handles large tool-result artifacting through
      `before_tool_result`.
- [x] The full result is durable and readable through the artifact read surface.
- [x] The final driver-visible `tool.result` uses plugin-produced preview,
      `artifact`, and `truncated` fields.
- [x] Kernel/runtime no longer perform semantic artifacting as product policy.
- [x] Unit and installed tests cover a large result round-trip through the hook
      plugin.

## Files

- Add: `base/tool-result-artifacts/*`
- Edit: `_lib/python/src/tabula_artifacts/__init__.py`
- Edit: `base/bundle.toml`
- Edit: `code-immune/distro.toml`
- Edit: `code/distro.toml`
- Edit: `internal/runtime/host/pool/pool.go`
- Edit: `internal/runtime/conn/conn.go`

## Verify

```bash
python3 -m unittest discover -s "base/tool-result-artifacts" -p "test_*.py"
PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src" python3 -m tabula_testbed_runner.cli run --tabula-root "/Users/mak/src/tabula" --suite plugin-fixture-execution --source tabula-bundles=/Users/mak/src/tabula-bundles --source tabula-distrib=/Users/mak/src/tabula-distrib
go test ./internal/runtime/host/pool ./internal/runtime/conn ./internal/kernel
```

## Risk

Medium.

The risk is accidentally leaving a hidden emergency artifacting path in the
kernel/runtime and ending up with two competing sources of truth.

## Notes

- It is fine to keep transport guards, but not semantic artifacting policy, in
  the kernel/runtime.
- Prefer a new dedicated artifacting bundle name over overloading `base/artifacts`
  if read and write responsibilities become confusing.
