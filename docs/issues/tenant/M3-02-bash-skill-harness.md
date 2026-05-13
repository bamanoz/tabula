# M3-02 — Bash skill harness

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

The first concrete skill harness: a generated worker process
that wraps a bash skill `exec` template and speaks the worker
protocol (M1-02). After this slice, a bash skill (the
canonical example: any tool whose `exec` starts with `bash` or
plain shell command) runs through the runtime daemon.

Components:

- `internal/runtime/host/harness/bash/`:
  - `Spawn(ctx, manifest, callID, tenantCtx)` — `os/exec.Command`
    invoking the resolved `exec` template. NOT through `sh -c`
    by default — split the exec string with shellwords so we
    behave like a real exec (see Q-skill-exec-mode below).
  - Sets working dir to skill root (`SKILL_DIR`).
  - Env passthrough subset matches plugin worker env (M2-03)
    plus skill-specific:
    - `TABULA_SKILL_DIR` — path to the skill directory.
    - `TABULA_TOOL_NAME` — current tool name.
    - `TABULA_CALL_ID`.
    - `TABULA_TENANT_ID`, `TABULA_KERNEL_ID`, `TABULA_TARGET_ID`.
    - `TABULA_HOME`.
  - Pipes worker-protocol NDJSON over stdin/stdout. The harness
    is "speaking the protocol" on behalf of the bash script,
    which itself does not know the protocol. See "Wrapper
    behavior" below.
- Wrapper behavior (`cold` mode):
  1. On spawn, harness sends `WorkerInit{tenant_ctx, target_id,
     tool_name, args}` to the bash subprocess via... no — the
     bash script does not parse this. Instead the **harness
     transforms** the `WorkerCall` into the `exec` invocation:
     args are passed as JSON on stdin to the bash process.
  2. Bash script reads stdin (the skill author's responsibility:
     `args="$(cat)"`) and writes its result JSON to stdout.
  3. Harness wraps stdout into `WorkerResult{ok: true, data: <stdout>}`
     and forwards to runtime, then exits.
  4. Non-zero exit code → `WorkerResult{ok: false, error: {code:
     "skill_exec_failed", message: <stderr tail>}}`.
- `exec` template variable expansion: `${SKILL_DIR}`,
  `${TABULA_HOME}`. Template parser is a simple `${var}`
  expander, not full shell interpolation.
- Wire bash harness into `policy.bare` (M2-03) as a new path
  alongside plugin worker spawn. Manifest `harness_kind ==
  "bash"` selects it.

### Q-skill-exec-mode

Current `process_manager.go` uses `sh -c <cmd>`. Should the
new harness do the same?

Decision: NO `sh -c` by default. Skills' `exec` strings in the
existing bundles are simple `python3 path/script.py args`
patterns; shellwords-splitting is sufficient and avoids quoting
hazards. If a skill genuinely needs shell features, it can
spell them itself: `exec: "bash -c '...'"`. Document this.

## Acceptance criteria

- [ ] Test fixture skill (`testbed-echo` from
      `tabula-bundles/test-fixtures/`) executed end-to-end
      through the bash harness returns expected output.
- [ ] Bash skill that exits non-zero produces `skill_exec_failed`
      `WorkerResult{ok: false}` with stderr captured.
- [ ] `${SKILL_DIR}` and `${TABULA_HOME}` substituted correctly
      in the exec template.
- [ ] After `WorkerResult` is sent, harness exits cleanly (cold
      mode); pool (M3-07) does not reuse the worker.
- [ ] Concurrent calls to the same bash skill from two tenants
      spawn two distinct processes with correct env isolation.
- [ ] Cancel mid-execution: SIGTERM → 5s grace → SIGKILL
      (matches plugin worker cancel semantics).
- [ ] Race-clean.

## Blocked by

- M3-01 (manifest reader produces `harness_kind`)
- M2-03 (worker spawn / pool infrastructure to plug into)

## Notes

- This harness covers the simplest case. Python and Node get
  their own slices (M3-03, M3-04) so we can test each
  independently.
- "Skills don't speak the worker protocol" is the key insight:
  the harness lies on the skill's behalf. Author surface
  unchanged from current `process_manager.go` behavior — they
  read stdin, write stdout, exit. ADR §4 commitment.
- Until M3-07 wires routing, this harness is reachable only via
  the runtime's internal `Invoke` plumbing — no kernel tool
  call hits it yet. M3-02 is correctness-tested in isolation
  via runtime integration tests.
