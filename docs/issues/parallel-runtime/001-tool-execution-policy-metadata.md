# Tool execution policy metadata

Type: Runtime / Feature

Priority: P1

Status: Planned

Repos: `tabula`

## Parent

`docs/issues/parallel-runtime/README.md`

## Problem

The runtime has no manifest-level vocabulary for "these tools may run in
parallel" versus "these tools must serialize with this conflict set". As a
result, every warm plugin effectively behaves as a single-flight worker.

We need a plugin-declared contract that remains internal to the runtime and is
not exposed as user configuration.

## What to build

Add tool-level execution policy metadata to plugin manifests and runtime wire
types:

- `concurrency = "serial" | "parallel"`
- `execution_group = "..."`
- `conflicts_with_groups = ["..."]`

Defaulting rules:

- `concurrency = "serial"`
- `execution_group = <tool-name>`
- `conflicts_with_groups = [execution_group]`

These defaults must preserve the current runtime behavior for all existing
plugins.

## Acceptance criteria

- [ ] Plugin manifest parsing accepts explicit execution policy fields.
- [ ] Missing fields default to current serial behavior.
- [ ] Invalid `concurrency` values fail manifest validation clearly.
- [ ] Runtime `ToolSpec` carries the normalized policy metadata.
- [ ] No behavior change yet: existing tests for serial warm plugins still pass.

## Files

- Edit: `internal/runtime/host/manifest/manifest.go`
- Edit: `internal/runtime/wire/types.go`
- Edit: `internal/runtime/host/manifest/manifest_test.go`

## Verify

```bash
go test ./internal/runtime/host/manifest
```

## Risk

Low.

This issue only adds metadata and defaults. The main risk is accidentally
changing default behavior for old manifests.

## Notes

- Keep field names runtime-oriented and internal. They are not product-facing.
- Do not add any user config path for overriding these values.
