# M2-03 — Worker spawn via bare PluginExecPolicy

Status: done
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§3, §4)

## What to build

Implement real worker spawn inside `tabula-runtime`. After this
slice, the daemon can host plugin workers and answer `Invoke`
end-to-end. Skills are still kernel-side (`process_manager.go`)
until M3 — this slice is plugin-only.

Components:

- `internal/runtime/host/policy/bare/`: full `barePolicy.Spawn`
  implementation:
  - `os/exec.CommandContext` with manifest's entrypoint,
    `Cmd.Dir = manifest.RootDir`.
  - Inherit stdin/stdout pipes; wire to worker protocol NDJSON
    framing helpers from M1-02.
  - Set env: `TABULA_KERNEL_ID`, `TABULA_TENANT_ID`,
    `TABULA_TARGET_ID`, plus passthrough subset (`TABULA_HOME`,
    `PATH`, `HOME`, `LANG`, …).
  - Send `WorkerInit`, await `WorkerInitAck` with timeout (10s
    default).
- `internal/runtime/host/policy/bare/worker.go`: `Worker`
  implementation:
  - `Call`: send `WorkerCall`, await matching `WorkerResult` by
    `call_id`, return.
  - `Shutdown`: send `WorkerShutdown` → wait 5s → SIGTERM →
    wait 5s → SIGKILL.
  - `Wait` blocks on process exit; surfaces `ExitError`.
  - `IsAlive` checks process state.
- `internal/runtime/host/pool/`: worker pool keyed by
  `(kernel_id, tenant_id, target_id)`:
  - Lazy spawn on first Invoke for that triple.
  - Warm reuse for plugins (M2 default; cold is a future Q8
    detail past skill migration in M3).
  - Per-key mutex so concurrent Invokes for same key serialize
    correctly on a single warm worker.
  - Eviction on `Reload` (per-target or full).
- `internal/runtime/host/manifest/`: read `plugin.toml` files from
  configured search dirs at startup; build `target_id →
  manifest` map. Reload re-reads.
- `ListCapabilities` returns the actual list of available
  targets (not empty anymore).
- `Invoke` routes to pool, returns real `WorkerResult` data.
- `Reload` invalidates pool entries for affected targets.

## Acceptance criteria

- [ ] Test plugin (one-line bash echo speaking NDJSON) spawns
      and answers a `read_file`-shaped Invoke.
- [ ] Multiple Invokes for same `(tenant, target)` reuse one
      worker process (warm).
- [ ] Worker crash mid-call: next Invoke spawns a fresh worker;
      first Invoke returns `internal_error` with crash details.
- [ ] Concurrent Invokes for different tenants on the same
      target spawn distinct worker processes (no plugin sharing,
      ADR §6).
- [ ] `tenant_id` value visible to worker via env (assert in
      test plugin).
- [ ] `Cancel(callID)` mid-Invoke: worker receives no signal in
      M2 (Cancel signal propagation is part of Q6 but is
      worker-protocol concern — runtime returns
      `cancel_ack` and abandons the call, worker output for
      that call is discarded).
- [ ] `Reload(target=fs)` evicts the matching worker; next
      Invoke spawns fresh.
- [ ] `tabula-runtime` exit cleanly reaps all workers.
- [ ] Race-detector clean.

## Blocked by

- M2-02 (binary skeleton to host this logic)

## Notes

- Cancel propagation to running worker is intentionally weak in
  M2: runtime returns `cancel_ack` and stops listening for that
  call_id, but the worker may still be computing. M3 unified
  worker model adds per-call abort capability via worker
  protocol if it proves needed; until then, long-running calls
  rely on per-call timeout (Q6c).
- Cold mode (kill worker after one call) is not exercised in
  M2 — no skill execution lives in runtime yet. The mode flag
  is plumbed but defaults to warm.
- Plugin manifest schema is whatever `tabula-bundles` ships
  today; do not change it in this slice. M2-04 covers SDK side.
