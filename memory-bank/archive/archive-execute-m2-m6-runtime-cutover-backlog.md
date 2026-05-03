# System Archive: Tabula Runtime Cutover Backlog Slice
Date Archived: 2026-05-04
Status: COMPLETED & ARCHIVED

## 1. System Overview
- Description: This archive preserves the completed Level 4 slice of Tabula's grouped M2-M6 runtime cutover backlog. The archived outcome is the delivered BUILD + SECURITY slice: richer Runtime API and worker-protocol plugin-control semantics, runtime-backed kernel catalog/hook dispatch, managed sibling `tabula-runtime` supervision for `tabula serve` and `tabula run`, runtime config/reload handoff, stronger dual-binary smoke coverage, and post-SECURITY hardening of runtime log/snapshot trust boundaries.
- Complexity: Level 4
- Duration: 2026-05-02 to 2026-05-04
- Business objectives met:
  - Made runtime-hosted plugin control real without breaking kernel genericity.
  - Shifted local runtime startup for `serve`/`run` onto a managed sibling `tabula-runtime` path.
  - Strengthened installed-layout/runtime smoke expectations around attach state, PID visibility, capability evidence, and teardown.
  - Closed the blocking security leak around plugin-controlled log fields and externally publishable runtime diagnostics.
  - Preserved honest scope boundaries by archiving this as a completed slice, not full grouped M2-M6 completion.

## 2. Architecture Documentation
- Architecture style: Runtime-daemon execution architecture with a single authenticated kernel↔runtime connection, runtime-owned worker processes, runtime-backed kernel catalog/hook/status read models, and managed local sidecar supervision for `tabula` entrypoints.
- Key architectural decisions:
  - Extended the Runtime API and worker protocol to preserve plugin-control semantics across the cutover instead of reverting to manifest-only behavior.
  - Kept runtime as the only plugin/worker process owner; kernel remained a generic router, read-model owner, and policy boundary.
  - Used a single `RuntimeConn` with callback-sink ownership for async runtime-originated frames.
  - Switched live reload behavior to fail closed when no attached local runtime exists instead of reviving kernel-owned plugin reload paths.
  - Canonicalized runtime diagnostics at kernel publication boundaries to prevent untrusted text leakage.
- Component structure:
  - `internal/runtime/**`: Runtime API wire contract, async connection routing, mocks, worker protocol contract.
  - `cmd/tabula-runtime/**`: daemon handler, config, dialer, pool, bare worker policy, manifest-backed capability state.
  - `internal/kernel/**`: runtime attach/registry/snapshot state, runtime-backed hook/tool dispatch, async sink handling.
  - `cmd/tabula/**`: managed local runtime supervision, runtime config generation, reload integration, `serve`/`run` cutover entrypoints.
  - `tools/tabula-testbed/**`: isolated runtime/status smoke guardrails.
- Integration points:
  - Authenticated local runtime attach over unix socket plus token file.
  - Runtime async frames feeding kernel catalog/hook/bus/log/lifecycle handling.
  - Managed sibling `tabula-runtime` binary launched by `tabula serve` and `tabula run`.
  - Packaging/install/testbed paths updated to stage both `tabula` and `tabula-runtime`.

## 3. Design Decisions
- Summary of all creative phase outcomes:
  - Selected the single-connection callback-sink model for async plugin-control traffic.
  - Froze richer capability metadata carrying tool schemas, deadlines, and hook subscriptions.
  - Added explicit async Runtime API operations for catalog updates, hook events/replies, plugin bus sends, plugin logs, and lifecycle notices.
  - Chose a worker `op` envelope with single-reader demultiplexing to preserve dynamic tool and hook behavior.
  - Preserved managed local runtime supervision and no-kernel-fallback execution boundaries.
- Links to creative documents:
  - `memory-bank/creative/creative-runtime-plugin-control-contract.md`
  - `memory-bank/creative/creative-runtime-api-contract.md`

## 4. Implementation Details
- Phased implementation summary:
  - Phase 1: PLAN identified that manifest-only cutover was insufficient and forced a bidirectional plugin-control design.
  - Phase 2: CREATIVE fixed the Runtime API/worker contract, readiness model, and supervision boundaries.
  - Phase 3: BUILD landed runtime async routing, runtime-backed kernel adapters, worker event routing, managed local runtime supervision, packaging/install/testbed updates, and reload cutover.
  - Phase 4: SECURITY found a blocking trust-boundary leak, BUILD re-entered narrowly to sanitize log/snapshot outputs, and SECURITY passed on attempt 2.
  - Phase 5: REFLECT and second opinion approved archiving this work as a completed slice with preserved follow-ups.
- Primary components:
  - Runtime API async/plugin-control contract
  - Worker protocol op-envelope and router
  - Runtime pool/daemon capability and lifecycle publication
  - Kernel runtime async sink, registry, hook subscriber, and runtime-backed dispatch
  - Managed local runtime supervisor for `serve`/`run`
  - Dual-binary packaging and installed-layout smoke runner
- Technology stack:
  - Go and Go standard library
  - Unix domain sockets for local runtime transport
  - TOML runtime configuration
  - Runtime-owned subprocess workers using NDJSON worker protocol
  - Existing testbed tooling and release/install scripts
- Key files and directories:
  - `cmd/tabula/main.go`
  - `cmd/tabula/local_runtime.go`
  - `cmd/tabula-runtime/config/config.go`
  - `cmd/tabula-runtime/daemon/handler.go`
  - `cmd/tabula-runtime/dialer/dialer.go`
  - `cmd/tabula-runtime/policy/bare/bare.go`
  - `cmd/tabula-runtime/pool/pool.go`
  - `internal/kernel/runtime_async.go`
  - `internal/kernel/runtime_attach.go`
  - `internal/kernel/runtime_hooksub.go`
  - `internal/kernel/runtime_registry.go`
  - `internal/kernel/snapshot.go`
  - `internal/kernel/tool_dispatch.go`
  - `internal/kernel/tool_service.go`
  - `internal/runtime/api.go`
  - `internal/runtime/conn/conn.go`
  - `internal/runtime/wire/types.go`
  - `internal/runtime/worker/wire/types.go`
  - `.goreleaser.yaml`
  - `Makefile`
  - `scripts/install-dev.sh`
  - `scripts/install.sh`
  - `scripts/install-dev.ps1`
  - `scripts/install.ps1`
  - `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`

## 5. Testing Documentation
- Testing strategy: Focused Go tests validated runtime wire/conn/worker/pool/kernel/command changes first, then the isolated testbed runner enforced supervised-runtime attachment, capability evidence, and teardown behavior. Security re-entry added targeted regression coverage on trust-boundary publication surfaces.
- Test coverage:
  - Runtime protocol, connection, daemon, dialer, pool, and worker policy tests
  - Kernel runtime attach, dispatch, snapshot, async sink, and status-related tests
  - Command-level tests for runtime config writing, managed supervision, and reload behavior
  - Testbed runner checks for dual-binary layout, generated `runtime.toml`, runtime attach, PID visibility, capability evidence, and clean shutdown
- Performance test results: No dedicated benchmark suite was recorded in Memory Bank for this slice.
- SECURITY Phase 4.7 report: `memory-bank/security/security-execute-m2-m6-runtime-cutover-backlog.md`
- SECURITY verdict / attempts: PASSED, 2
- SECURITY evidence directory: `memory-bank/security/artifacts/execute-m2-m6-runtime-cutover-backlog/attempt-2/`
- REFLECT second-opinion report: `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog-second-opinion.md`
- REFLECT second-opinion verdict: APPROVED
- Known limitations:
  - `QA` was skipped for this task and no QA report exists.
  - Remaining compiled kernel stdio plugin ownership/runtime surfaces still need deletion before M2-07 is fully closed under the no-legacy rule.
  - Release/bootstrap proof for installed binaries is still incomplete; current smoke is isolated source-built coverage.
  - Companion M2-04/M2-04b SHA/PR/test evidence is still missing from this task record.
  - One non-blocking warning remains about worker-controlled crash text on the authenticated internal runtime wire; current kernel consumers canonicalize it before publication.

## 6. Deployment Information
- Deployment approach: The shipped slice moves local runtime behavior toward a dual-binary deployment model where `tabula` manages a sibling `tabula-runtime` process and runtime config in `$TABULA_HOME/config/runtime.toml`, with runtime attach over the local authenticated socket.
- Configuration requirements:
  - `TABULA_HOME` is the runtime/config/state root.
  - Runtime config uses strict `[[kernel]]` entries and `token_file`.
  - Local runtime attach requires the issued token file and the kernel runtime socket under `$TABULA_HOME/run/`.
  - Install/release layouts must place `tabula-runtime` alongside `tabula`.
- Environment dependencies:
  - Unix socket support for the local runtime path.
  - Runtime worker executables for migrated plugins.
  - Installer/testbed surfaces that preserve the dual-binary layout.

## 7. Maintenance Guide
- Key operational procedures:
  - Use `tabula status --json` to verify local runtime attachment, PID, and capability evidence.
  - Preserve strict runtime config parsing; update configs to canonical shapes rather than adding aliases.
  - Keep supervised-runtime smoke green while removing remaining legacy kernel plugin ownership surfaces.
  - Treat trust-boundary canonicalization of runtime diagnostics as required until a tighter allowlisted redaction policy exists.
- Monitoring points:
  - Runtime attach/detach state and runtime PID visibility
  - Runtime capability presence and lifecycle state in status/snapshots
  - Managed child teardown and socket cleanup during shutdown
  - Sanitized runtime log/status outputs after crashes or disconnects
- Common troubleshooting:
  - Runtime not attached: verify runtime socket, token file, generated `runtime.toml`, and sibling `tabula-runtime` resolution.
  - Reload failure: confirm the local runtime is attached; reload now fails closed without legacy plugin fallback.
  - Missing runtime capability evidence: inspect runtime pool initialization/catalog publication and testbed smoke assertions.
  - Coarse diagnostics: expected after security hardening; detailed crash text should not be trusted outside authenticated internal runtime boundaries.

## 8. Reflection & Strategic Insights
- Link: `memory-bank/reflection/reflection-execute-m2-m6-runtime-cutover-backlog.md`
- Top strategic insights:
  1. Atomic runtime cutovers need both live-path cutover and compiled legacy-surface deletion gates.
  2. Canonicalization at publication boundaries is an effective short-term defense for untrusted runtime text.
  3. Installed-layout smoke that checks real runtime attach, PID visibility, capability evidence, and teardown is a durable acceptance pattern for runtime migrations.

## 9. Known Issues & Future Roadmap
- Deferred items:
  - Delete the remaining compiled kernel stdio plugin ownership/runtime surfaces and keep the supervised-runtime smoke green.
  - Capture M2-04 and M2-04b companion repo SHA/PR/test evidence for the worker-protocol cutover.
  - Extend M2-08 proof from isolated source-built smoke to release/bootstrap installed binaries for `tabula` + `tabula-runtime`.
- Future enhancements:
  - Decide and document whether `TABULA_SKIP_MCP=1` remains valid for `tabula run` after the runtime cutover.
  - Split the remaining M3-M6 backlog into successor tasks and continue execution with narrower archive boundaries.
  - Continue skill runtime cutover, tenant enforcement, workspace decomposition, and remote service closure work in successor tasks.
- Technical debt:
  - Remaining compiled kernel stdio plugin runtime/dispatch/test surfaces.
  - Release/bootstrap installed-layout validation gap.
  - Lifecycle crash-text warning on the authenticated internal runtime wire until a stricter runtime-layer redaction policy is chosen.
