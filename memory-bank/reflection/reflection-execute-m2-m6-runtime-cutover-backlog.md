# Strategic Reflection: M2-M6 Runtime Cutover Backlog Slice
Date: 2026-05-04

## System Summary
The completed slice made the local-runtime cutover real in `tabula`: richer Runtime API and worker protocol semantics, runtime-backed kernel catalog/hook dispatch, managed sibling `tabula-runtime` supervision for `serve` and `run`, runtime config/reload handoff, stronger dual-binary smoke coverage, and post-SECURITY hardening of runtime log/snapshot trust boundaries.

This reflection treats the archived outcome as the completed BUILD + SECURITY slice recorded in `memory-bank/progress.md`, not as proof that the entire grouped M2-M6 backlog is finished. Full M2-07 no-legacy deletion, companion M2-04/M2-04b evidence capture, release/bootstrap installed-layout proof, and most M3-M6 scope remain follow-up work.

## 1. Overall Outcome
- **Scope coverage**: Strong for runtime-hosted plugin control, managed local runtime supervision, runtime-backed kernel dispatch/read-model work, installed-layout smoke guardrails, and security hardening.
- **Requirements coverage assessment**:
  - M2-07 is materially advanced and the managed-runtime path is live, but the no-legacy deletion gate is not fully closed because compiled kernel stdio plugin ownership/runtime surfaces still remain.
  - M2-08 status/runtime smoke is materially stronger in the isolated testbed, but release-archive/bootstrap installed-layout proof is still incomplete.
  - M2-04/M2-04b companion SHA/PR/test evidence is not captured in this task record.
  - M3-M6 are not materially completed in this repo slice.
- **Quality attribute achievement**:
  - Kernel genericity was preserved: runtime owns workers/processes; kernel owns catalog, hooks, status, and policy.
  - SECURITY passed on attempt 2 with 0 blocking findings, 1 warning, and 0 degraded checks.
  - Observability improved through runtime PID/capability/teardown smoke checks, with intentionally coarse diagnostics at trust boundaries.

## 2. Process Effectiveness
- **Metrics**:
  - BUILD required 13 principal implementation slices plus 2 effective security re-entry hardening passes.
  - SECURITY attempts: 2.
  - Rework count: 1 targeted BUILD re-entry after SECURITY attempt 1 found a blocking log/PII issue.
  - Warning findings at close: 1 non-blocking internal-wire warning.
  - Three later re-entry agents correctly declined duplicate BUILD work instead of thrashing scope.
- **Phase-by-phase analysis**:
  - **VAN**: Effective; metadata was valid (`implement`, `deep`, `L4`).
  - **PLAN**: Strong; it identified the contract gap, the managed-runtime cutover path, and the installed-layout/test evidence gates.
  - **CREATIVE**: Strong; `creative-runtime-plugin-control-contract.md` gave BUILD an actionable async/plugin-control contract.
  - **BUILD**: Effective for the approved slice. It delivered the live managed-runtime path and the security fixes, but the task title still overstates total backlog closure.
  - **SECURITY**: Effective; it found a real blocking leak, forced a narrow re-entry, and verified the fix cleanly on attempt 2.

## 3. Architectural Planning Review
- **Architectural principles followed**:
  - The runtime daemon remains the worker/process owner.
  - Kernel additions stayed generic and runtime-facing instead of embedding distro/plugin policy.
  - The single RuntimeConn + callback-sink design was preserved.
  - Reload now fails closed instead of silently reactivating kernel-owned plugin reloads.
- **Were alternatives properly evaluated?** Yes. The creative contract compared callback-sink, event-channel, and split-control-connection designs before selecting the smallest safe option.
- **Were the right architectural decisions made?** Mostly yes: explicit op envelopes, richer capability metadata, worker single-reader routing, managed child supervision, and coarse trust-boundary diagnostics all reduced cutover risk.
- **Architecture validation outcomes**:
  - Runtime-backed tool/hook/catalog paths are now real and exercised.
  - Managed local runtime attach, PID visibility, and teardown are covered in status/testbed smoke.
  - The no-legacy rule is only partially satisfied because compiled stdio plugin ownership paths still exist.

## 4. Creative Phase Review
- The key design work was executed in `memory-bank/creative/creative-runtime-plugin-control-contract.md`.
- Design quality was high: it froze the async op roster, richer capability model, hook reply semantics, readiness states, and supervision boundary without reviving kernel plugin hosting.
- Design-to-implementation fidelity is high for RuntimeConn async sinks, worker op envelopes, hook routing, runtime attach priming, supervision, and runtime smoke.
- Fidelity is incomplete at the final deletion gate and at full release/bootstrap smoke proof.

## 5. Implementation Review
- **Phase completion success**: BUILD completed after one focused security re-entry; SECURITY then passed.
- **Milestone checkpoint effectiveness**:
  - Protocol/control foundation landed first.
  - Runtime async emission, hook routing, and attach priming followed in the right order.
  - Managed local runtime supervision plus runtime status/smoke turned the new path into an executable acceptance gate.
  - Security hardening was isolated cleanly to kernel trust boundaries.
- **Integration challenges**:
  - Init-time async frames could be lost before worker readiness bookkeeping was fixed.
  - Hook reply semantics were initially too implicit.
  - Reload still had to be cut free from kernel-owned plugin fallback.
  - Runtime/plugin-controlled text crossed trust boundaries and required hardening.
- **Performance outcomes**:
  - The single-reader worker router and explicit async frame classes reduce call corruption/drop risk.
  - No benchmark/perf metrics were recorded in Memory Bank for this slice.

## 6. Testing Review
- **Test coverage adequacy**: Strong focused coverage across `internal/runtime/**`, `cmd/tabula-runtime/**`, `internal/kernel/**`, `cmd/tabula/**`, and the isolated testbed runner.
- **Issues found at each stage**:
  - BUILD uncovered init-time async publication loss and fixed current-worker ordering.
  - BUILD replaced a daemon hook-event deny stub with real worker routing.
  - BUILD made hook `reply_mode` explicit to prevent semantic drift.
  - SECURITY attempt 1 found plugin-controlled structured log fields and externally publishable runtime diagnostic text.
  - SECURITY re-entry added regression coverage for logs, lifecycle/catalog diagnostics, and detached-runtime `last_error`.
- **Testing process improvements needed**:
  - Add release/archive/bootstrap installed-layout proof, not only isolated source-built smoke.
  - Capture companion repo evidence alongside core repo results before claiming grouped cutover completion.
  - Keep explicit regression tests around trust-boundary canonicalization/redaction.

### Security Review Integration
- **QA status**: `memory-bank/tasks.md` records `QA: SKIPPED`, and no QA report exists for this task. That is consistent with the executed workflow and should remain explicit at ARCHIVE.
- **SECURITY attempt 1**: `memory-bank/progress.md` records `FAILED` with 1 blocking finding, 1 warning, and 0 degraded checks. The blocking issue was plugin-controlled structured log fields plus externally publishable runtime diagnostic text at the kernel boundary.
- **SECURITY re-entry evidence**: BUILD re-entry updated `internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, and `internal/kernel/snapshot_test.go`; `memory-bank/progress.md` records `go test ./internal/kernel` ✅ for both re-entry passes.
- **SECURITY attempt 2 outcome**: `memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md` records `PASSED` with 0 blocking findings, 1 warning, and `Degraded checks: none`.
- **Remaining warning**: the only unresolved warning is the authenticated internal-wire `LifecycleNotice.Message` crash text noted in `memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md` and `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/findings.md`; current kernel consumers canonicalize that text before status/log publication.

## 7. Successes with Evidence
1. **Runtime-backed plugin control survived the cutover boundary** — Evidence: `internal/runtime/wire/types.go` and `internal/runtime/conn/conn.go` now carry the richer async/plugin-control contract; `cmd/tabula-runtime/pool/pool.go` synthesizes `catalog_update` / `hook_event_reply` / `plugin_send` / `plugin_log` / `lifecycle_notice`; `internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, and `internal/kernel/runtime_hooksub.go` consume those frames into kernel state. `memory-bank/progress.md` records focused validation from the BUILD entries `2026-05-03 — BUILD progress (Agent 4, runtime async emission)` and `2026-05-03 — BUILD progress (Agent 6)` including `go test ./internal/runtime/conn ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./cmd/tabula-runtime/policy/bare ./internal/runtime/... ./internal/kernel/...` ✅ and `go test ./internal/runtime/wire ./internal/runtime/conn ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./internal/kernel/... ./cmd/tabula-runtime/dialer ./internal/runtime/...` ✅.
2. **Managed local runtime became the normal `serve`/`run` path** — Evidence: `cmd/tabula/main.go` and `cmd/tabula/local_runtime.go` start the sibling `tabula-runtime`; `internal/kernel/runtime_attach.go` and `internal/kernel/runtime_registry.go` surface attached-state and PID updates; `tools/tabula-testbed/src/tabula_testbed_runner/runner.py` requires `tabula status --json` to show attached runtime, positive pid, capability evidence, and clean teardown. `memory-bank/progress.md` records `go test ./cmd/tabula ./internal/kernel` ✅ for the supervision cutover and `python3 -m py_compile tools/tabula-testbed/src/tabula_testbed_runner/runner.py` ✅ for the smoke-runner hardening.
3. **The only blocking security issue was eliminated** — Evidence: `internal/kernel/runtime_async.go` now redacts plugin-controlled structured log fields, `internal/kernel/runtime_registry.go` canonicalizes published runtime diagnostics, and `internal/kernel/runtime_async_test.go` plus `internal/kernel/snapshot_test.go` lock in the leak regressions. `memory-bank/progress.md` records both re-entry passes with `go test ./internal/kernel` ✅, and `memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md` records SECURITY attempt 2 as `PASSED` with 0 blocking findings and 0 degraded checks.

## 8. Challenges with Solutions
1. **Challenge: init-time async worker frames could be dropped** → **Solution**: `cmd/tabula-runtime/pool/pool.go` was changed so spawned workers become current before `init_ack`, reload emits explicit `stopping` lifecycle notices, and async publication semantics were tightened; `cmd/tabula-runtime/pool/pool_test.go` covers the init-time frame path. **Outcome**: authoritative catalog/lifecycle events now survive initialization and reload transitions. Evidence: `memory-bank/progress.md` entry `2026-05-03 — BUILD progress (Agent 5)` with `go test ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon` ✅.
2. **Challenge: hook semantics were too implicit across the runtime boundary** → **Solution**: `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/pool/pool.go`, and `cmd/tabula-runtime/daemon/handler.go` added real runtime→worker `HookEvent` routing; `internal/runtime/wire/types.go` and `internal/kernel/runtime_hooksub.go` then made `hook_event.reply_mode` explicit on the Runtime API wire. **Outcome**: hook mutator/blocker/recorder behavior is preserved through the runtime path with less semantic drift risk. Evidence: `memory-bank/progress.md` entries `2026-05-03 — BUILD progress (Agent 5, hook routing)` and `2026-05-03 — BUILD progress (Agent 6)` with `go test ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./internal/runtime/conn ./cmd/tabula-runtime/dialer ./internal/runtime/... ./internal/kernel/...` ✅ and `go test ./internal/runtime/wire ./internal/runtime/conn ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./internal/kernel/... ./cmd/tabula-runtime/dialer ./internal/runtime/...` ✅.
3. **Challenge: untrusted runtime text crossed trust boundaries** → **Solution**: `internal/kernel/runtime_async.go` drops plugin-controlled structured log fields, `internal/kernel/runtime_registry.go` and `internal/kernel/snapshot.go` canonicalize externally published runtime diagnostics, and `internal/kernel/runtime_async_test.go` plus `internal/kernel/snapshot_test.go` add regression coverage for both live and detached runtime paths. **Outcome**: SECURITY attempt 2 passed, with only one non-blocking warning remaining about raw crash text still existing on the authenticated internal runtime wire. Evidence: `memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md` checklist items 4 and 6, `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/findings.md`, and `memory-bank/progress.md` entries `2026-05-04 — BUILD progress (Agent 1)` / `(Agent 2)` with `go test ./internal/kernel` ✅.

## 9. Strategic Technical Insights
- Atomic cutovers need two gates: live-path cutover and compiled legacy-surface deletion. Closing only the live path is not enough under a no-legacy policy.
- Canonicalization at every consumer boundary is an effective short-term defense, but raw worker crash text on the authenticated internal wire should eventually be centrally redacted or permanently isolated from new publishers.
- Installed-layout smoke that asserts real runtime attach, PID visibility, capability evidence, and clean teardown is a durable acceptance pattern for runtime/installer migrations.

## 10. Process Improvement Insights
- Task titles and closure language should match the approved slice; broad backlog labels make archive-readiness ambiguous when significant follow-up scope remains.
- Companion repo SHA/PR/test evidence and release-installer command output should be mandatory inputs before grouped cutover milestones are declared done.
- The targeted security re-entry pattern worked well here: named trust-boundary issue, minimal patch, focused regressions, immediate rerun.

## 11. Business Impact
- **Value delivered**: Tabula now has a materially more production-like local runtime path with runtime-owned plugin control, stronger smoke coverage, and a cleaner security posture around runtime diagnostics.
- **Business metrics affected**:
  - Reduced architecture risk for the runtime cutover.
  - Improved operator confidence in local runtime attach/status/teardown behavior.
  - Improved security confidence for log/status trust boundaries.
  - No claimable end-user completion of the full grouped M3-M6 roadmap yet.

## 12. Strategic Action Items
- **Priority 1**: Finish the no-legacy M2-07 deletion by removing the remaining compiled kernel stdio plugin ownership/runtime surfaces and verifying the supervised-runtime path stays green.
- **Priority 2**: Close the evidence gap with companion M2-04/M2-04b SHA/PR/test capture and true release/bootstrap installed-layout proof.
- **Priority 3**: Split or queue the remaining M3-M6 work as successor tasks so archive records do not overclaim grouped backlog completion.

## 13. Follow-up Tasks
<!-- FOLLOW_UP_START -->
- [ ] Delete the remaining compiled kernel stdio plugin ownership/runtime surfaces and keep the supervised-runtime smoke green | Priority: high | Source: execute-m2-m6-runtime-cutover-backlog
- [ ] Capture M2-04 and M2-04b companion repo SHA/PR/test evidence for the worker-protocol cutover before claiming grouped backlog completion | Priority: high | Source: execute-m2-m6-runtime-cutover-backlog
- [ ] Extend M2-08 proof from isolated source-built smoke to release/bootstrap installed binaries for `tabula` + `tabula-runtime` | Priority: high | Source: execute-m2-m6-runtime-cutover-backlog
- [ ] Decide and document whether `TABULA_SKIP_MCP=1` remains valid for `tabula run` after the runtime cutover | Priority: medium | Source: execute-m2-m6-runtime-cutover-backlog
- [ ] Either keep every consumer on canonicalized lifecycle diagnostics or add an allowlisted redaction policy for `LifecycleNotice.Message` at the runtime layer | Priority: medium | Source: execute-m2-m6-runtime-cutover-backlog
- [ ] Split the remaining M3-M6 backlog into successor tasks and continue execution without overloading one archive record | Priority: high | Source: execute-m2-m6-runtime-cutover-backlog
<!-- FOLLOW_UP_END -->

## Reflection Integration Notes
- SECURITY passed on attempt 2 with 0 blockers, 1 warning, and no degraded checks.
- Remaining warning: worker-controlled crash text still exists in authenticated `LifecycleNotice.Message` on the internal runtime wire, but current kernel consumers canonicalize it before publication.
- Archive readiness: this work is ready for ARCHIVE only as a completed BUILD + SECURITY slice. ARCHIVE should not represent the entire grouped M2-M6 backlog as finished; it must preserve the follow-up items above.
- Structured lesson append/rotation is deferred pending L4 second-opinion approval per CR-B.
