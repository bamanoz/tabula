# AM-18 — Launcher binding resolution

Status: completed
Type: AFK
Repo: tabula, tabula-bundles, tabula-distrib
Labels: needs-triage, area/installer, area/clients, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add end-to-end coverage for launcher app selection.

Launchers should resolve app and kernel through explicit CLI args, environment
variables, nearest directory binding, and default binding. The selected app id
must be passed as `tenant_id` when the client joins the kernel.

## Acceptance criteria

- [x] Explicit `--app` / `--kernel` path works.
- [x] `TABULA_APP_ID` / `TABULA_KERNEL_ID` path works.
- [x] Nearest directory binding path works.
- [x] Default binding path works.
- [x] Missing app selection fails with an actionable error.
- [x] Resolved app id is passed as `tenant_id` on join.

## Blocked by

- AM-14.

## Notes

- Bindings are technical selection rules, not workspace/global platform scopes.
- Verified by Claw launcher tests and SDK unit coverage for explicit, env,
  directory, default, and missing-selection paths.
