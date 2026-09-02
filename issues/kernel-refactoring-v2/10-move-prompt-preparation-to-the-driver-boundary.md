# Move prompt preparation to the driver boundary

**Type:** HITL  
**Status:** proposed

## What to build

Define and approve a new ownership boundary in which kernel commits opaque prepared attempt context but does not assemble prompts, select prompt hooks, or construct provider-visible tool catalogs.

A driver-side or runtime-side preparation component obtains tenant-scoped capabilities and bundle contributions, builds the opaque preparation result before permit, and submits it under the current fence. Kernel validates authority and commits the bytes without understanding their schema.

## Evidence

- `internal/kernel/turn_lifecycle.go:prepareTurnContext` assembles prompt context, tools, `before_prompt_build`, and `before_turn` data.
- `internal/kernel/runtime_catalog.go` refresh behavior depends on the `before_prompt_build` hook.
- Drivers already own providers, model selection, prompts, history, and compaction.
- ADR 0007, ADR 0008, ADR 0030, and protocol v4 currently encode the old boundary.

## Required design work

- Add a new ADR superseding affected prompt-build and synchronous catalog decisions.
- Define preparation authority, timeouts, retries, catalog revision consistency, and fence validation.
- Preserve the rule that preparation cannot perform provider calls or side effects.
- Define how gateways receive their own UI/tool metadata without sharing the driver prompt path.

## Acceptance criteria

- [ ] Maintainer approves the new preparation contract and migration sequence.
- [ ] Kernel does not parse or construct prompt context, prompt tools, prompt-builder metadata, or turn-context schemas.
- [ ] Prepared context remains durable and redeliverable for the exact attempt.
- [ ] Tool catalog readiness is deterministic for the first turn after install/reload.
- [ ] Bundle hooks retain required composition behavior outside kernel.
- [ ] Driver crash before permit remains safely retryable; no preparation path performs external side effects.
- [ ] Protocol, ADRs, driver SDK, gateways, `tabula-guide`, and installed tests are updated.

## Blocked by

None. This is an architecture gate for prompt-preparation changes.
