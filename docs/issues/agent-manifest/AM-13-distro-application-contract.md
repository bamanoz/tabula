# AM-13 — Distro application contract

Status: done
Type: HITL
Repo: tabula, tabula-distrib
Labels: needs-triage, area/installer, area/distro-integration, area/docs

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Lock down the stable contract between `tabula-install app apply/run` and a
distro-owned application materializer.

The contract should define how the installer passes the manifest path, app lock,
tenant dir, expanded values, local overrides, and dry-run/audit mode to the
distro. It must preserve the architecture boundary: the generic installer owns
mechanical apply/run orchestration, while the distro owns the meaning of
`[values]`, prompt policy, and product-specific materialization.

## Acceptance criteria

- [x] Contract states what the generic installer owns and what the distro
      materializer owns.
- [x] Contract covers validation, dry-run, apply, run, and audit behavior.
- [x] Contract defines failure behavior and diagnostic output.
- [x] Contract explicitly says `[values]` are opaque to the platform.
- [x] Docs or ADR are updated before implementation slices depend on it.

## Blocked by

None - can start immediately.

## Notes

- Do not make `values.workspace`, `values.prompt`, `values.tools`, or
  `values.memory` generic platform concepts.
- Do not add app apply/run behavior to the `tabula` kernel binary.

## Progress

- Added `docs/APP_MATERIALIZER_CONTRACT.md`.
- Linked ADR 0002 to the materializer contract.
- Extended materializer invocation with lock, phase, and dry-run environment
  inputs.
