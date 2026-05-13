# M2-08 — CI integration: build matrix + smoke

Status: done
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/ci, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§9)

## What to build

CI plumbing for the new two-binary world. Until this lands,
M2-07's atomic cutover is unverified outside developer
machines.

Components:

- Build matrix: `tabula` + `tabula-runtime` produced for the
  same target list current CI uses (likely linux/amd64,
  linux/arm64, darwin/amd64, darwin/arm64). Both binaries ship
  in the same release artifact set.
- New CI job `runtime-smoke`:
  1. Build both binaries.
  2. Stand up a clean `$TABULA_HOME` in temp dir.
  3. Run `bootstrap.sh` (M2-07).
  4. Wait for `tabula status --json` to report
     `kernel.running == true && len(runtimes) > 0`.
  5. Invoke at least one tool from a migrated bundle plugin
     (pick the simplest — likely `base/sessions` or
     `base/hook-logger`) and assert success.
  6. Tear down (`tabula serve` SIGTERM, verify `tabula-runtime`
     exits, no leftover processes).
- Race detector run for `internal/runtime/...` on the existing
  `go test` job (extend the matrix, do not duplicate the job).
- Testbed integration: existing testbed suite (`tools/tabula-
  testbed`) must pass against the new install path.
- Lint guard (optional but recommended):
  - `grep -r "exec.Command" internal/kernel/` should match
    only `process_manager.go` until M3. CI fails on additional
    matches. Single-line make target so M3 can flip it to
    "match nothing" trivially.
  - `grep -r "tabula_plugin_sdk\." tabula-bundles/` should
    match no legacy symbol names (legacy list documented in
    M2-04).
- Release script (`scripts/release.sh` or whatever convention
  the repo uses): include `tabula-runtime` in the release
  tarball.

## Acceptance criteria

- [ ] CI matrix produces both binaries on every supported
      platform.
- [ ] `runtime-smoke` job green on clean checkout.
- [ ] Race-detector test of `internal/runtime/...` runs in CI.
- [ ] Testbed CI job green against new install layout.
- [ ] Lint guards in place (or explicitly skipped with
      reasoning if the repo's CI conventions disallow grep
      gates).
- [ ] Release artifact contains `tabula-runtime` binary.
- [ ] Documentation updated: any developer-onboarding doc
      mentioning "build tabula" now mentions both binaries.

## Blocked by

- M2-07 (atomic cutover landed; without it, `runtime-smoke`
  has nothing to verify)

## Notes

- This issue is intentionally separate from M2-07 to keep that
  change reviewable. M2-07 may land with `runtime-smoke` flagged
  as `continue-on-error` until M2-08 lands fully green —
  document the gate in the M2-07 PR description.
- Cross-repo CI for `tabula-bundles` (M2-04) is a separate
  concern living in that repo's CI; out of scope for this
  issue.
- M3 will revisit this when `process_manager.go` deletion lifts
  the `exec.Command` lint exception.
