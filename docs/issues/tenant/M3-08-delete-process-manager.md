# M3-08 — Delete `process_manager.go`

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§10)

## What to build

Physically remove `internal/kernel/process_manager.go` and
every caller. After M3-07 the kernel no longer uses it for
skill execution; this slice is the cleanup with no observable
behavior change.

Per `tabula/AGENTS.md` "no legacy / no backward compatibility":
delete in-place, no shim, no rename-to-deprecated.

Components:

- Delete `internal/kernel/process_manager.go`.
- Delete every reference to `LocalProcessLauncher`,
  `ProcessLauncher`, `SkillExec` (or whatever the type names
  are — confirm by grep before merging).
- Delete tests that exercised those types directly. Their
  intent (skill execution end-to-end) is now covered by
  runtime-side tests (M3-02..04) and integration tests
  (M3-09).
- Update / remove any kernel-side initialization that
  constructed a `ProcessLauncher`.
- Lint guard from M2-08: flip the
  `grep -r "exec.Command" internal/kernel/` check to "match
  nothing". Update CI config accordingly.
- Documentation pass:
  - Any kernel doc that mentioned "skills run as kernel
    subprocess" → update to "skills run on the runtime via
    cold workers".
  - `docs/plans/REMOTE_RUNTIME.md` §6 M3 status note: mark
    deletion done.

## Acceptance criteria

- [ ] `process_manager.go` and tests deleted.
- [ ] `go build ./...` clean.
- [ ] `go test -race ./...` green.
- [ ] `grep -r "ProcessLauncher\|process_manager\|SkillExec" internal/`
      returns nothing (or only documented exceptions, which
      should be zero).
- [ ] CI lint guard updated to forbid `exec.Command` anywhere
      in `internal/kernel/`.
- [ ] Manual smoke: full session with skill calls works
      end-to-end (verify against M2-08 `runtime-smoke` job
      extended to cover at least one skill call).
- [ ] Plan doc M3 section updated.

## Blocked by

- M3-07 (kernel no longer uses the file in production)

## Notes

- Smallest possible diff modulo file deletion; intentionally
  no refactoring of remaining kernel code in this slice. If
  removal exposes opportunities to simplify (e.g. `Hub` no
  longer needs a launcher field), capture them as a follow-up
  issue rather than expanding this slice.
- After this lands, **the kernel is process-mgmt-free**: no
  `os/exec` calls in `internal/kernel/` at all. That is the
  load-bearing invariant for the rest of the program.
