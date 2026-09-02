# Externalize tenant init metadata materialization

**Type:** AFK  
**Status:** proposed

## What to build

Remove distro/product configuration parsing from `internal/cli/tabula/tenant_init_meta.go`. Distro installers or materializers must write one opaque installed metadata artifact for the runtime/client adapter that needs it.

Core may load and forward bounded opaque metadata but must not understand prompt-builder module names, workspace schemas, Claw compatibility fields, or assistant kinds.

## Evidence

The core CLI currently parses `[agent].prompt_builder`, `[workspace].project_root`, legacy `[claw].workspace.path`, and emits `kind: "assistant"`. The same values are authored by `../tabula-distrib` and consumed by driver/bundle code.

## Acceptance criteria

- [ ] Core CLI contains no `prompt_builder`, workspace product schema, Claw field, or assistant-kind knowledge.
- [ ] Each distro materializes the metadata required by its installed driver/gateway components.
- [ ] Core treats the materialized document as bounded opaque JSON with tenant scope.
- [ ] The legacy `[claw].workspace.path` fallback is deleted without an alias.
- [ ] Code-immune, Claw, and harness-bench installed tests prove their metadata behavior.
- [ ] Runtime layout documentation and `tabula-guide` identify the metadata owner and path.

## Blocked by

None - can start immediately.
