# Progress

Implementation progress is tracked here across all tasks.

### Pipeline Build Log

- Agent 13: FAILED after 3 retries — treated as DECLINE
- Agent 14: FAILED after 3 retries — treated as DECLINE
- Agent 15: FAILED after 3 retries — treated as DECLINE

#### Agent 1 — [CONTRIBUTE]
- Role: Runtime API/plugin-control foundation builder
- Work: Implemented the first M2-07 BUILD slice across the runtime wire and connection layer. Replaced string-only capability metadata with schema-bearing tool/hook/state/source structs, added the async Runtime API plugin-control ops and validation, added a sink-aware `RuntimeConn` with kernel-originated `hook_event` writes plus async frame routing, updated manifest-backed capability generation/snapshots, and refreshed runtime fixture workers/tests to match the op-enveloped worker protocol already expected by the runtime policy.
- Addressed: N/A — first agent
- Files: `internal/runtime/wire/types.go`; `internal/runtime/wire/codec.go`; `internal/runtime/api.go`; `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `internal/runtime/mock/mock.go`; `internal/runtime/wire/types_test.go`; `internal/runtime/contract_test.go`; `internal/runtime/transport/unixsock/unixsock_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `cmd/tabula-runtime/manifest/manifest.go`; `cmd/tabula-runtime/manifest/manifest_test.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `internal/kernel/snapshot.go`; `internal/kernel/runtime_attach_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The runtime attachment path still instantiates `runtimeconn.New(...)` without a Hub-owned async sink, so `catalog_update`/`hook_event_reply`/`plugin_send`/lifecycle notices are not yet connected to kernel state. Worker target management still needs the runtime-side router/manager before BUILD can treat dynamic catalog revisions as authoritative.
- Open issues: Next BUILD agents should wire `RuntimeConn` sinks into the kernel runtime registry/Hub adapter boundary, implement the runtime worker target manager that emits authoritative catalog/hook/lifecycle updates, decide how `plugin_log`/`plugin_send` sink into existing Hub facilities, and then continue into shared local-runtime supervision plus reload/packaging cutover.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress
- Timestamp: 2026-05-03
- Requirement: Start BUILD for the grouped M2-M6 runtime cutover backlog by landing the protocol/runtime foundation required by the accepted plugin-control creative contract.
- Approach: Updated actual runtime wire, manifest, connection, snapshot, and daemon seams first; kept scope on the first two planned BUILD steps (wire contract + RuntimeConn callback sink path); then refreshed integration tests and Python fixture workers to the op-enveloped worker protocol already exercised by the runtime policy.
- Files modified: `internal/runtime/wire/types.go`, `internal/runtime/wire/codec.go`, `internal/runtime/api.go`, `internal/runtime/conn/conn.go`, `internal/runtime/conn/conn_test.go`, `internal/runtime/mock/mock.go`, `internal/runtime/wire/types_test.go`, `internal/runtime/contract_test.go`, `internal/runtime/transport/unixsock/unixsock_test.go`, `cmd/tabula-runtime/daemon/handler.go`, `cmd/tabula-runtime/daemon/handler_test.go`, `cmd/tabula-runtime/manifest/manifest.go`, `cmd/tabula-runtime/manifest/manifest_test.go`, `cmd/tabula-runtime/dialer/dialer_test.go`, `internal/kernel/snapshot.go`, `internal/kernel/runtime_attach_test.go`.
- Testing results: `go test ./internal/runtime/wire ./internal/runtime/conn ./internal/runtime/mock ./internal/runtime/transport/unixsock ./cmd/tabula-runtime/manifest ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./internal/kernel`

### Pipeline Creative Log

#### CREATIVE — [DONE]
- Role: Level 4 creative design for runtime-backed plugin control and local cutover
- Work: Read the PLAN handoff, active/progress context, existing creative contracts, system/tech context, M2-07/M2-08 issue docs, and current runtime/kernel/worker seams. Created `memory-bank/creative/creative-runtime-plugin-control-contract.md` and froze the M2-07/M2-08 design around a single RuntimeConn callback sink, richer capability metadata, explicit worker op-envelope demux, manifest-seeded worker-confirmed readiness, shared `serve`/`run` local runtime supervision, and installed-layout smoke expectations.
- Addressed: Closed PLAN's remaining CREATIVE questions about callback sink vs event channel/control connection, async op roster/severity, worker protocol discriminator, readiness/status model, run-mode coverage, and kernel stdio deletion boundaries.
- Files: `memory-bank/creative/creative-runtime-plugin-control-contract.md`; `memory-bank/tasks.md`; `memory-bank/activeContext.md`; `memory-bank/progress.md`; `memory-bank/systemPatterns.md`
- Risks: BUILD still needs careful async routing and sink-fatality tests to avoid deadlocks or fail-open hook behavior; run-mode MCP behavior and non-unix sibling-binary resolution remain explicit implementation questions.
- Open issues: Resolve `TABULA_SKIP_MCP=1` in `tabula run`; finalize sibling-binary resolution details without reintroducing kernel fallback execution.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: Worker-protocol foundation builder
- Work: Implemented the M2 worker op-envelope foundation in `internal/runtime/worker/wire`, including frame validation, decode routing, richer `init_ack` metadata, and async frame shapes. Updated the bare worker policy and runtime policy interface to use the new protocol and return initial worker metadata for later runtime target management.
- Addressed: N/A — first agent
- Files: `internal/runtime/worker/wire/types.go`; `internal/runtime/worker/wire/types_test.go`; `cmd/tabula-runtime/policy/policy.go`; `cmd/tabula-runtime/policy/bare/bare.go`; `cmd/tabula-runtime/policy/bare/bare_test.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `internal/runtime/contract_test.go`; `internal/runtime/conn/conn_test.go`
- Risks: Runtime still lacks the single-reader target manager that consumes `init_ack`/`tools_updated`/`event_reply`/`send`/`log`; current bare policy fails closed on unexpected async frames beyond logs, so dynamic plugins cannot cut over until that router lands.
- Open issues: Build the runtime-side worker router and target-state manager; plumb returned `init_ack` metadata into runtime capability publication; then wire runtime-backed kernel catalog/hook adapters before deleting kernel plugin stdio ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Runtime target-state integrator
- Work: Added manifest-seeded/worker-confirmed target state to the runtime pool, promoting targets through `manifest_loaded`, `initializing`, `ready`, and `failed`, and publishing worker `init_ack` tool/hook metadata through a new `pool.Capabilities()` view. Updated the daemon to serve that runtime-backed capability view and added pool/daemon tests for ready/failed/reload transitions.
- Addressed: Closed the remaining Agent 1 handoff item to plumb `init_ack` metadata into runtime capability publication and made runtime status reflect the CREATIVE readiness model before async `tools_updated` lands.
- Files: `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`
- Risks: The runtime still lacks a true single-reader worker router, so `tools_updated`, `event_reply`, `send`, and `log` are not yet processed outside the narrow init/call path; dynamic-tool and hook-preserving cutover remains blocked on that router.
- Open issues: Build the target manager/worker router for continuous async frame handling; connect it to Runtime API async emission and runtime-backed kernel catalog/hook adapters; keep `TABULA_SKIP_MCP=1` and sibling-binary resolution for later BUILD passes.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Async worker-router builder
- Work: Refactored the bare worker policy to use a persistent single-reader loop that routes init/results internally and surfaces async worker frames through a new `policy.WorkerAsyncEvent` stream. Added pool-side worker watchers that continuously drain async frames and apply authoritative `tools_updated` revisions to runtime capability state.
- Addressed: Closed Agent 2's open issue to replace synchronous per-call worker reads with an initial continuous router, and extended runtime capability publication beyond startup `init_ack` to post-ready catalog updates.
- Files: `cmd/tabula-runtime/policy/policy.go`; `cmd/tabula-runtime/policy/bare/bare.go`; `cmd/tabula-runtime/policy/bare/bare_test.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`
- Risks: The runtime watcher currently drains `event_reply`, `send`, and `log` but does not yet forward them into Runtime API async emission or kernel-facing sinks; worker exit/lifecycle is only represented as target failure today, not full `lifecycle_notice` diagnostics.
- Open issues: Extend the target manager to publish `catalog_update`/`plugin_send`/`plugin_log`/`hook_event_reply` and lifecycle notices over Runtime API; wire those async sink callbacks into kernel Hub adapters; defer `TABULA_SKIP_MCP=1` and sibling-binary decisions to later BUILD passes.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Kernel runtime-sink / runtime-dispatch integrator
- Work: Wired authenticated runtime attachments through a Hub-owned async sink, added runtime-backed tool dispatch and hook subscribers keyed by runtime target state, routed `hook_event_reply`/`plugin_send`/`plugin_log`/`lifecycle_notice` into existing kernel facilities, and expanded runtime snapshots with per-target hook/state/source/revision/lifecycle diagnostics. Added focused kernel tests covering async sink routing, runtime-backed dispatch invocation, and detach cleanup.
- Addressed: Closed Agent 3's remaining kernel-side handoff by connecting RuntimeConn async frames to the runtime registry, hook engine, bus fan-out, and runtime-target tool table instead of dropping them after attach.
- Files: `internal/kernel/runtime_async.go`; `internal/kernel/runtime_registry.go`; `internal/kernel/runtime_attach.go`; `internal/kernel/runtime_hooksub.go`; `internal/kernel/helpers.go`; `internal/kernel/tool_dispatch.go`; `internal/kernel/tool_service.go`; `internal/kernel/snapshot.go`; `internal/kernel/runtime_attach_test.go`; `internal/kernel/tool_dispatch_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Runtime-backed dispatch now depends on authoritative runtime async emission that the runtime target manager still does not publish for `event_reply`/`send`/`log`/lifecycle in production; runtime tool shadowing still follows the existing overwrite-only tool table semantics; local supervisor/reload/bootstrap packaging and stdio deletion remain later BUILD work.
- Open issues: Finish the runtime target manager so pool watcher outputs become real Runtime API async frames, then drive those frames through the new kernel sink path; after that continue local supervisor/reload/bootstrap packaging and kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Runtime async-emission bridge builder
- Work: Added a server-side async frame bridge in `internal/runtime/conn` so handlers can emit runtime-originated plugin-control frames on the same authenticated connection. Extended the runtime pool to synthesize `catalog_update`, `hook_event_reply`, `plugin_send`, `plugin_log`, and `lifecycle_notice` from worker-router events and target-state transitions, and delegated those frames through the daemon handler.
- Addressed: Closed the runtime-side half of the prior Agent 4 handoff by turning pool watcher outputs into actual Runtime API async frames that can flow into the already-landed kernel async sink/dispatch path.
- Files: `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler.go`
- Risks: `Handler.HookEvent` still denies instead of routing kernel hook events to workers; dynamic targets are still lazily initialized, so initial authoritative catalog/hook state may not be emitted before first invoke; lifecycle still reports start/ready/crash but not full stop/exit semantics.
- Open issues: Implement real `HookEvent` routing with `WorkerEvent`/`WorkerEventReply`; decide whether attach/reload should proactively initialize dynamic targets; continue local supervisor/reload/bootstrap packaging and kernel stdio deletion; later resolve `TABULA_SKIP_MCP=1` and sibling-binary behavior.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Runtime pool emission hardener
- Work: Re-read the actual pool/daemon state and found the remaining reliability hole in the new async bridge: `watchWorker` could still ignore init-time frames because spawned workers were not marked current until after `init_ack`. Fixed that ordering, retired workers immediately on async crash/error paths, emitted `stopping` lifecycle notices during reload, and tightened publication semantics so critical async frames remain synchronous while plugin logs stay best-effort. Added focused pool tests for init-time async frames and reload stopping notices.
- Addressed: Closed the stale part of Agent 4's runtime-side handoff where the async bridge was present but still lossy during worker initialization and reload transitions.
- Files: `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: `Handler.HookEvent` still denies instead of routing to workers, so runtime-backed hook subscribers remain functionally blocked; dynamic target warm-init policy is still unresolved and affects whether initial authoritative catalog state should exist before first invoke.
- Open issues: Route daemon `HookEvent` requests through `policy.Worker.HookEvent`; decide whether attach/reload should proactively initialize dynamic targets; then continue local supervisor/reload/bootstrap packaging and kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress (Agent 1)
- Timestamp: 2026-05-03
- Requirement: Start BUILD by landing the worker-protocol foundation required by the CREATIVE plugin-control contract.
- Approach: Re-read the active handoff plus the worker/bare-policy/pool seams, then converted the worker wire format to an explicit `op` envelope with validation and async frame types, updated the policy interface to return initial worker metadata, and adapted the bare policy/tests and focused runtime tests to the new protocol.
- Files modified: `internal/runtime/worker/wire/types.go`, `internal/runtime/worker/wire/types_test.go`, `cmd/tabula-runtime/policy/policy.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/policy/bare/bare_test.go`, `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `internal/runtime/contract_test.go`, `internal/runtime/conn/conn_test.go`.
- Testing results: `go test ./internal/runtime/... ./cmd/tabula-runtime/policy/... ./cmd/tabula-runtime/pool` ✅

### 2026-05-03 — BUILD progress (Agent 2)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by wiring initial worker metadata into runtime capability publication and target readiness state.
- Approach: Started from the BUILD handoff and actual pool/daemon/manifest seams, then introduced a pool-owned target capability map seeded from manifests and updated by worker init results. Switched daemon `ListCapabilities` to the runtime-backed view and added focused tests for ready/failed/reload state transitions.
- Files modified: `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `cmd/tabula-runtime/daemon/handler.go`, `cmd/tabula-runtime/daemon/handler_test.go`.
- Testing results: `go test ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./cmd/tabula-runtime/policy/bare ./internal/runtime/...` ✅

### 2026-05-03 — BUILD progress (Agent 3)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by replacing synchronous worker reads with the first persistent async worker router and applying post-ready catalog updates.
- Approach: Started from the BUILD handoff plus actual bare-policy/pool seams, then converted the bare worker to a single-reader router that owns init/result delivery and emits async worker events. Added pool-side watcher goroutines to consume those events continuously and update runtime capability state from `tools_updated`, with focused tests for async-frame tolerance and capability upgrades.
- Files modified: `cmd/tabula-runtime/policy/policy.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/policy/bare/bare_test.go`, `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`.
- Testing results: `go test ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./internal/runtime/...` ✅

### 2026-05-03 — BUILD progress (Agent 4)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by landing the kernel-side RuntimeConn sink/registry adapters needed once runtime async frames start flowing.
- Approach: Started from the active handoff plus actual runtime-attach/registry/hook/tool-dispatch/snapshot seams, then built a Hub-owned async sink that applies runtime catalog/lifecycle updates to kernel state, exposes ready targets as runtime-backed tool dispatch + hook subscribers, and reuses the existing bus/hook/log facilities instead of inventing a second kernel transport.
- Files modified: `internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/runtime_attach.go`, `internal/kernel/runtime_hooksub.go`, `internal/kernel/helpers.go`, `internal/kernel/tool_dispatch.go`, `internal/kernel/tool_service.go`, `internal/kernel/snapshot.go`, `internal/kernel/runtime_attach_test.go`, `internal/kernel/tool_dispatch_test.go`.
- Testing results: `go test ./internal/kernel -run 'TestServeAuthenticatedRuntime(CatalogUpdatePopulatesRuntimeDispatchAndSnapshot|AsyncFramesRouteHookRepliesAndBusMessages|DetachRemovesRuntimeTools)|TestHandleDynamicTool_RuntimeSourceInvokesAttachedRuntime'` ✅; `go test ./internal/kernel` ✅

### 2026-05-03 — BUILD progress (Agent 4, runtime async emission)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by turning routed worker async events into real Runtime API async frames that reach the existing kernel sink path.
- Approach: Re-read the current BUILD handoff plus the actual runtime connection, pool, daemon, and kernel async-sink files. Added an optional async-frame source to the server-side Runtime API serve loop, made the runtime pool synthesize async Runtime API frames from worker-router events and target-state transitions, and verified the kernel consumer side was already in place.
- Files modified: `internal/runtime/conn/conn.go`, `internal/runtime/conn/conn_test.go`, `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `cmd/tabula-runtime/daemon/handler.go`.
- Testing results: `go test ./internal/runtime/conn ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./cmd/tabula-runtime/policy/bare ./internal/runtime/... ./internal/kernel/...` ✅

### 2026-05-03 — BUILD progress (Agent 5)
- Timestamp: 2026-05-03
- Requirement: Harden the runtime-side async bridge so authoritative worker frames survive initialization and reload transitions.
- Approach: Started from the latest BUILD handoff and actual pool/policy/daemon code, verified the new async bridge existed, then fixed the remaining initialization race, added reload `stopping` lifecycle publication, and separated critical-vs-best-effort async frame delivery.
- Files modified: `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`.
- Testing results: `go test ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon` ✅

#### Agent 5 — [CONTRIBUTE]
- Role: Hook-event routing implementer
- Work: Implemented real runtime→worker hook routing across the runtime stack. Added `policy.Worker.HookEvent`, taught the bare worker router to correlate `event_reply`, wired `pool.HookEvent(...)` to translate Runtime API hook events into worker protocol events, and replaced the daemon deny stub with live worker routing.
- Addressed: Closed the primary open issue from the handoff by making kernel `HookEvent` delivery reach workers and return `WorkerEventReply` values through the runtime path.
- Files: `cmd/tabula-runtime/policy/policy.go`; `cmd/tabula-runtime/policy/bare/bare.go`; `cmd/tabula-runtime/policy/bare/bare_test.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `internal/runtime/conn/conn.go`
- Risks: Reply-mode semantics are still derived from hook event names inside the runtime pool instead of being explicit in the Runtime API wire contract, so future hook additions could drift if the mappings diverge. Dynamic targets are still lazily initialized, and lifecycle still lacks explicit stopping/exited emission outside reload/crash paths.
- Open issues: Decide whether to add explicit `reply_mode` to Runtime API `hook_event`; decide whether attach/reload should proactively initialize dynamic targets and emit initial authoritative catalog state; continue local supervisor/reload/bootstrap packaging and kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Runtime attach-priming / reload-dedup finisher
- Work: Verified the stale handoff against the real daemon/pool/conn code, then finished the remaining runtime-side gap by proactively priming manifest-backed targets on every authenticated attach and after reload so kernels receive authoritative catalog/hook state before first invoke and on reconnect. Also deduplicated reload evictions/lifecycle notices across multiple tenant workers and restored default hook reply-mode mapping for direct/internal callers while preserving request contexts.
- Addressed: Closed Agent 5's open issue around proactive attach/reload initialization, confirmed daemon `HookEvent` routing is already live, and fixed the reconnect/reload regression where ready runtime targets could stay invisible until a later mutation.
- Files: `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `internal/runtime/conn/conn.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Proactive priming currently uses a default-tenant warm worker per target, which can increase worker count until tenant-aware/dynamic-only catalog discovery is refined; the remaining M2 critical path is still local-runtime supervision/bootstrap/release packaging and kernel stdio deletion.
- Open issues: Implement shared local-runtime startup/config/reload orchestration for `serve` and `run`; ship/install `tabula-runtime` alongside `tabula` with bootstrap/testbed smoke; later decide whether default-tenant priming should narrow to explicitly dynamic targets.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Hook-event contract hardener
- Work: Made `hook_event.reply_mode` explicit in the Runtime API wire contract, propagated it through kernel runtime hook subscribers and runtime send paths, and removed the runtime-pool fallback that inferred reply semantics from hook names.
- Addressed: Closed Agent 5's remaining contract ambiguity by encoding reply expectations directly on the wire instead of deriving them locally inside the runtime.
- Files: `internal/runtime/wire/types.go`; `internal/runtime/wire/codec.go`; `internal/runtime/wire/types_test.go`; `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `internal/runtime/contract_test.go`; `internal/kernel/runtime_hooksub.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler_test.go`
- Risks: The remaining BUILD work is now mostly operational: deciding whether proactive attach/reload priming should stay broad or narrow to dynamic targets, plus local supervisor/bootstrap/release packaging and final kernel stdio deletion.
- Open issues: Refine or confirm the attach/reload priming policy; then continue local supervisor/reload/bootstrap packaging, installed-layout smoke coverage, and kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 7 — [CONTRIBUTE]
- Role: Local runtime config handoff builder
- Work: Added a canonical runtime config writer (`cmd/tabula-runtime/config.Save`), wired `tabula serve` to materialize `$TABULA_HOME/config/runtime.toml`, and added `tabula-runtime --version` plus dev-install runtime shutdown support so runtime packaging/install flow can hand off cleanly to the upcoming managed child.
- Addressed: Closed part of Agent 6's operational handoff by creating the concrete `runtime.toml` handoff path needed before a shared local-runtime supervisor can launch `tabula-runtime` for `serve` or `run`.
- Files: `cmd/tabula/main.go`; `cmd/tabula/main_test.go`; `cmd/tabula-runtime/main.go`; `cmd/tabula-runtime/main_test.go`; `cmd/tabula-runtime/config/config.go`; `cmd/tabula-runtime/config/config_test.go`; `scripts/install-dev.sh`
- Risks: `tabula serve` now writes runtime config, but it still does not supervise a local `tabula-runtime` child; reload-trigger behavior still calls `hub.ReloadPlugins`; installed-layout bootstrap/release cutover and kernel stdio deletion remain ahead.
- Open issues: Launch a shared managed local runtime for `serve`/`run` using the generated config; replace reload-trigger kernel plugin reload with Runtime API reload; finish bootstrap/release plumbing and final kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Local runtime config fidelity / launch-prep hardener
- Work: Moved runtime config generation into a dedicated helper, expanded it so `tabula run` and reload-trigger boot refreshes also rewrite `$TABULA_HOME/config/runtime.toml`, switched `plugin_dirs` from a blanket `$TABULA_HOME/plugins` assumption to deduped absolute boot manifest paths, and added a tested sibling-binary resolver helper for the upcoming managed-child supervisor.
- Addressed: Closed the remaining fidelity gap in Agent 7's handoff by making the generated runtime config follow the real boot plugin selection across both entrypoints and reloads, while also preparing the launch primitive the later supervisor slice needs.
- Files: `cmd/tabula/main.go`; `cmd/tabula/main_test.go`; `cmd/tabula/local_runtime.go`; `cmd/tabula/local_runtime_test.go`
- Risks: The managed-child supervisor still is not wired, the new sibling-binary resolver helper is not used by process launch yet, and reload still ends at `hub.ReloadPlugins` instead of Runtime API reload.
- Open issues: Use the new config/resolver helpers in a shared `serve`/`run` local-runtime supervisor; replace reload-trigger kernel plugin reload with runtime reload; add bootstrap/runtime smoke on the dual-binary install surface; then delete kernel stdio plugin ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Packaging / install surface builder
- Work: Landed the first M2-08 packaging slice by teaching GoReleaser, the Makefile, both dev installers, both release installers, and the source-built testbed runner to stage `tabula-runtime` alongside `tabula` instead of assuming a single-binary world.
- Addressed: Closed most of Agent 4's packaging handoff by making dual-binary release/dev installs real and by preparing the testbed `bin/` layout for upcoming bootstrap/runtime-smoke coverage.
- Files: `.goreleaser.yaml`; `Makefile`; `scripts/install-dev.sh`; `scripts/install.sh`; `scripts/install.ps1`; `scripts/install-dev.ps1`; `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Bootstrap/runtime smoke is still not wired, so the new sibling-binary surfaces are packaged but not yet exercised end-to-end; local-runtime managed-child startup/reload remains the main blocker before these paths prove runtime behavior.
- Open issues: Implement shared local-runtime startup/config/reload orchestration for `serve` and `run`; add bootstrap/runtime smoke using the new dual-binary layout; then delete kernel stdio plugin ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: Managed runtime launch scaffold builder
- Work: Added reusable local-runtime command/supervisor scaffolding in `cmd/tabula/local_runtime.go`, including sibling-binary resolution reuse, canonical `tabula-runtime start --config ...` command construction, `TABULA_HOME` propagation, and timeout-based shutdown behavior. Added focused tests for command/env handoff.
- Addressed: Closed another piece of Agent 7's/Agent 6's handoff by defining the concrete launch/stop scaffold the shared `serve`/`run` local-runtime supervisor can call instead of re-solving process semantics later.
- Files: `cmd/tabula/local_runtime.go`; `cmd/tabula/local_runtime_test.go`
- Risks: The scaffold is not yet wired into `serve` or `run`, so kernel plugin ownership/reload behavior is unchanged; supervisor integration still has to account for duplicate plugin ownership until stdio deletion lands.
- Open issues: Integrate the new managed runtime launch/shutdown scaffold into shared `serve`/`run` supervision; replace reload-trigger plugin reload with Runtime API reload; then continue bootstrap/runtime smoke coverage and final kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 3/5, Coherence 5/5, Applicability 4/5, Mission 4/5

#### Agent 7 — [CONTRIBUTE]
- Role: Runtime disconnect / restart-semantics hardener
- Work: Verified the managed-runtime handoff against the actual dialer loop and fixed the reconnect guard so `Reconnect: false` now stops the default unix-socket dial path after disconnect instead of quietly retrying forever. Added a regression test that exercises the real listener/handshake flow and proves the runtime exits promptly after a post-handshake kernel disconnect when reconnect is disabled.
- Addressed: Closed the lingering supervisor-readiness risk that a managed `tabula-runtime` child would outlive kernel/socket teardown because the dialer treated non-reconnect mode as test-only behavior.
- Files: `cmd/tabula-runtime/dialer/dialer.go`; `cmd/tabula-runtime/dialer/dialer_test.go`
- Risks: Shared local-runtime supervision for `serve`/`run` is still absent, so the new dialer behavior is only a prerequisite until command-level child startup/reload wiring lands; reload still ends at `hub.ReloadPlugins`; bootstrap/runtime smoke and kernel stdio deletion remain ahead.
- Open issues: Wire the shared managed-child supervisor into `serve`/`run` using the generated config plus sibling-binary resolver; replace reload-trigger kernel plugin reload with runtime reload; add installed-layout bootstrap/runtime smoke; then delete kernel stdio plugin ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: Managed runtime readiness/lifecycle scaffold hardener
- Work: Strengthened `cmd/tabula`'s reusable managed-local-runtime scaffold so it now owns a single background `Wait()` lifecycle, exposes child PID plus done/wait helpers, and provides an attachment wait gate that fails fast if the runtime exits before connecting. Added helper-process tests that cover successful attachment waiting, graceful shutdown, and early-exit detection.
- Addressed: Closed the scaffold-quality gap left by the earlier launch-helper slice, so the next supervisor integration pass can gate readiness and observe child exit without double-`Wait()` races or ad hoc PID plumbing.
- Files: `cmd/tabula/local_runtime.go`; `cmd/tabula/local_runtime_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The scaffold still is not wired into `serve` or `run`, so runtime reload/boot behavior is unchanged until a later BUILD slice integrates it; helper-process tests currently exercise command lifecycle rather than real runtime attachment.
- Open issues: Wire the hardened scaffold into shared `serve`/`run` local-runtime supervision; replace reload-trigger kernel plugin reload with Runtime API reload; add installed-layout bootstrap/runtime smoke; then delete kernel stdio plugin ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 4/5

### 2026-05-03 — BUILD progress (Agent 5, hook routing)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by replacing the daemon `HookEvent` deny stub with real runtime→worker event routing.
- Approach: Started from the active BUILD handoff plus the actual runtime hook subscriber, daemon, pool, and bare worker router code. Added a synchronous worker hook-event path with `event_reply` correlation, routed Runtime API hook events through the pool to workers, and adjusted server-side Runtime API handling to allow fire-and-forget hooks to omit a reply frame.
- Files modified: `cmd/tabula-runtime/policy/policy.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/policy/bare/bare_test.go`, `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `cmd/tabula-runtime/daemon/handler.go`, `cmd/tabula-runtime/daemon/handler_test.go`, `internal/runtime/conn/conn.go`.
- Testing results: `go test ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./internal/runtime/conn ./cmd/tabula-runtime/dialer ./internal/runtime/... ./internal/kernel/...` ✅

### 2026-05-03 — BUILD progress (Agent 4, attach priming)
- Timestamp: 2026-05-03
- Requirement: Close the remaining runtime-side BUILD gap by making attached kernels see authoritative runtime target state before first invoke and across reconnect/reload.
- Approach: Re-read the actual pool/daemon/conn/attach seams and found the real gap was not hook routing anymore but catalog publication timing: ready targets only emitted async state on transitions. Added proactive attach/reload priming plus ready-state republish for already-live workers, deduplicated reload target/lifecycle reporting across per-tenant workers, and restored default hook reply-mode mapping for direct/internal `SendHookEvent` callers.
- Files modified: `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `cmd/tabula-runtime/daemon/handler.go`, `cmd/tabula-runtime/daemon/handler_test.go`, `cmd/tabula-runtime/dialer/dialer_test.go`, `internal/runtime/conn/conn.go`.
- Testing results: `go test ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./cmd/tabula-runtime/dialer ./internal/runtime/conn` ✅

### 2026-05-03 — BUILD progress (Agent 6)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by eliminating locally inferred hook reply semantics and making `hook_event.reply_mode` explicit in the Runtime API contract.
- Approach: Re-read the active handoff plus the actual runtime wire, hook-subscriber, conn, and pool files. Added explicit `reply_mode` to Runtime API `hook_event`, propagated it through kernel/runtime send paths, removed the pool-side reply-mode fallback, and updated focused hook-routing tests.
- Files modified: `internal/runtime/wire/types.go`, `internal/runtime/wire/codec.go`, `internal/runtime/wire/types_test.go`, `internal/runtime/conn/conn.go`, `internal/runtime/conn/conn_test.go`, `internal/runtime/contract_test.go`, `internal/kernel/runtime_hooksub.go`, `cmd/tabula-runtime/pool/pool.go`, `cmd/tabula-runtime/pool/pool_test.go`, `cmd/tabula-runtime/daemon/handler_test.go`.
- Testing results: `go test ./internal/runtime/wire ./internal/runtime/conn ./cmd/tabula-runtime/policy/bare ./cmd/tabula-runtime/pool ./cmd/tabula-runtime/daemon ./internal/kernel/... ./cmd/tabula-runtime/dialer ./internal/runtime/...` ✅

### 2026-05-03 — BUILD progress (Agent 7)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by creating the concrete runtime config/bootstrap handoff required before `serve`/`run` can supervise a shared local runtime daemon.
- Approach: Started from the current BUILD handoff and actual `cmd/tabula`, `cmd/tabula-runtime/config`, release-install, and runtime main files. Added a reusable runtime config writer, had `tabula serve` emit the default local `runtime.toml`, and aligned the daemon binary with release/install expectations via `--version` support and dev-install runtime shutdown handling.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `cmd/tabula-runtime/main.go`, `cmd/tabula-runtime/main_test.go`, `cmd/tabula-runtime/config/config.go`, `cmd/tabula-runtime/config/config_test.go`, `scripts/install-dev.sh`.
- Testing results: `bash -n scripts/install-dev.sh && go test ./cmd/tabula ./cmd/tabula-runtime ./cmd/tabula-runtime/config` ✅

### 2026-05-03 — BUILD progress (Agent 6, runtime config fidelity)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by making the generated local runtime config reflect the active boot plugin set for `serve`, `run`, and reload-trigger boot refreshes, and prepare the upcoming managed-child launch primitive.
- Approach: Re-read the active BUILD handoff plus the actual `cmd/tabula` and runtime-config seams, then split runtime config generation into a dedicated helper, expanded the write path to `run` and reload, replaced the blanket plugin-dir assumption with deduped boot manifest paths, and added focused tests plus a sibling-binary resolver helper for the next supervisor slice.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `cmd/tabula/local_runtime.go`, `cmd/tabula/local_runtime_test.go`.
- Testing results: `go test ./cmd/tabula ./cmd/tabula-runtime/config` ✅

### 2026-05-03 — BUILD progress (Agent 8)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by turning the new runtime.toml handoff into a reusable launch/stop scaffold for the upcoming shared local-runtime supervisor.
- Approach: Re-read the active BUILD handoff and the current `cmd/tabula` local-runtime helper files, then added canonical child command construction around the sibling `tabula-runtime` binary plus a small managed shutdown wrapper so the next pass can focus on supervisor wiring instead of process invocation mechanics.
- Files modified: `cmd/tabula/local_runtime.go`, `cmd/tabula/local_runtime_test.go`.
- Testing results: `go test ./cmd/tabula ./cmd/tabula-runtime ./cmd/tabula-runtime/config` ✅

### 2026-05-03 — BUILD progress (Agent 7, dialer disconnect semantics)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by aligning the local runtime dialer with the planned supervisor-owned restart policy so managed children do not self-retry forever after kernel/socket teardown.
- Approach: Re-read the active BUILD handoff plus the actual `cmd/tabula-runtime/dialer` control flow, traced the disconnect path through `Run → runOnce → serveOnce`, and fixed the reconnect guard that was accidentally limited to injected test dialers. Added a unix-socket regression test that exercises the default transport and asserts prompt exit after a post-handshake disconnect when reconnect is disabled.
- Files modified: `cmd/tabula-runtime/dialer/dialer.go`, `cmd/tabula-runtime/dialer/dialer_test.go`.
- Testing results: `go test ./cmd/tabula-runtime/dialer` ✅; `go test ./cmd/tabula-runtime/...` ✅

### 2026-05-03 — BUILD progress (Agent 8, managed runtime readiness scaffold)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by hardening the reusable managed-local-runtime scaffold before it is wired into shared `serve`/`run` supervision.
- Approach: Re-read the active BUILD handoff plus the actual `cmd/tabula/local_runtime` helper code, then replaced the ad hoc shutdown-only wait path with a single owned background wait lifecycle. Added PID/done/wait helpers and an attachment wait gate that reports early child exit instead of silently timing out.
- Files modified: `cmd/tabula/local_runtime.go`, `cmd/tabula/local_runtime_test.go`.
- Testing results: `go test ./cmd/tabula` ✅

#### Agent 9 — [CONTRIBUTE]
- Role: Runtime reload-path integrator
- Work: Added a generic kernel-side `ReloadAttachedRuntime(...)` bridge and updated the serve-side reload trigger to prefer Runtime API reloads after boot/runtime.toml refreshes, falling back to `hub.ReloadPlugins(...)` only when no runtime daemon is attached yet.
- Addressed: Closed the reload half of Agent 8/7's handoff by making the new runtime reload path reachable from the live kernel before managed-child supervision is fully wired.
- Files: `cmd/tabula/main.go`; `internal/kernel/runtime_async.go`; `internal/kernel/runtime_attach_test.go`; `internal/runtime/mock/mock.go`
- Risks: Managed local-runtime supervision is still not wired into `serve` or `run`, so reload still falls back to kernel plugin reloads in the common no-runtime-attached case; bootstrap/runtime smoke and final stdio plugin deletion remain ahead.
- Open issues: Integrate the managed local-runtime supervisor into `serve`/`run` so Runtime API reload becomes the normal path; add bootstrap/runtime smoke on the dual-binary install surface; then delete kernel stdio plugin ownership.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 10 — [CONTRIBUTE]
- Role: Installed-layout smoke hardener
- Work: Extended the isolated testbed runner with explicit runtime-sidecar smoke checks: it now verifies the sibling `tabula-runtime` binary exists and runs with `--version`, and validates that the generated `runtime.toml` exists with the expected kernel/token/socket/plugin-dir shape before the smoke suite proceeds.
- Addressed: Closed part of Agent 9's remaining bootstrap/runtime-smoke handoff by turning dual-binary packaging assumptions into an actual installed-layout runtime-sidecar guardrail.
- Files: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`
- Risks: This only verifies packaging/layout plus generated config shape; it does not yet prove a supervised local runtime child actually launches and attaches during smoke, because `serve`/`run` supervision is still not wired.
- Open issues: Integrate the managed local-runtime supervisor into `serve`/`run`; then let the existing testbed runner exercise the real supervised path and continue to final kernel stdio deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

### 2026-05-03 — BUILD progress (Agent 9)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by switching the live reload trigger toward Runtime API reloads before the managed local-runtime supervisor is fully wired.
- Approach: Re-read the active BUILD handoff plus the actual reload-trigger and runtime-registry seams, then added a generic kernel reload bridge for attached runtimes and changed the reload watcher to prefer Runtime API reloads after boot/runtime.toml refreshes, only falling back to kernel plugin reloads when no runtime daemon is attached.
- Files modified: `cmd/tabula/main.go`, `internal/kernel/runtime_async.go`, `internal/kernel/runtime_attach_test.go`, `internal/runtime/mock/mock.go`.
- Testing results: `go test ./cmd/tabula ./internal/kernel ./internal/runtime/mock` ✅

### 2026-05-03 — BUILD progress (Agent 10)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by adding bootstrap/runtime smoke coverage for the new dual-binary sidecar layout before supervisor wiring lands.
- Approach: Re-read the active BUILD handoff plus the isolated testbed runner that stages `tabula` and `tabula-runtime` into a temporary TABULA_HOME. Added explicit verification that the sibling `tabula-runtime` binary exists/runs and that the generated `runtime.toml` has the expected kernel/token/socket/plugin-dir shape before smoke tests execute.
- Files modified: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`.
- Testing results: `python3 -m py_compile tools/tabula-testbed/src/tabula_testbed_runner/runner.py` ✅

#### Agent 9 — [CONTRIBUTE]
- Role: Shared local-runtime supervision integrator
- Work: Wired the managed sibling `tabula-runtime` child into both `tabula serve` and `tabula run`. Each path now issues the local runtime token, opens the unix runtime listener, starts the managed child, waits for the runtime attachment gate, and skips kernel-owned plugin loading on the happy path. Also extended runtime attach/registry helpers so the kernel can report attached-state and update the managed runtime PID after attach.
- Addressed: Closed the main open issue from Agents 8-10 by turning the launch/readiness scaffold into actual shared `serve`/`run` supervision while preserving the already-landed runtime-preferred reload path.
- Files: `cmd/tabula/main.go`; `cmd/tabula/local_runtime.go`; `internal/kernel/runtime_attach.go`; `internal/kernel/runtime_registry.go`; `internal/kernel/runtime_attach_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Installed-layout smoke still needs to prove the supervised sibling-binary path end-to-end; reload still keeps a no-runtime-attached fallback to `hub.ReloadPlugins(...)` until kernel stdio deletion removes the legacy path entirely; `TABULA_SKIP_MCP=1` remains unresolved for later run-mode decisions.
- Open issues: Add bootstrap/testbed smoke that exercises installed `tabula` + installed `tabula-runtime`; delete kernel stdio plugin ownership and the remaining reload fallback; then revisit run-mode MCP policy if required before M3.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress (Agent 9, shared runtime supervision)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by converting the local-runtime launch/readiness scaffold into real shared supervision for `tabula serve` and `tabula run`.
- Approach: Re-read the active BUILD handoff plus the actual `cmd/tabula` and kernel runtime-attach/registry seams, then replaced the old `hub.LoadPlugins(...)` happy path with managed child startup on both entrypoints. Added attachment-state helpers and PID update support in the kernel read model so the supervised child can be observed after attach.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/local_runtime.go`, `internal/kernel/runtime_attach.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/runtime_attach_test.go`.
- Testing results: `go test ./cmd/tabula ./internal/kernel` ✅

#### Agent 11 — [CONTRIBUTE]
- Role: Supervised runtime smoke upgrader
- Work: Strengthened the isolated testbed runner so it now waits for `tabula status --json` to report an attached local runtime with a positive pid after kernel startup, not just a present sidecar binary/config file.
- Addressed: Closed Agent 10's remaining smoke gap by making the installed-layout guardrail exercise the actual supervised runtime-child attachment path.
- Files: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`
- Risks: The smoke still proves attachment/readiness at the status level, not every post-attach capability mutation path; kernel stdio plugin ownership/deletion and any remaining serve/run supervision edge cases are still ahead.
- Open issues: Finish any remaining serve/run supervision edge cases if needed, then rely on the strengthened status-based smoke while deleting kernel stdio plugin ownership and the last legacy reload/plugin paths.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress (Agent 11)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by turning the runtime-sidecar layout smoke into a real supervised-runtime attachment check.
- Approach: Re-read the active BUILD handoff plus the current isolated testbed runner and status CLI behavior. Added a lightweight poll of `tabula status --json` so the runner now waits for the managed local runtime child to appear as attached with a real pid before smoke tests proceed.
- Files modified: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`.
- Testing results: `python3 -m py_compile tools/tabula-testbed/src/tabula_testbed_runner/runner.py` ✅

#### Agent 10 — [CONTRIBUTE]
- Role: Runtime smoke teardown hardener
- Work: Re-read the latest smoke/supervision handoff and tightened the isolated testbed runner further. It now requires `tabula status --json` to show the attached `local` runtime with non-empty capability evidence, records the managed runtime PID from status, and fails teardown if kernel shutdown leaves the `tabula-runtime` child alive or the unix runtime socket behind.
- Addressed: Closed the remaining quality gap after Agent 11 by turning runtime capability visibility and sidecar cleanup into executable M2-08 guardrails instead of attachment-only checks.
- Files: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The smoke still uses the source-built isolated testbed layout rather than a release archive/bootstrap installer flow; kernel stdio plugin ownership deletion and the no-runtime-attached reload fallback remain the core code-path work ahead.
- Open issues: Delete kernel stdio plugin ownership and the remaining reload fallback; keep the stronger runtime smoke green while doing so; revisit `TABULA_SKIP_MCP=1` only if run-mode MCP policy still needs an explicit M2 decision.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress (Agent 10, runtime smoke teardown)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by extending supervised-runtime smoke from attachment-only validation to capability visibility and clean teardown checks.
- Approach: Re-read the active BUILD handoff plus the isolated testbed runner, `tabula status` runtime fields, shared local-runtime supervision path, and unix socket cleanup behavior. Tightened the runner to require capability evidence on the attached runtime and to fail if kernel shutdown leaves the managed child or socket behind.
- Files modified: `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`.
- Testing results: `python3 -m py_compile tools/tabula-testbed/src/tabula_testbed_runner/runner.py` ✅

#### Agent 12 — [CONTRIBUTE]
- Role: Live reload fallback remover
- Work: Removed the serve-side reload trigger's fallback to `hub.ReloadPlugins(...)` and replaced it with a small `reloadLocalRuntime(...)` helper that now requires an attached local runtime. Added focused tests covering the attached-runtime and not-attached cases.
- Addressed: Closed the active handoff item to remove the remaining live no-runtime-attached/plugin-reload fallback surface from the serve path.
- Files: `cmd/tabula/main.go`; `cmd/tabula/main_test.go`
- Risks: Kernel stdio plugin ownership/runtime code still exists in the repo for legacy coverage and explicit deletion remains ahead; if the supervised runtime ever fails to attach, reloads now fail loudly instead of silently reactivating kernel-owned plugin reloads.
- Open issues: Delete the remaining kernel stdio plugin ownership/runtime code paths and keep the strengthened supervised-runtime smoke green while doing so; revisit `TABULA_SKIP_MCP=1` only if run-mode MCP behavior still needs an explicit pre-M3 decision.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-03 — BUILD progress (Agent 12)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by deleting the remaining live reload fallback that could reactivate kernel-owned plugin reloads.
- Approach: Re-read the active BUILD handoff plus the actual `watchReloadTrigger` and shared managed-runtime supervision code. Replaced the old attached-runtime-or-kernel-fallback branch with an explicit `reloadLocalRuntime(...)` helper and added focused command tests for attached vs missing-runtime behavior.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/main_test.go`.
- Testing results: `go test ./cmd/tabula` ✅

#### Agent 11 — [CONTRIBUTE]
- Role: BUILD handoff coherence auditor
- Work: Re-read the latest handoff against the actual `cmd/tabula/main.go` serve/reload path and confirmed the no-runtime-attached `hub.ReloadPlugins(...)` fallback is already gone. Updated the active BUILD scope/handoff to reflect that the remaining core work is kernel stdio plugin ownership deletion, and fixed the stale reload comment in `cmd/tabula/main.go` so the code/documentation surface matches the real cutover state.
- Addressed: Closed the stale open issue from the previous handoff that still described a live reload fallback; later agents can focus directly on removing the remaining compiled stdio plugin runtime/dispatch/snapshot surfaces.
- Files: `cmd/tabula/main.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The kernel still compiles legacy plugin-handle ownership paths (`internal/kernel/plugin_runtime.go`, `internal/kernel/plugin_tools.go`, plugin-handle hook subscribers, and plugin snapshot state), so the final deletion slice will likely touch both production code and legacy tests together. `TABULA_SKIP_MCP=1` remains unresolved for run mode.
- Open issues: Delete the remaining kernel stdio plugin ownership/runtime surfaces; keep the strengthened supervised-runtime smoke green during that deletion; revisit `TABULA_SKIP_MCP=1` only if run-mode MCP policy still needs an explicit M2 decision.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 13 — [CONTRIBUTE]
- Role: Legacy runtime ownership minimizer
- Work: Removed eager `plugin.NewRuntime()` construction from `kernel.NewHub` so the live kernel no longer instantiates legacy stdio plugin runtime machinery on every startup. Added a focused regression test that asserts the legacy runtime remains nil until `RegisterPlugin(...)` is explicitly used.
- Addressed: Narrowed the remaining kernel stdio ownership surface from the active handoff by ensuring supervised-runtime boots no longer even allocate the old plugin runtime by default.
- Files: `internal/kernel/kernel.go`; `internal/kernel/plugin_runtime_test.go`
- Risks: The legacy plugin runtime/dispatch/hook/snapshot code still compiles and remains reachable from tests or explicit `RegisterPlugin(...)` use; full deletion will still require a broader multi-file cleanup.
- Open issues: Delete the remaining compiled kernel stdio plugin ownership/runtime surfaces while keeping the supervised-runtime smoke green; revisit `TABULA_SKIP_MCP=1` only if run-mode MCP policy still needs an explicit M2 decision.
- Quality: Accuracy 5/5, Completeness 3/5, Coherence 5/5, Applicability 4/5, Mission 4/5

### 2026-05-03 — BUILD progress (Agent 13)
- Timestamp: 2026-05-03
- Requirement: Continue BUILD by shrinking the remaining kernel stdio ownership footprint without destabilizing the already-landed supervised runtime path.
- Approach: Re-read the active handoff plus the actual kernel plugin-runtime and hub initialization seams. Instead of attempting the full legacy deletion in one pass, removed the eager legacy runtime allocation from `NewHub` and added a focused regression test to lock in lazy-only behavior while preserving explicit `RegisterPlugin(...)` compatibility.
- Files modified: `internal/kernel/kernel.go`, `internal/kernel/plugin_runtime_test.go`.
- Testing results: `go test ./internal/kernel` ✅

#### Agent 1 — [CONTRIBUTE]
- Role: Security re-entry sanitizer
- Work: Closed the blocking SECURITY finding by removing plugin-controlled structured `fields` from kernel runtime log output and replacing them with a `fields_redacted` marker. Also hardened runtime snapshot diagnostics so catalog/lifecycle messages no longer trust runtime-provided text, and added kernel regression tests that prove secrets do not leak into logs or `SnapshotRuntimes()` output.
- Addressed: N/A — first agent
- Files: `internal/kernel/runtime_async.go`; `internal/kernel/runtime_registry.go`; `internal/kernel/runtime_attach_test.go`; `internal/kernel/runtime_async_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Status diagnostics are now intentionally coarse (canonical state strings only), so operators lose runtime-provided detail until a future allowlisted redaction policy is designed; broader kernel stdio plugin ownership deletion is still outstanding outside this security slice.
- Open issues: Rerun SECURITY against the patched log/snapshot surfaces; if clean, continue the remaining kernel stdio plugin ownership deletion backlog while preserving the new leak-regression coverage.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Runtime snapshot leak-regression hardener
- Work: Re-read the security report plus the real runtime snapshot/read-model seams and added focused coverage for the remaining detached-runtime boundary. `SnapshotRuntimes()` is now explicitly regression-tested to ensure `last_error` stays sanitized to the fixed coarse string and never echoes token/password material from disconnect errors.
- Addressed: Closed the coverage gap left after Agent 1 where target diagnostics and plugin logs were protected, but detached runtime `last_error` had no direct regression guard.
- Files: `internal/kernel/snapshot_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Operator-facing runtime diagnostics remain intentionally coarse until an allowlisted/redaction policy is designed; kernel stdio plugin ownership deletion is still the larger remaining BUILD backlog.
- Open issues: Rerun SECURITY against the patched log/snapshot surfaces; if clean, continue the remaining kernel stdio plugin ownership deletion backlog while preserving the expanded leak-regression coverage.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-04 — BUILD progress (Agent 1)
- Timestamp: 2026-05-04
- Requirement: Re-enter BUILD to close SECURITY attempt 1's blocking logging/PII finding and the related runtime-diagnostics warning.
- Approach: Re-read the security report plus the real runtime async sink/registry/snapshot seams, then removed kernel emission of plugin-controlled structured fields, normalized runtime snapshot diagnostics to canonical state strings, and added focused regression tests that assert secrets cannot leak through either surface.
- Files modified: `internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/runtime_attach_test.go`, `internal/kernel/runtime_async_test.go`.
- Testing results: `go test ./internal/kernel` ✅

### 2026-05-04 — BUILD progress (Agent 2)
- Timestamp: 2026-05-04
- Requirement: Strengthen the security re-entry regression coverage so detached runtime read-model errors cannot silently reintroduce secret leakage through `tabula status --json` / `SnapshotRuntimes()`.
- Approach: Started from the latest BUILD handoff and security report, then verified the remaining snapshot read-model seam in `internal/kernel/snapshot.go` and `internal/kernel/runtime_registry.go`. Added a focused detached-runtime regression test that drives `MarkDetached(...)` with secret-bearing error text and asserts the exported snapshot keeps only the coarse sanitized `runtime connection failed` string.
- Files modified: `internal/kernel/snapshot_test.go`.
- Testing results: `go test ./internal/kernel` ✅

#### Agent 3 — [DECLINE]
- Role: N/A
- Work: Re-read the current security re-entry handoff, findings, and actual kernel runtime async/registry/snapshot test seams; then re-ran `go test ./internal/kernel` to verify the redaction and sanitized-diagnostic regressions stay green. I did not find an additional scoped BUILD change beyond the already-landed fixes and coverage.
- Addressed: Confirmed Agent 1 and Agent 2 already closed the blocking plugin-log `fields` leak and the detached-runtime `last_error` regression gap described in the SECURITY report.
- Files: none
- Risks: none
- Open issues: rerun SECURITY against the patched kernel log/snapshot surfaces; if it passes, resume the broader kernel stdio plugin ownership deletion backlog.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 4 — [DECLINE]
- Role: N/A
- Work: Re-read the active security re-entry handoff plus the actual patched kernel surfaces (`runtime_async.go`, `runtime_registry.go`, `runtime_async_test.go`, `snapshot_test.go`) and the attempt-1 SECURITY findings, then re-ran `go test ./internal/kernel`. I did not find another meaningful BUILD delta inside this re-entry slice beyond the already-landed sanitization and regression coverage.
- Addressed: Confirmed Agent 1 and Agent 2 already closed the blocking structured-log `fields` leak, the warning-level runtime diagnostic exposure, and the detached-runtime `last_error` regression gap that SECURITY attempt 1 called out.
- Files: none
- Risks: none
- Open issues: rerun SECURITY against the patched kernel log/snapshot surfaces; if it passes, return to the broader kernel stdio plugin ownership deletion backlog.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 5 — [DECLINE]
- Role: N/A
- Work: Re-read the current BUILD re-entry handoff plus the actual patched kernel log/snapshot seams (`internal/kernel/runtime_async.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/runtime_async_test.go`, `internal/kernel/runtime_attach_test.go`, `internal/kernel/snapshot_test.go`) and the SECURITY attempt-1 findings, then re-ran `go test ./internal/kernel`. I did not find another scoped BUILD change that adds meaningful value before SECURITY reruns.
- Addressed: Confirmed the prior agents already covered the only concrete re-entry gaps from attempt 1: plugin-log `fields` are dropped at the kernel boundary, runtime diagnostics are canonicalized, and detached runtime `last_error` has explicit leak-regression coverage.
- Files: none
- Risks: none
- Open issues: rerun SECURITY against the patched kernel log/snapshot surfaces; if it passes, resume the broader kernel stdio plugin ownership deletion backlog.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

### Pipeline Plan Log

#### PLAN Router — [DONE]
- Role: Sequential pipeline router finalization
- Work: Confirmed the PLAN phase has accumulated sufficient evidence and handoff detail across Agents 1-7, then advanced the task to CREATIVE.
- Addressed: Finalized the PLAN phase mechanically by updating the Phase Status block and next-phase handoff.
- Files: `memory-bank/tasks.md`; `memory-bank/activeContext.md`; `memory-bank/progress.md`
- Risks: None recorded in PLAN finalization.
- Open issues: CREATIVE should resolve the plugin-control contract, runtime supervision shape, and installed-layout smoke/release decisions before BUILD.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 1 — [CONTRIBUTE]
- Role: Initial L4 runtime-cutover architect and evidence-gate planner
- Work: Validated task metadata, read the required Memory Bank context, pulled in only task-referenced optional Memory Bank/docs, and spot-checked the actual M2 runtime/kernel seams. Added an initial task plan covering M2-04/M2-04b evidence, M2-07 managed-child/plugin-dispatch deletion cutover, M2-08 installed-layout smoke/release packaging, and M3-M6 sequencing.
- Addressed: N/A — first agent
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Runtime API `Capability{target, tools}` may not yet preserve plugin schema/hook subscription/update metadata needed to replace kernel plugin registration; M2-07 remains blocked on companion repo SHA/PR evidence; broad race gates may be noisy due pre-existing plugin/tool-dispatch races; `.goreleaser.yaml` and absent `scripts/bootstrap.sh` indicate M2-08 packaging/smoke work is non-trivial.
- Open issues: Later PLAN agents should refine the plugin catalog/hook compatibility strategy, define the local backend/process-supervision shape, map exact M2-08 testbed files and release changes, and decide whether CREATIVE docs are needed for the M2-07 catalog contract before BUILD.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence N/A (first agent), Applicability 4/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Plugin catalog/hook compatibility planner
- Work: Investigated Agent 1's top open issue by reading the actual plugin stdio protocol, handle/hook adapter, dynamic catalog validation, runtime manifest/capability, worker protocol, and client init/snapshot paths. Added a concrete catalog/hook contract gap analysis to `tasks.md`, including two possible strategies and required tests before M2-07 BUILD.
- Addressed: Refined Agent 1's unresolved Runtime API capability risk into specific missing semantics: tool schema/deadline, hook subscriptions, dynamic `update_tools`, plugin event replies, plugin-originated `send`, and logs.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: M2-04 currently includes `test-fixtures/testbed-dynamic-tools/`, but current worker/runtime API protocols cannot represent dynamic `update_tools`; choosing a static manifest-authoritative cutover may be a behavior change that must be approved and documented before deleting kernel stdio.
- Open issues: Later PLAN agents should inspect companion `tabula-bundles` plugins/fixtures (`base/mcp`, hook plugins, dynamic-tools fixture) to determine whether static manifest-authoritative cutover is viable, or else require a CREATIVE/API extension for bidirectional plugin control before M2-07.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Companion plugin behavior evidence planner
- Work: Inspected targeted `tabula-bundles` and `tabula-distrib` plugin sources called out by the handoff. Added companion behavior evidence showing `base/mcp` and `testbed-dynamic-tools` materially depend on `update_tools`, testbed hook plugins rely on dynamic SDK hook registration without manifest hooks, hook plugins require event/reply semantics, and `gateway-telegram` requires lifecycle/logging decisions.
- Addressed: Resolved Agent 2's key viability question: static manifest-authoritative M2-07 cutover is not viable against current M2 scope unless scope is explicitly reduced. Recommended a CREATIVE/API bidirectional plugin-control extension before BUILD.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Bidirectional extension expands M2-07 beyond the original minimal call/result worker protocol; if product chooses to defer dynamic semantics instead, docs/issues/tests across core, bundles, and distro must be amended before code deletion to avoid silent behavior loss.
- Open issues: Later PLAN agents should specify the exact Runtime API/worker frame roster and ownership boundaries for catalog updates, hook event replies, lifecycle, structured logs, and plugin-originated `send`; then map local backend supervision and M2-08 installed-layout smoke/release packaging.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Runtime API / worker protocol contract extension planner
- Work: Read the existing Runtime API creative contract and actual runtime/kernel adapter seams, then added a concrete bidirectional plugin-control plan. Specified richer capability metadata, Runtime API async ops (`catalog_update`, `hook_event`, `hook_event_reply`, `plugin_send`, `plugin_log`, `lifecycle_notice`), worker protocol frames, kernel/runtime ownership boundaries, sequencing, and deletion-gate tests.
- Addressed: Turned Agent 3's broad bidirectional-extension requirement into an actionable contract roster and implementation sequence for CREATIVE/BUILD, including the need to refactor `policy/bare` around a per-worker frame router and add RuntimeConn async sinks.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Async Runtime API frames require new callback ownership in `internal/runtime/conn` and Hub sinks; warm-initializing plugins to discover dynamic catalogs may affect startup latency/failure behavior; the current `policy.Worker` interface is too synchronous for async catalog/hooks/logs and must be redesigned carefully.
- Open issues: Later PLAN agents should decide warm-initialize-vs-lazy catalog discovery policy, refine local backend child supervision/runtime.toml generation, map M2-08 installed-layout/release packaging, and record companion repo SHA/PR evidence expectations.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Local runtime supervision and installed-layout validation planner
- Work: Inspected `tabula serve`, runtime config/dialer/main, status, release, Makefile, and testbed runner surfaces. Added a concrete M2-07 local supervisor/config/status plan plus M2-08 release/testbed/bootstrap/CI smoke requirements.
- Addressed: Closed Agent 4's handoff item to refine local backend child supervision/runtime.toml generation and map installed-layout/release packaging. Confirmed current gaps: no runtime config writer/forked child, GoReleaser/Makefile/testbed only build `tabula`, and `scripts/bootstrap.sh` is absent.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Runtime child binary resolution must work for dev installs, release archives, and Windows constraints; installed smoke capability assertions depend on the unresolved warm-vs-lazy catalog discovery decision; `tabula run` still uses in-process plugin loading and may need separate cutover planning if stdio plugin transport is deleted atomically.
- Open issues: Later PLAN agents should resolve `tabula run` cutover scope, decide warm-initialize vs lazy catalog discovery, and formalize companion SHA/PR evidence capture before PLAN finalization.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Run-mode cutover and catalog-readiness planner
- Work: Inspected one-shot `tabula run`, `Hub.RunOneShot`, kernel shutdown, stdio plugin lifecycle/dispatch, and snapshot seams. Added a plan requiring `tabula run` to share the managed local runtime path, replacing kernel plugin load/reload callers, using manifest-seeded worker-confirmed catalog readiness, and capturing concrete companion repo evidence.
- Addressed: Resolved Agent 5's `tabula run` scope risk by recommending run-mode cutover in M2-07 rather than serve-only deletion; refined Agent 4's warm-vs-lazy issue into manifest-seeded/worker-confirmed readiness with per-target status; added companion evidence checklist.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: `TABULA_SKIP_MCP=1` in run mode may intentionally or accidentally suppress MCP after runtime cutover; `watchReloadTrigger` currently calls `hub.ReloadPlugins` and must become a Runtime API reload path; `SnapshotPlugins` consumers may need migration to runtime-hosted target diagnostics.
- Open issues: CREATIVE still must formalize async Runtime API JSON fields/callback ownership; later PLAN agents should prepare concise PLAN-to-CREATIVE handoff and decide whether any remaining gaps justify another pass.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 7 — [CONTRIBUTE]
- Role: CREATIVE contract handoff planner
- Work: Re-read the accepted Runtime API creative contract and actual wire/conn/worker decode seams, then added a concise CREATIVE handoff. Recommended a companion creative doc for plugin-control, callback sinks on RuntimeConn, explicit async op schemas, worker protocol envelope/demux, backpressure/severity rules, and BUILD sequencing.
- Addressed: Closed Agent 6's open issue to prepare PLAN-to-CREATIVE handoff and sharpened Agent 4's async callback ownership gap into concrete design decisions CREATIVE must make.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Callback sink default still needs CREATIVE validation against reconnect/backpressure; worker protocol envelope is a breaking SDK-major change that companion M2-04 must align with; malformed async frame severity must be chosen carefully to avoid fail-open hook/catalog behavior.
- Open issues: Remaining work is primarily CREATIVE, not PLAN: choose callback sink/event channel/control connection, freeze JSON schemas, and formalize worker demux. Later PLAN agents may decline unless they find a concrete uncovered gap.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: CREATIVE plugin-control contract designer
- Work: Created `memory-bank/creative/creative-runtime-plugin-control-contract.md` and froze the single-connection callback-sink design, rich capability/tool/hook schema shapes, async Runtime API op roster, worker `op` envelope and demux rules, per-frame backpressure/fatality rules, and runtime-hosted target readiness/status states.
- Addressed: Resolved Agent 7's open CREATIVE decisions and gave BUILD exact contract shapes for dynamic catalog updates, hook replies, plugin bus sends, structured logs, and lifecycle notices.
- Files: `memory-bank/creative/creative-runtime-plugin-control-contract.md`; `memory-bank/tasks.md`; `memory-bank/activeContext.md`; `memory-bank/progress.md`; `memory-bank/systemPatterns.md`
- Risks: BUILD must keep sink callbacks cheap enough not to stall the shared RuntimeConn read loop; worker-protocol breaking changes must land atomically with the companion SDK/plugin migrations; status consumers will need additive runtime-target fields when the new readiness model lands.
- Open issues: BUILD still must decide `TABULA_SKIP_MCP=1` post-cutover behavior and exact dev/release binary resolution for `tabula-runtime`; SECURITY later must audit async protocol-fatal paths and log sanitization.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-02 — PLAN progress
- Timestamp: 2026-05-02
- Requirement: Begin Level 4 planning for grouped M2-M6 runtime cutover backlog after VAN, using Intent `implement` and Category `deep`.
- Approach: Read required Memory Bank files first, then task-referenced backlog/project/system/tech/archive context and active M2 issue/ADR/amendment docs; verified current source seams for runtime attachment, daemon config/dialer, kernel plugin stdio execution, tool dispatch, release packaging, and testbed presence.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. Initial plan confirms the repo is pre-M2-07: runtime listener/status exist, managed `tabula-runtime` child startup and plugin dispatch cutover do not; kernel stdio plugin runtime still exists by design until the deletion gate.

### 2026-05-02 — PLAN progress (Agent 2)
- Timestamp: 2026-05-02
- Requirement: Close or sharpen Agent 1's M2-07 plugin catalog/hook compatibility planning gap.
- Approach: Compared current kernel plugin `register`/`update_tools`/hook/event semantics with runtime `Capability{target, tools}` and worker `WorkerCall`/`WorkerResult` protocols.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. The plan now requires an explicit catalog/hook strategy or creative extension before M2-07 can safely delete kernel stdio plugin transport.

### 2026-05-03 — PLAN progress (Agent 3)
- Timestamp: 2026-05-03
- Requirement: Verify companion plugin behavior to choose the M2-07 catalog/hook strategy.
- Approach: Read targeted companion repo files for MCP dynamic tools, dynamic-tools fixture, hook fixtures, static hook manifests, and gateway Telegram lifecycle/logging; compared them against current Runtime API and worker protocol frame sets.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. Evidence supports routing the task through CREATIVE for a bidirectional plugin-control contract before M2-07 BUILD unless M2 scope is intentionally reduced and acceptance criteria are rewritten.

### 2026-05-03 — PLAN progress (Agent 4)
- Timestamp: 2026-05-03
- Requirement: Specify the bidirectional plugin-control contract shape needed before M2-07 deletes the kernel stdio plugin path.
- Approach: Compared the accepted typed Runtime API contract, current `Capability`/`RuntimeConn`/daemon handler implementation, current worker protocol and bare policy, and existing kernel plugin catalog/hook/send/log semantics; drafted the minimum Runtime API and worker frame roster plus sequencing/tests.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. The plan now gives CREATIVE an explicit contract-extension checklist and flags the worker frame-router / RuntimeConn async-sink refactors as prerequisites to M2-07 deletion.

### 2026-05-03 — PLAN progress (Agent 5)
- Timestamp: 2026-05-03
- Requirement: Refine M2-07 local backend supervision/runtime config and M2-08 installed-layout smoke/release packaging.
- Approach: Read the actual serve/status/runtime config/dialer/main code plus GoReleaser, Makefile, M2-08 issue, and testbed runner/template files; compared them against the M2-07/M2-08 acceptance criteria.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. The plan now identifies exact build/release/testbed/bootstrap gaps and gives BUILD a supervisor/config/readiness/shutdown sequence for the managed local runtime child.

### 2026-05-03 — PLAN progress (Agent 6)
- Timestamp: 2026-05-03
- Requirement: Resolve run-mode cutover scope, catalog discovery/readiness, and companion evidence planning.
- Approach: Read the one-shot `tabula run` path, Hub one-shot/shutdown behavior, kernel stdio plugin lifecycle/dispatch code, and runtime/plugin snapshots; compared those callers against M2-07's physical deletion requirement.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. The plan now requires `serve` and `run` to share local runtime orchestration, replaces `hub.ReloadPlugins` with runtime reload after cutover, and records a concrete companion evidence checklist.

### 2026-05-03 — PLAN progress (Agent 7)
- Timestamp: 2026-05-03
- Requirement: Prepare final PLAN-to-CREATIVE handoff for bidirectional plugin-control contract and async Runtime API ownership.
- Approach: Re-read `creative-runtime-api-contract.md` and the actual Runtime API/worker wire, decode, interface, and connection routing code; identified where new async frames need callback ownership and worker demultiplexing.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Phase Status was not modified per sequential PLAN protocol. The plan now recommends a companion CREATIVE doc and lists the exact design questions that should be answered before BUILD.

### 2026-05-03 — CREATIVE progress
- Timestamp: 2026-05-03
- Requirement: Produce the CREATIVE phase output for the M2-07 plugin-control/runtime cutover contract.
- Approach: Used the PLAN handoff plus the accepted runtime contract/transport docs and current runtime/kernel/worker code seams to choose the callback-sink design, freeze the async op schemas and worker envelope, define readiness/state semantics, and decide M2-07 scope for `plugin_send` and `plugin_log`.
- Files modified: `memory-bank/creative/creative-runtime-plugin-control-contract.md`, `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`, `memory-bank/systemPatterns.md`.
- Verification notes: Phase Status was intentionally not modified for this sequential handoff. The new contract preserves kernel genericity and the no-legacy deletion boundary by keeping all plugin process ownership inside `tabula-runtime`.

### SECURITY Phase — Attempt 1 (2026-05-03)
- Verdict: FAILED
- Blocking findings: 1
- Warning findings: 1
- Degraded checks: none
- Report: memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md

### SECURITY Phase — Attempt 2 (2026-05-04)
- Verdict: PASSED
- Blocking findings: 0
- Warning findings: 1
- Degraded checks: none
- Report: memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md

### 2026-05-04 — REFLECT progress
- Timestamp: 2026-05-04
- Requirement: Complete Level 4 strategic reflection for the active runtime-cutover slice, incorporating the final BUILD state and SECURITY attempt 2 outcome.
- Approach: Validated task metadata, read the active handoff/progress/security evidence plus the plugin-control creative contract, then created `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog.md` with outcome, architecture, testing, security, archive-readiness, and follow-up analysis.
- Files modified: `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/tasks.md`.
- Verification notes: Reflection confirms SECURITY passed on attempt 2 with no blockers and one non-blocking internal-wire warning. The current work is ready for ARCHIVE as a completed BUILD + SECURITY slice, but not as full grouped M2-M6 backlog completion; remaining no-legacy deletion, companion evidence, release/bootstrap smoke, and successor M3-M6 work are preserved as follow-ups. Structured lesson append/rotation is deferred pending L4 second-opinion approval per CR-B.

### REFLECT Second Opinion (2026-05-04)
- Verdict: REQUEST_REVISION
- Revisions consumed: 1 / 1
- Report: memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog-second-opinion.md

### 2026-05-04 — REFLECT revision
- Timestamp: 2026-05-04
- Requirement: Address the second-opinion revision request by strengthening evidence backing in Sections 7-8 and adding explicit SECURITY integration while preserving archive-scope limits and follow-ups.
- Approach: Re-read `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`, the primary reflection, the second-opinion report, and the SECURITY report; then revised `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog.md` to add concrete file/test/report citations plus a dedicated Security Review Integration subsection.
- Files modified: `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/tasks.md`.
- Verification notes: REFLECT is complete again and ready for a fresh second-opinion pass. The revision keeps exact SECURITY attempt-2 facts (`PASSED`, 0 blockers, 1 warning, 0 degraded checks), preserves QA-skipped truth, and leaves follow-up scope unchanged.

### REFLECT Second Opinion (2026-05-04)
- Verdict: APPROVED
- Revisions consumed: 1 / 1
- Report: memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog-second-opinion.md

### ARCHIVE Phase — DONE (2026-05-04)
- ✅ Archive artifact created: `memory-bank/archive/archive-execute-m2-m6-runtime-cutover-backlog.md`
- ✅ tasks.md reset to "No active tasks"
- ✅ activeContext.md reset to "No active context"
- ✅ progress.md preserved as historical log
- ✅ Creative and reflection artifacts preserved as permanent references
