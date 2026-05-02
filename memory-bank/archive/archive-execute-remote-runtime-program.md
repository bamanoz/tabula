# System Archive: Remote Runtime Program Foundation
Date Archived: 2026-05-02
Status: COMPLETED & ARCHIVED

## 1. System Overview
- Description: This archive covers the completed BUILD scope for the Tabula remote-runtime program task, specifically the M1 Runtime API contract spine and the M2 pre-cutover runtime foundation. The work established transport-independent Runtime API wire contracts, worker protocol types, kernel-facing runtime interfaces and mocks, production codec/unix transport packages, a runnable `tabula-runtime` daemon skeleton, runtime-side plugin manifest discovery, worker pooling and bare worker execution, local token authentication, kernel runtime attachment/read-model support, and `tabula status --json` readiness surfaces.
- Complexity: Level 4
- Duration: 2026-05-02 to 2026-05-02. Exact wall-clock duration was not recorded in Memory Bank.
- Business objectives met:
  - Established the tested foundation for remote execution and future multi-backend runtime work.
  - Preserved Tabula kernel generic boundaries by placing runtime contract/worker execution in `internal/runtime/**` and `cmd/tabula-runtime/**` while keeping kernel changes scoped to listener/auth/read-model/status seams.
  - Reduced cutover risk by implementing strict Runtime API validation, strict runtime config parsing, and pre-cutover status/readiness paths before M2-07.
  - Preserved atomic program gates: M2-07 plugin dispatch cutover, M2-08 installed-layout smoke, and M3-M6 remain explicit follow-up work rather than being overclaimed as complete.

## 2. Architecture Documentation
- Architecture style: Runtime-daemon execution architecture with a typed Runtime API, kernel-listens/runtime-dials local transport, bearer-token first-frame authentication, and runtime-owned worker process supervision. The kernel remains a generic router/read-model owner and does not gain a fallback embedded tool execution path.
- Key architectural decisions:
  - Runtime API uses typed op envelopes and operation-specific payloads rather than generic maps or transport-coupled structs.
  - Invoke targets use canonical `{kind, id}` wire objects; display strings such as `skill:name` are not wire target forms.
  - The canonical wire error roster comes from amendments and excludes stale aliases such as `tenant_denied` and `target_not_authorized`.
  - Local transport follows accepted topology: kernel creates `$TABULA_HOME/run/runtime.sock`, runtime reads `token_file`, dials the kernel, and sends `Hello` as the first authenticated frame.
  - Runtime config is strict and alias-free: runtime-side config uses singular `[[kernel]]` and `token_file`; stale `token`, plural `[[kernels]]`, top-level `runtime_id`, and kernel-side `[[runtime]]` in `runtime.toml` are rejected.
  - Worker execution is runtime-side only. `os/exec` is allowed for runtime worker/backend launch responsibilities, while kernel-side deletion gates remain scheduled for later issues.
- Component structure:
  - `internal/runtime/wire/`: Runtime API frame types, validation, canonical errors, target object shape.
  - `internal/runtime/worker/wire/`: worker NDJSON protocol structs and helpers.
  - `internal/runtime/`: `RuntimeConn`/`Backend` contracts.
  - `internal/runtime/mock/`: programmable in-memory runtime connection for kernel tests.
  - `internal/runtime/codec/`, `conn/`, `transport/unixsock/`, `auth/`: production codec, multiplexed connection, unix transport, and token-auth primitives.
  - `cmd/tabula-runtime/`: runtime daemon CLI, config, dialer, daemon handler, manifest parser, worker pool, and bare worker policy.
  - `internal/kernel/runtime_attach.go`, `runtime_registry.go`, `snapshot.go`: generic runtime attachment/read-model seams.
  - `cmd/tabula/status.go`: human and JSON kernel/runtime status surface.
- Integration points:
  - Runtime attaches to kernel through authenticated Runtime API `Hello` over unix socket.
  - Kernel exposes loopback-only internal runtime snapshot diagnostics for `tabula status`.
  - Runtime daemon discovers plugin manifests from configured plugin dirs, defaults to `$TABULA_HOME/plugins`, and invokes workers through runtime-owned stdio NDJSON.
  - Future integration points remain planned for M2-07 plugin dispatch, M3 skill routing, M4 tenant registry/enforcement, M5 workspace plugins, and M6 WSS/mTLS/SSH/service install.

## 3. Design Decisions
- Summary of creative phase outcomes:
  - Runtime API contract selected typed envelopes with centralized validation, canonical wire errors, explicit cancellation/timeout/disconnect semantics, and contract-test requirements.
  - Runtime daemon transport selected listener-kernel/dial-out-runtime over unix socket with token handshake, reconnect token reread, explicit reload semantics, and no embedded fallback.
  - Tenant/config registry design selected source-aware tenant validation with `default` as system-managed-valid, alias-free config registry shapes, simplified tenant layout, and additive status JSON evolution for later M4.
  - Workspace decomposition selected resolver-first rollout with atomic legacy deletion, preserving global executable code roots and per-tenant introspection symlinks for later M5.
  - Remote-backend security selected layered bearer-token auth plus optional mTLS for WSS, system `ssh` for SSH backend, token lifecycle/revocation requirements, and shell-owned service installation for later M6.
- Links to creative documents:
  - `memory-bank/creative/creative-runtime-api-contract.md`
  - `memory-bank/creative/creative-runtime-daemon-transport.md`
  - `memory-bank/creative/creative-tenant-config-registry.md`
  - `memory-bank/creative/creative-workspace-decomposition.md`
  - `memory-bank/creative/creative-remote-backends-security.md`

## 4. Implementation Details
- Phased implementation summary:
  - Phase 1: VAN classified the task as Level 4 / implement / deep and established the Memory Bank metadata needed for the workflow.
  - Phase 2: PLAN normalized the M1-M6 program against `docs/issues/TASK.md`, the accepted ADR, amendments, issue graph, repo boundaries, and high-risk contradictions; it produced a mechanical execution checklist and cross-repo evidence plan.
  - Phase 3: CREATIVE produced the five design handoff documents listed above.
  - Phase 4: BUILD completed M1 and advanced M2 pre-cutover foundation through 16 pipeline agents, ending before M2-07 because cross-repo companion prerequisites were not yet complete.
  - Phase 4.7: SECURITY reviewed the actual delta and passed attempt 1 with no blocking findings.
  - Phase 5: REFLECT captured outcome, lessons, follow-ups, residual risk, and archive readiness.
  - Phase 6: ARCHIVE preserves the completed scope and resets active Memory Bank files.
- Primary components:
  - Runtime wire contract and worker wire protocol.
  - Runtime connection/codec/unix transport/auth stack.
  - Runtime daemon CLI, config loader, dialer, manifest discovery, worker pool, and bare worker execution policy.
  - Kernel runtime attachment/read model and status CLI.
- Technology stack:
  - Go module and standard library.
  - `github.com/coder/websocket v1.8.14` for Runtime API WebSocket-compatible framing/transport support.
  - Unix domain sockets for local kernel/runtime attachment.
  - TOML runtime config parsing.
  - Runtime-owned subprocess workers using stdio NDJSON.
- Key files and directories:
  - `internal/runtime/**`
  - `cmd/tabula-runtime/**`
  - `internal/kernel/runtime_attach.go`
  - `internal/kernel/runtime_registry.go`
  - `internal/kernel/snapshot.go`
  - `internal/kernel/kernel.go`
  - `cmd/tabula/main.go`
  - `cmd/tabula/status.go`
  - `go.mod`
  - `go.sum`
  - `memory-bank/progress.md`
  - `memory-bank/security/security-execute-remote-runtime-program.md`
  - `memory-bank/reflection/reflection-execute-remote-runtime-program.md`

## 5. Testing Documentation
- Testing strategy: Focused Go tests were run first for new runtime packages, daemon components, kernel attachment/read-model surfaces, and status CLI. Race tests targeted new runtime packages and selected command/kernel paths. End-to-end pre-cutover smoke exercised unix/dialer/RuntimeConn invocation of a real Python NDJSON plugin worker through the new runtime daemon path.
- Test coverage:
  - M1 Runtime API contract tests cover wire/worker/interface/mock behavior, cancellation/disconnect/protocol-error paths, concurrent invoke behavior, canonical error rejection, and worker-frame symmetry.
  - M2 codec/conn/unix transport tests cover validation reuse, concurrent invokes, disconnect drain, protocol-error recovery, unix socket permissions, and authenticated serve paths.
  - Runtime daemon tests cover config loading/strict rejection, dialer reconnect/token reread, graceful SIGTERM smoke, manifest discovery/schema parity, worker pool behavior, bare worker spawn, env propagation, timeout/kill, crash eviction, and reload eviction.
  - Kernel/status tests cover runtime attachment registration/detachment, snapshot output, `tabula status --json`, kernel-down status, running-kernel runtime snapshots, tenant listing, invalid state behavior, and future runtime PID propagation.
- Performance test results:
  - No dedicated benchmark suite was recorded.
  - Functional/performance-relevant guardrails were implemented: 1 MiB frame limit, bounded read/write deadlines, concurrent invoke routing, fail-fast disconnect handling, pending-call drain, warm worker reuse, and worker pool keying by `(kernel_id, tenant_id, target_id)`.
- SECURITY Phase 4.7 report: `memory-bank/security/security-execute-remote-runtime-program.md`
- SECURITY verdict / attempts: PASSED, 1
- SECURITY evidence directory: `memory-bank/security/artifacts/execute-remote-runtime-program/attempt-1/`
- REFLECT second-opinion report: absent. No `memory-bank/reflection/reflection-execute-remote-runtime-program-second-opinion.md` artifact exists in this workspace; reflection notes that structured lesson append/rotation was deferred pending L4 second-opinion approval.
- REFLECT second-opinion verdict: absent / not recorded.
- Known limitations:
  - The archive covers the approved M1/M2 pre-cutover BUILD scope, not completion of the entire M1-M6 remote-runtime program.
  - M2-04/M2-04b companion migrations, M2-07 managed local child/plugin dispatch cutover, and M2-08 installed-layout smoke remain follow-up work.
  - M3 skill unification, M4 tenancy enforcement/config registry, M5 workspace decomposition, and M6 WSS/mTLS/SSH/service/operator closure remain unbuilt.
  - Full Go dependency vulnerability review was degraded because the SECURITY protocol did not include an allowed Go vulnerability-audit command.
  - Pre-existing full-race failures in plugin/tool-dispatch tests were noted as unrelated but should be triaged before later cutovers.

## 6. Deployment Information
- Deployment approach: Current work is a pre-cutover foundation inside the `tabula` repo. The runtime daemon binary can be built and tested, and `tabula serve` creates local runtime-token/socket attachment surfaces, but production plugin dispatch cutover to the managed local runtime child is intentionally deferred to M2-07.
- Configuration requirements:
  - `TABULA_HOME` remains the runtime/config/state root.
  - Runtime-side config defaults to `$TABULA_HOME/config/runtime.toml` and uses singular `[[kernel]]` entries.
  - Runtime config must use `token_file = "${TABULA_HOME}/run/runtime-token"`; inline `token`, plural `[[kernels]]`, top-level `runtime_id`, and kernel-side `[[runtime]]` shapes are rejected.
  - Local runtime auth expects `$TABULA_HOME/run/` mode `0700` and runtime token file mode `0600`.
- Environment dependencies:
  - Unix domain socket support for local runtime transport.
  - Worker runtime executables such as Python for manifest-backed plugin smoke tests.
  - Future installed-layout validation must use installed `tabula` and `tabula-runtime` binaries via testbed, not only in-package tests.

## 7. Maintenance Guide
- Key operational procedures:
  - Use `tabula status` or `tabula status --json` to inspect kernel state and attached runtime read-model data.
  - Treat `cmd/tabula-runtime/config` strict parsing failures as intended drift detection; update configs to the canonical shapes rather than adding aliases.
  - Preserve the M2-07 and M3-08 deletion gates: do not add kernel fallback execution to compensate for incomplete runtime cutover.
  - Before M2-07, collect SHA/PR evidence for `M2-04` and `M2-04b` companion migrations.
- Monitoring points:
  - Runtime attachment/detachment state in the kernel runtime registry.
  - `tabula status --json` runtime entries, capabilities, worker count, and future PID values.
  - Runtime logs for sanitized auth failures, reconnect attempts, worker spawn failures, and reload events.
  - Token file permissions and absence of plaintext token leakage in logs/status.
- Common troubleshooting:
  - Runtime cannot attach: verify `$TABULA_HOME/run/runtime.sock`, runtime `token_file`, token contents, and that `Hello` is the first frame.
  - Runtime config fails to load: check for unsupported stale fields such as `token`, `[[kernels]]`, top-level `runtime_id`, or `[[runtime]]` in runtime-side config.
  - Status shows runtime `pid: 0`: expected until M2-07 supplies the managed local child PID.
  - Worker invocation fails: inspect manifest entry path validity, runtime allowlisted env, worker init/ack timeout, and daemon/pool logs.

## 8. Reflection & Strategic Insights
- Link: `memory-bank/reflection/reflection-execute-remote-runtime-program.md`
- Top strategic insights:
  1. Typed Runtime API envelopes with centralized validation are a durable guardrail against stale issue names, plugin-internal errors, and transport-specific API drift.
  2. For atomic architecture migrations, stopping before a deletion gate can be the correct outcome when companion repositories are not ready; preserving the gate is safer than forcing a partial cutover.
  3. Runtime config should fail closed on unknown fields before managed-child cutover so documentation drift becomes test failure rather than runtime ambiguity.

## 9. Known Issues & Future Roadmap
- Deferred items:
  - Complete M2-04 and M2-04b companion worker-protocol migrations with repo SHA/PR evidence.
  - Execute M2-07 managed local runtime child startup, runtime config generation, plugin dispatch cutover, and kernel stdio plugin transport deletion with no fallback.
  - Add and run M2-08 installed-layout runtime/status smoke coverage using real installed `tabula` and `tabula-runtime`.
  - Add an approved Go dependency vulnerability-review path to SECURITY.
  - Triage pre-existing plugin/tool-dispatch race-test failures before broader runtime cutover.
- Future enhancements:
  - M3: route skill execution through Runtime API, add skill harnesses/cold workers, and delete `internal/kernel/process_manager.go`.
  - M4: implement tenant data model, tenant CLI/config overlay, runtime registry, whitelist enforcement, additive multi-tenant status, and multi-tenant testbed.
  - M5: implement resolver-first workspace decomposition, reusable `workspace/fs` and `workspace/exec` plugins, and delete legacy workspace/file/shell surfaces.
  - M6: implement WSS, opt-in mTLS, SSH backend via system `ssh`, launchd/systemd service scripts, operator docs, and final multi-backend testbed closure.
- Technical debt:
  - The kernel-side stdio plugin path remains until M2-07 by design.
  - Skill subprocess execution remains until M3-08 by design.
  - Runtime manifest parser schema parity must be revalidated against `tabula-bundles`/`tabula-distrib` companion migrations before cutover.
  - Installed-layout/testbed coverage for runtime/status behavior is still required at M2-08.
