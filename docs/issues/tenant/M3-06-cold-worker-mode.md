# M3-06 — Cold worker mode in the runtime pool

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4, §6)

## What to build

Make the runtime pool (M2-03) handle two worker lifecycles
side by side:

- `warm` (plugins): spawn once per `(kernel, tenant, target)`,
  reuse for many calls, serialize concurrent calls per worker.
- `cold` (skills): spawn fresh per Invoke, exit after one
  result. No pool reuse, no per-key serialization (parallel
  calls = parallel processes).

Components:

- Extend `pool.Pool` (M2-03) to switch behavior on
  `manifest.WorkerMode`:
  - `warm`: existing path.
  - `cold`: `Spawn → Invoke → Wait`. Skip pool registration.
- Concurrency model in cold mode:
  - Each Invoke gets its own subprocess.
  - No mutex on `(kernel, tenant, target)` — parallel calls
    spawn parallel processes.
  - Cap total concurrent cold workers per tenant (config
    default: 16, configurable via `runtime.toml`
    `pool.cold_workers_per_tenant_max`). Excess Invokes block
    on a semaphore with a 30s timeout, then return
    `runtime_busy` (new error code — add to wire types if
    missing, otherwise reuse `internal_error` with a
    structured message; pick whichever the wire vocabulary
    supports today, document choice).
- Cancel semantics in cold mode: SIGTERM → 5s grace → SIGKILL,
  same as warm.
- Reload: cold mode is unaffected by reload (no persistent
  state); warm mode evicts (existing behavior).
- Metrics / introspection:
  - Pool reports `warm_workers_active`, `cold_workers_active`,
    `cold_workers_queued` per tenant via existing
    `Health` / `ListCapabilities` channels (or a new lightweight
    op — pick whichever the existing surface supports without
    growing the wire schema).

## Acceptance criteria

- [ ] Plugin Invoke uses warm path (verified by spawning one
      process and asserting reuse across 10 calls).
- [ ] Skill Invoke uses cold path (verified: 5 sequential
      Invokes spawn 5 distinct PIDs).
- [ ] 10 parallel Invokes of the same skill from the same
      tenant: 10 parallel processes, all complete.
- [ ] 20 parallel Invokes when limit is 16 → 16 immediate, 4
      queued briefly, all complete (no error).
- [ ] 100 parallel Invokes when limit is 16: queue overflow
      → some return `runtime_busy` after 30s wait.
- [ ] Cancel mid-Invoke (cold): subprocess receives SIGTERM,
      exits within grace window.
- [ ] Race-clean.

## Blocked by

- M3-02 (a real cold worker — bash harness — must exist to
  test against)

## Notes

- Limit of 16 is chosen to match a typical interactive workload
  (a handful of parallel skill invocations during a single
  agent turn) without exhausting OS process limits. Tune in
  ops if real workloads diverge.
- We deliberately do not pool cold workers (e.g. "warm-skill
  cache"): per ADR §4 commit, skill workers are one-shot. If
  a skill genuinely needs persistent state, it should be a
  plugin instead. Document this decision in the pool package
  godoc.
