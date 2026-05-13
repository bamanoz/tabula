# AM-17 — Cold managed app run lifecycle

Status: completed
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/runtime, area/testbed

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Prove that `tabula-install app run` can start from no running kernel or runtime
and bring up the managed topology declared by the manifest.

The command should start or reuse the matching managed kernel/runtime, wait for
readiness, then launch the app path. Failures should identify whether kernel
startup, runtime attach, materialization, or launcher startup failed.

## Acceptance criteria

- [x] Test starts with no running kernel process.
- [x] Manifest `[kernel] mode = "managed"` starts a kernel or reuses only a
      matching healthy kernel.
- [x] Manifest `[[runtimes]] mode = "managed"` starts or attaches runtime for
      the app tenant.
- [x] Readiness uses the same protocol as real clients, not only `/health`.
- [x] Failure diagnostics identify kernel startup, runtime attach, or app launch
      failures.

## Blocked by

- AM-14.

## Notes

- The current AM-12 suite reuses the testbed kernel. This issue covers the cold
  managed lifecycle separately.
- Verified by `claw-app-manifest`, which creates a secondary cold home and starts
  managed `tabula serve` through `tabula-install app run`.
