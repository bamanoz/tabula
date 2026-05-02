# Progress

Implementation progress is tracked here across all tasks.

## execute-remote-runtime-program

### 2026-05-02 — PLAN progress
- Requirement: Establish the initial Level 4 implementation plan for executing the remote-runtime M1-M6 program from `docs/issues/TASK.md`.
- Approach: Verified required planning inputs, confirmed intent/category metadata, sampled the current kernel/runtime seams in real source files, and drafted a milestone-by-milestone implementation plan in `memory-bank/tasks.md`.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Confirmed `internal/runtime/` and `cmd/tabula-runtime/` are not present yet; verified current stdio plugin spawn, skill process manager, WebSocket client path, `Hub.ProjectRoot`, and testbed runner locations as active seams for later milestones.

### Pipeline Plan Log

#### Agent 1 — [CONTRIBUTE]
- Role: Initial program architect and dependency mapper
- Work: Read the mandatory Memory Bank context and task inputs, verified the accepted ADR/amendment constraints, checked current code seams for plugin stdio, skill subprocess execution, WebSocket clients, project-root metadata, and testbed runner layout, then added an initial M1-M6 plan/scope snapshot to `tasks.md`.
- Addressed: N/A — first agent
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Individual issue files contain stale names overridden by `AMENDMENTS.md`; M5 dependency direction in README conflicts with amendment C7; atomic cutovers M2-07/M3-08/M5-05 require cross-repo merge discipline; testbed coverage can be accidentally under-scoped if agents rely only on unit tests.
- Open issues: Later planning agents should refine the executable dependency graph, especially companion issue sequencing and M5 resolver ordering; verify all individual M2-M6 issue files against amendments before BUILD.
- Quality: Accuracy 4/5, Completeness 3/5, Coherence N/A (first agent), Applicability 4/5, Mission 4/5

#### Agent 2 — [CONTRIBUTE]
- Role: Amendment reconciliation and build-gate planner
- Work: Read the handoff and plan log, then inspected the task brief, ADR, amendments, README dependency tables, and targeted high-risk issue files for C7/C4/C8/C1/C2/C6 companion sequencing. Added an amendment-normalized executable dependency graph, required CREATIVE document list, and milestone validation gates to `tasks.md`.
- Addressed: Refined Agent 1's open dependency-graph issue by normalizing M2 companion merge order, inserting M4-08 before enforcement/backends, correcting the M5 resolver/project-root direction, and adding explicit validation/testbed gates per milestone.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Source issue files still contain stale `Blocked by` text for M5-01/M5-02/M5-04 and stale fallback/config wording in M4 issue text; build agents must use this Memory Bank graph plus `AMENDMENTS.md` as the effective router until issue docs are updated.
- Open issues: Later PLAN agents should spot-check remaining hotspot issues: M1-01 for C8 names, M2-06/M4-06 for C2 status shape, M4-01/M4-04 for tenant layout amendments, M6-01..03 for C4 config examples, and M6-04 for dropped `--foreground` assumptions.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Hotspot contradiction auditor and CREATIVE checklist refiner
- Work: Read the required Memory Bank context, then spot-checked the handoff hotspot issues for stale wire error names, status shape evolution, tenant layout drift, remote backend config drift, and the dropped service foreground flag. Added effective overrides and CREATIVE acceptance checklist refinements to `tasks.md`.
- Addressed: Closed Agent 2's hotspot spot-check request for M1-01, M2-06/M4-06, M4-01/M4-04, M6-01..04; confirmed known amendment overrides and found one unresolved M4 contradiction around reserving `default` while auto-creating `tenants/default/`.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The unresolved `default` tenant invariant could cause either invalid fresh boot behavior or a validation exception/shim if not decided before M4 BUILD; M6 examples still contain inline/plural config forms that must not be copied verbatim.
- Open issues: Resolve or route the `default` tenant validation contradiction; later PLAN agents may convert the graph into a mechanical issue checklist if useful.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Tenant invariant resolver and M4 validation planner
- Work: Read the required Memory Bank context, then inspected M4-01, M4-02, `AMENDMENTS.md`, the remote-runtime plan, and ADR multi-tenant invariants around the `default` tenant. Added a concrete M4 default-tenant invariant, source-aware creation policy, validator-boundary guidance, and M4 test requirements to `tasks.md`.
- Addressed: Resolved Agent 3's open contradiction by treating `default` as a well-known system-managed tenant ID that is valid for runtime/routing/status/env/resolver use, while keeping user creation guarded and hard-reserving `system`, `runtime`, `kernel`, and `admin`.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: If BUILD collapses syntax validation, hard-reserved validation, and user-create policy into one helper, it could reintroduce the contradiction; CREATIVE must name the validator/function boundaries explicitly.
- Open issues: Produce a mechanical issue-by-issue execution checklist if later PLAN agents need more build routing detail; otherwise none from the default-tenant contradiction.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Mechanical execution checklist planner
- Work: Read the required Memory Bank context plus the program task, amendments, README dependency tables, and issue-file inventory. Added an issue-by-issue BUILD routing checklist for M1-M6 that translates the amendment-normalized graph into concrete execution order, repo boundaries, amendment caveats, atomic-gate prerequisites, and validation expectations.
- Addressed: Closed Agent 4's optional open issue by producing the mechanical checklist without broad re-reading of all issue bodies; preserved the existing default-tenant resolution and incorporated it into the M4 execution path.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Checklist is intentionally a planning/router aid, not a replacement for reading each issue file and `AMENDMENTS.md`; BUILD agents could still drift if they copy stale individual issue examples instead of citing the amendment IDs.
- Open issues: None critical for PLAN; later agents may add cross-repo release/merge-window packaging guidance or SECURITY evidence planning if they can do so without duplicating the checklist.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Release packaging and SECURITY evidence planner
- Work: Read the required Memory Bank context, then targeted the handoff's optional release/SECURITY planning path by checking the task brief, amendments, ADR, plan, README, and security-relevant issue slices (`M2-05`, `M4-05`, `M4-08`, `M6-02`, `M6-05`). Added milestone evidence-package requirements, atomic cross-repo merge-window packaging guidance, and SECURITY-phase evidence scope to `tasks.md`.
- Addressed: Expanded Agent 5's optional open issue without duplicating the mechanical checklist; turned cross-repo compatibility and later SECURITY validation into concrete artifacts for BUILD/SECURITY handoff.
- Files: `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: `M4-08` still contains comment examples with `token = "..."` for backend configs; if copied into CREATIVE/BUILD docs it could conflict with the `token_file` amendment and M6 security expectations.
- Open issues: None critical for PLAN; CREATIVE should incorporate the evidence-package and SECURITY-scope requirements into the relevant design docs.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-02 — CREATIVE progress
- Requirement: Produce the five required Level 4 creative design handoff documents for the remote-runtime M1-M6 program.
- Approach: Validated task metadata, read the PLAN handoff plus accepted ADR/program/amendment sources and hotspot issue files, then created task-scoped design docs with architecture options, decisions, implementation guidelines, BUILD checklists, and architecture rubric reviews.
- Files modified: `memory-bank/creative/creative-runtime-api-contract.md`, `memory-bank/creative/creative-runtime-daemon-transport.md`, `memory-bank/creative/creative-tenant-config-registry.md`, `memory-bank/creative/creative-workspace-decomposition.md`, `memory-bank/creative/creative-remote-backends-security.md`, `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`, `memory-bank/systemPatterns.md`, `memory-bank/techContext.md`.
- Verification notes: Confirmed all five creative docs exist and cover the plan refinements: canonical wire errors and target shape; cancellation vs timeout vs disconnect; local daemon token/reload/reconnect/deletion gates; default tenant/config registry/whitelist invariants; resolver-first workspace rollout and legacy deletion; WSS/mTLS/SSH/token/service security posture.

#### CREATIVE Agent — [CONTRIBUTE]
- Role: Level 4 design handoff author
- Work: Created the required creative docs only, consolidating the accepted ADR, amendments, PLAN refinements, issue sequencing, and security evidence requirements into concrete BUILD/SECURITY guidance.
- Addressed: Converted Agents 3/4/5/6 acceptance constraints into design docs, including the default-tenant invariant, C7 resolver order, C8 error roster, C4 config registry shape, M16 service foreground correction, and milestone evidence requirements.
- Files: `memory-bank/creative/creative-runtime-api-contract.md`; `memory-bank/creative/creative-runtime-daemon-transport.md`; `memory-bank/creative/creative-tenant-config-registry.md`; `memory-bank/creative/creative-workspace-decomposition.md`; `memory-bank/creative/creative-remote-backends-security.md`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`; `memory-bank/systemPatterns.md`; `memory-bank/techContext.md`
- Risks: Source issue files still contain stale examples; BUILD must continue treating `AMENDMENTS.md` and the creative docs as the normalized handoff rather than copying stale plural config keys, inline token fields, stale error codes, or service flags.
- Open issues: None blocking BUILD. SECURITY should later audit implementation evidence against `creative-remote-backends-security.md` and the PLAN evidence list.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### 2026-05-02 — BUILD progress
- Requirement: Start M1 implementation for the remote-runtime program without production dispatch changes, using `AMENDMENTS.md` C8/M1/M10 and the Runtime API creative handoff as the normalized source of truth.
- Approach: Captured task base commit `77953ca89eada433d601fbf74495b532af7cf687`, read M1 issue files and amendment/creative constraints, implemented the M1 contract spine, then completed the M1-06 in-memory `net.Pipe` end-to-end fixture and removed mock-test polling/sleep synchronization.
- Files modified: `internal/runtime/wire/types.go`, `internal/runtime/wire/codec.go`, `internal/runtime/wire/types_test.go`, `internal/runtime/worker/wire/types.go`, `internal/runtime/worker/wire/types_test.go`, `internal/runtime/api.go`, `internal/runtime/api_test.go`, `internal/runtime/contract_test.go`, `internal/runtime/mock/mock.go`, `internal/runtime/mock/mock_test.go`, `cmd/tabula-runtime/policy/policy.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/policy/bare/bare_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: `go test ./internal/runtime/... ./cmd/tabula-runtime/policy/...` passed; `go test ./...` passed; `go test -race ./internal/runtime/...` passed.

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: M1 runtime contract foundation implementer
- Work: Implemented behavior-free runtime contract scaffolding: typed Runtime API frames/validation with canonical error roster, worker NDJSON protocol structs, `RuntimeConn`/`Backend` interfaces, runtime-side policy contracts with bare placeholder, and a programmable mock runtime connection with cancellation and close/drain semantics.
- Addressed: N/A — first agent
- Files: `internal/runtime/wire/types.go`; `internal/runtime/wire/codec.go`; `internal/runtime/wire/types_test.go`; `internal/runtime/worker/wire/types.go`; `internal/runtime/worker/wire/types_test.go`; `internal/runtime/api.go`; `internal/runtime/api_test.go`; `internal/runtime/mock/mock.go`; `internal/runtime/mock/mock_test.go`; `cmd/tabula-runtime/policy/policy.go`; `cmd/tabula-runtime/policy/bare/bare.go`; `cmd/tabula-runtime/policy/bare/bare_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: M1-06 full `net.Pipe` end-to-end contract fixture is not yet implemented; mock tests use a small polling loop rather than the M1-06 no-sleep fixture requirement; later agents must keep stale issue error names (`tenant_denied`, `target_not_authorized`) and plugin-internal codes out of wire errors.
- Open issues: Add the explicit M1-06 in-memory end-to-end test covering all 10 scenarios; consider tightening mock test synchronization while doing that; later M2 codec work should reuse/extend the centralized `wire.Decode` validation instead of inventing transport-specific validation.
- Quality: Accuracy 4/5, Completeness 3/5, Coherence N/A (first agent), Applicability 4/5, Mission 4/5

#### Agent 2 — [CONTRIBUTE]
- Role: M1-06 contract fixture implementer and synchronization hardener
- Work: Added `internal/runtime/contract_test.go`, a test-local `net.Pipe` RuntimeConn/fake-runtime fixture covering the M1-06 scenarios: handshake, nominal/error invoke, cancel propagation, health pings during an in-flight call, capability listing, reload, disconnect fail-fast, missing-tenant protocol error, 50 concurrent invokes, and worker-frame symmetry. Replaced mock-test polling/sleep loops with `WaitForRecordedInvokes`, a channel/context synchronization helper.
- Addressed: Resolved Agent 1's open M1-06 full in-memory end-to-end test issue and tightened the no-sleep synchronization risk in mock tests; preserved canonical C8 wire errors by asserting stale/plugin-internal codes are rejected.
- Files: `internal/runtime/contract_test.go`; `internal/runtime/mock/mock.go`; `internal/runtime/mock/mock_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The M1-06 codec remains intentionally test-local until M2 productionizes transport/codec; next agents should avoid copying test fixture concurrency shortcuts into production transport without revisiting framing/backpressure/error-close behavior.
- Open issues: Begin M2-01 only after reading `docs/issues/M2-01-runtime-codec-and-unix-transport.md`, `AMENDMENTS.md`, and `memory-bank/creative/creative-runtime-daemon-transport.md`; keep production transport validation centralized on `wire.Decode`.
- Quality: Accuracy 4/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: M2-01 transport/codec implementer
- Work: Read the M2-01 issue (actual filename `docs/issues/M2-01-wire-codec-unix-socket-transport.md`), amendments, and daemon transport creative doc, then implemented the first production transport slice: `internal/runtime/codec` WebSocket JSON codec using centralized wire validation, `internal/runtime/conn` concrete multiplexed `RuntimeConn` plus server-side handler scaffolding, and `internal/runtime/transport/unixsock` listener/dialer with 0700 parent dir and 0600 socket permissions. Added focused codec/net.Pipe, concurrent invoke, disconnect drain, unix socket, and permission tests; added direct `github.com/coder/websocket` dependency.
- Addressed: Started Agent 2's open M2-01 handoff, promoted the test-local codec direction into production packages without adding production kernel callers, preserved centralized `wire.Decode` validation, and covered 50 concurrent invokes plus disconnect/Close fail-fast with race-clean tests.
- Files: `internal/runtime/codec/codec.go`; `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `internal/runtime/transport/unixsock/unixsock.go`; `internal/runtime/transport/unixsock/unixsock_test.go`; `go.mod`; `go.sum`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Server-side protocol-error response mapping is still minimal and should be hardened before auth/cutover; read/write deadline and backpressure behavior needs another targeted review before M2-05/M2-07 integration; `conn.Handler` shape may need refinement once token auth and daemon lifecycle land.
- Open issues: Continue M2-01 hardening/review before starting M2-02, or explicitly accept the scaffolding and begin `cmd/tabula-runtime` skeleton; no production code should call these packages until the planned M2 integration slice.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: M2-01 protocol-error and deadline hardener
- Work: Reviewed the current M2-01 working set against the issue, amendments, and daemon transport creative doc. Added raw-frame reads in the codec, default write deadlines, and server-side malformed-invoke recovery so recoverable bad `invoke` frames return structured `protocol_error` `InvokeResult` responses while malformed non-invoke frames remain connection-fatal. Added focused regression tests for both paths and verified no production callers outside the new runtime packages/tests.
- Addressed: Mitigated Agent 3's open protocol-error mapping risk and partially addressed read/write deadline behavior by adding a bounded write path in addition to the existing idle read bound.
- Files: `internal/runtime/codec/codec.go`; `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: `conn.Handler` may still need shape changes once M2-05 token auth and runtime daemon lifecycle registration are implemented; backpressure is currently bounded by write deadlines rather than queue-level flow control.
- Open issues: Decide whether the current `conn.Handler`/`Serve` interface is sufficient before M2-05 auth integration; M2-02 runtime binary skeleton is still not started.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: M2-02 runtime daemon skeleton implementer
- Work: Read the M2-02 issue, amendments, daemon transport creative handoff, and actual M2-01 transport files, then added a runnable `tabula-runtime` skeleton: stdlib CLI with default `start` and non-zero `stdio` placeholder, `runtime.toml` loader using singular `[[kernel]]` plus `token_file`, daemon admin-op handler, and unix dial/Hello/reconnect loop with graceful shutdown. Added focused config, handler, dialer, CLI, auth-rejection, and reconnect tests.
- Addressed: Resolved Agent 4's M2-02-not-started handoff and exercised the `conn.Handler`/`Serve` shape through the daemon skeleton; preserved the amendment correction from stale `token` to `token_file`.
- Files: `cmd/tabula-runtime/main.go`; `cmd/tabula-runtime/main_test.go`; `cmd/tabula-runtime/config/config.go`; `cmd/tabula-runtime/config/config_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `cmd/tabula-runtime/dialer/dialer.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: M2-02 still lacks a true subprocess SIGTERM smoke test; kernel-side token generation/Hello enforcement remains future M2-05 work; runtime id is CLI-provided/defaulted until later registry/config work decides whether to load it from config.
- Open issues: Next agents should inspect the new M2-02 lifecycle code, add a practical signal/subprocess smoke if valuable, or proceed to M2-03/M2-05 only after reading those issue files and re-checking amendments.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: M2-02 lifecycle smoke hardener
- Work: Inspected the M2-02 daemon skeleton, dialer lifecycle, unix transport, issue acceptance criteria, amendments, and daemon transport creative handoff. Added a true subprocess SIGTERM smoke test for `tabula-runtime start` that launches the test binary helper as a child process, completes Hello against a fake unix-socket kernel, sends SIGTERM, requires exit 0 within 5s, and verifies the fake kernel observes connection closure.
- Addressed: Closed Agent 5's open M2-02 subprocess/SIGTERM smoke-test gap and strengthened acceptance evidence for graceful shutdown without changing runtime production behavior or introducing any kernel-side execution path.
- Files: `cmd/tabula-runtime/main_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Kernel-side token generation/Hello enforcement and runtime-auth registration remain future M2-05 work; runtime ID is still CLI-provided/defaulted until later registry/config work decides whether it should move into runtime config.
- Open issues: M2-02 is now materially covered; next agents should read `docs/issues/M2-03-*.md` or `docs/issues/M2-05-*.md` plus `AMENDMENTS.md` before continuing with worker spawn policy or kernel-side token handshake enforcement.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 7 — [CONTRIBUTE]
- Role: M2-03 bare worker spawn implementer
- Work: Read the M2-03 issue, amendments, daemon transport creative handoff, worker-protocol docs, and current runtime daemon/policy files. Replaced the `policy/bare` stub with a real runtime-side `os/exec` worker policy: runtime command selection, working-dir entry resolution, passthrough env allowlist plus `TABULA_KERNEL_ID`/`TABULA_TENANT_ID`/`TABULA_TARGET_ID`, WorkerInit timeout, WorkerCall/WorkerResult exchange, warm sequential reuse, cold second-call rejection, cooperative shutdown with TERM/KILL escalation, process groups, exit info, and race-clean liveness tracking. Added subprocess fixture tests for init/call/shutdown, warm/cold behavior, env propagation, timeout kill, and validation.
- Addressed: Started Agent 6's M2-03 handoff by landing the core bare worker spawn/handle layer while preserving the runtime-side-only `os/exec` boundary and avoiding any kernel execution path.
- Files: `cmd/tabula-runtime/policy/policy.go`; `cmd/tabula-runtime/policy/bare/bare.go`; `cmd/tabula-runtime/policy/bare/bare_test.go`; `cmd/tabula-runtime/policy/bare/process_group_unix.go`; `cmd/tabula-runtime/policy/bare/process_group_windows.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: M2-03 is not complete until manifest discovery, worker pool, daemon Invoke/ListCapabilities/Reload integration, cancel-abandon behavior, crash recovery, and daemon-exit worker reaping land; current bare policy uses an allowlisted env passthrough that later tenant-enforcement work must tighten with `TABULA_TENANT_DIR`/`TABULA_KERNEL_ID` checks.
- Open issues: Continue M2-03 by adding `cmd/tabula-runtime/manifest/` and `cmd/tabula-runtime/pool/`, then wire `daemon.Handler` to real capabilities/invoke/reload; M2-05 token generation/auth remains open after or alongside that.
- Quality: Accuracy 4/5, Completeness 3/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: M2-03 manifest/pool/daemon integration implementer
- Work: Read the current BUILD handoff, M2-03 issue, amendments, daemon transport creative doc, and actual policy/daemon/config files. Added runtime-side plugin manifest discovery/store, configurable `plugin_dirs` defaulting to `$TABULA_HOME/plugins`, a warm worker pool keyed by `(kernel_id, tenant_id, target_id)`, daemon `Invoke`/`ListCapabilities`/`Reload`/`Cancel` integration, worker-count reporting, and runtime startup pool lifecycle cleanup.
- Addressed: Resolved most of Agent 7's explicit M2-03 handoff by landing `cmd/tabula-runtime/manifest/`, `cmd/tabula-runtime/pool/`, real daemon capability/invoke/reload routing, M2 cancel-abandon semantics, crash eviction/respawn tests, per-tenant worker separation tests, and daemon-exit worker reaping via `defer workerPool.Close()`.
- Files: `cmd/tabula-runtime/config/config.go`; `cmd/tabula-runtime/config/config_test.go`; `cmd/tabula-runtime/daemon/handler.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `cmd/tabula-runtime/main.go`; `cmd/tabula-runtime/manifest/manifest.go`; `cmd/tabula-runtime/manifest/manifest_test.go`; `cmd/tabula-runtime/pool/pool.go`; `cmd/tabula-runtime/pool/pool_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Manifest discovery intentionally uses a runtime-local manifest parser rather than importing kernel plugin packages, so later M2-04/M2-07 work should re-check schema parity with the migrated plugin SDK; M2 cancel-abandon returns promptly and leaves worker output to be discarded by the abandoned call goroutine, but a full transport-level smoke could still improve confidence before cutover.
- Open issues: M2-05 token generation/auth remains open; consider a focused end-to-end runtime invoke smoke over `conn`/unix transport; later M4 tenant-enforcement work must tighten worker env with `TABULA_TENANT_DIR`/`TABULA_KERNEL_ID` checks.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 9 — [CONTRIBUTE]
- Role: M2-03 transport-level invoke smoke hardener
- Work: Read the current BUILD handoff, M2-03 issue/amendments/creative guidance, and actual daemon/dialer/conn/pool/policy files. Added `TestRunServesManifestBackedInvokeOverUnix`, an end-to-end dialer/unix/RuntimeConn smoke that accepts Hello, lists manifest-backed capabilities, invokes a real Python NDJSON plugin worker through `daemon.Handler` + `pool` + bare policy, verifies tenant ID in both process env and WorkerInit, checks worker count, and reload-evicts the worker.
- Addressed: Closed Agent 8's explicit suggestion to add a focused end-to-end runtime invoke smoke over `conn`/unix transport before M2-05/M2-07; strengthened M2-03 confidence without changing production runtime behavior.
- Files: `cmd/tabula-runtime/dialer/dialer_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Manifest parser/schema parity with the migrated plugin SDK remains a later M2-04/M2-07 review point; M2-05 token generation/auth remains open; full installed-layout smoke still belongs to M2-08 after kernel cutover wiring.
- Open issues: M2-05 kernel-side token generation/Hello enforcement and runtime registration remain open; next agents may start M2-05 after reading its issue/amendments, or do targeted M2-03 schema-parity review if they can compare current plugin authoring without broad drift.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 10 — [CONTRIBUTE]
- Role: M2-05 token auth primitive and handshake hardener
- Work: Read the M2-05 issue, amendments, daemon transport creative handoff, and actual runtime conn/dialer/kernel seams. Added `internal/runtime/auth` with local token generation, 0700/0600 token-file writes, in-memory store, constant-time validation, sanitized `unauthorized` HelloAck responses, and regeneration semantics. Added `conn.ServeAuthenticated`/`ServeAfterHandshake` to enforce first-frame Hello on authenticated server paths while rejecting post-handshake Hello frames, plus unix/auth and dialer token-reread tests.
- Addressed: Started Agent 9's M2-05 open issue by covering kernel-side token generation/store primitives, auth response construction, runtime-side token reread evidence, wrong-token rejection, old-token rejection after regeneration, and no token-material leakage in auth errors.
- Files: `internal/runtime/auth/token.go`; `internal/runtime/auth/token_test.go`; `internal/runtime/conn/conn.go`; `internal/runtime/conn/conn_test.go`; `internal/runtime/transport/unixsock/auth_test.go`; `internal/runtime/transport/unixsock/unixsock_test.go`; `cmd/tabula-runtime/dialer/dialer.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Auth primitives are not yet wired into the real `tabula serve` startup/runtime listener path, so attached-runtime registration and `tabula status --json` readiness still require follow-up; M2-07 managed local runtime child startup remains a later cutover concern.
- Open issues: Finish M2-05 by integrating token issuance/authenticated listener registration into the kernel serve path and attachment read model; continue M2-03 manifest schema-parity review if auth integration is not the next slice.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 11 — [CONTRIBUTE]
- Role: M2-05 kernel serve integration and runtime attachment read-model implementer
- Work: Read the M2-05 issue, amendments, daemon transport creative handoff, Agent 10 auth/conn code, and the real `tabula serve`/kernel seams. Wired `tabula serve` to issue `$TABULA_HOME/run/runtime-token`, open `$TABULA_HOME/run/runtime.sock`, accept authenticated Runtime API Hello connections through `Hub.ServeAuthenticatedRuntime`, and track attached/detached runtime state in a generic kernel `RuntimeRegistry` surfaced through a loopback-only `/internal/snapshot/runtimes` diagnostics endpoint.
- Addressed: Closed Agent 10's open M2-05 gap for real serve-startup token issuance/authenticated listener registration and added attachment read-model evidence without cutting over plugin dispatch or adding a kernel execution fallback.
- Files: `cmd/tabula/main.go`; `internal/kernel/kernel.go`; `internal/kernel/runtime_attach.go`; `internal/kernel/runtime_attach_test.go`; `internal/kernel/runtime_registry.go`; `internal/kernel/snapshot.go`; `internal/runtime/conn/conn.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: The runtime read model currently exists as an internal diagnostics snapshot; the user-facing `tabula status --json` surface belongs to M2-06. Managed local runtime child startup/token-file config wiring remains intentionally deferred to M2-07. Full `go test -race ./internal/kernel ./internal/runtime/... ./cmd/tabula` still fails on pre-existing plugin test/tool-dispatch races (`plugin_bundles_test.go`/`plugin_runtime_test.go`) unrelated to this runtime attachment path.
- Open issues: Implement M2-06 `tabula status --json` on top of the runtime read model; M2-07 must start the managed local runtime child and cut plugin dispatch to Runtime API after companion repo prerequisites; continue targeted manifest schema-parity review before M2-07.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 12 — [CONTRIBUTE]
- Role: M2-06 status CLI implementer
- Work: Read the M2-06 issue, amendments, daemon transport creative handoff, Agent 11 runtime read-model files, and the real `tabula serve`/snapshot code. Added `tabula status` and `tabula status --json`, kernel status/pidfile write-remove lifecycle in `tabula serve`, loopback HTTP snapshot probing for attached runtimes, tenant enumeration from `$TABULA_HOME/state/tenants`, human output, documented exit-code behavior, and focused status snapshot tests.
- Addressed: Closed Agent 11's open M2-06 user-facing status-surface gap by exposing the runtime read model through a composable CLI primitive while preserving M4's additive flat `runtimes[].capabilities` contract and avoiding any plugin dispatch cutover.
- Files: `cmd/tabula/main.go`; `cmd/tabula/status.go`; `cmd/tabula/status_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: `status` currently reports runtime `pid: 0` because the managed local child process is intentionally deferred to M2-07; installed-layout smoke remains M2-08; `status` uses the loopback internal snapshot endpoint plus pid/status files rather than a separate Runtime API introspection op, which is acceptable for M2 but should be rechecked when M4/M6 registry surfaces expand.
- Open issues: M2-07 must add managed local runtime child startup/token-file runtime config wiring and plugin dispatch cutover after M2-04/M2-04b prerequisites; M2-03 manifest schema-parity with the migrated SDK remains a targeted review point; M2-08 must exercise `tabula status --json` through installed layout.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 13 — [CONTRIBUTE]
- Role: M2-03 manifest schema-parity hardener
- Work: Read the M2-03/M2-04 issue files, amendments, daemon transport creative handoff, current runtime manifest/pool/daemon code, and existing kernel manifest parser. Hardened `cmd/tabula-runtime/manifest` so runtime-side plugin discovery preserves advisory hooks, validates/preserves `[requires]`, enforces SemVer-like versions, keeps current python-only plugin runtime parity, and reuses the kernel-style relative-entry path invariant. Updated runtime manifest/daemon/pool/dialer fixtures to include `[requires]` and added schema-drift rejection tests.
- Addressed: Closed Agent 12's targeted M2-03 schema-parity review point for the `tabula` runtime parser; retained M2-07 gating by not starting managed child/plugin dispatch cutover before M2-04/M2-04b companion evidence exists.
- Files: `cmd/tabula-runtime/manifest/manifest.go`; `cmd/tabula-runtime/manifest/manifest_test.go`; `cmd/tabula-runtime/pool/pool_test.go`; `cmd/tabula-runtime/daemon/handler_test.go`; `cmd/tabula-runtime/dialer/dialer_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Runtime manifest parser now intentionally mirrors the current python-only plugin schema; if M2-04 changes `tabula-bundles` plugin schema in a companion repo, that repo must provide explicit validation evidence before M2-07 rather than relying on this in-repo parser alone.
- Open issues: M2-07 still owns managed local runtime child startup/token-file runtime config wiring and plugin dispatch cutover after M2-04/M2-04b prerequisites; `status` runtime `pid` remains `0` until managed child tracking exists; M2-08 must exercise `tabula status --json` through installed layout.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 14 — [CONTRIBUTE]
- Role: M2-06/M2-07 status PID readiness hardener
- Work: Read the active handoff, latest M2 build log, M2-06/M2-07 issue text, amendments, daemon transport creative handoff, and actual status/runtime-attachment files. Propagated an optional runtime PID from `RuntimeAttachOptions` into the generic kernel runtime registry, `/internal/snapshot/runtimes`, and `tabula status --json`, with regression tests for both the kernel snapshot and status fetch path.
- Addressed: Partially mitigated Agent 13's `status` runtime `pid` gap by making the status/read-model path capable of reporting a real managed child PID as soon as M2-07 supplies it; retained the M2-07 gate by not starting managed child startup or plugin dispatch cutover before M2-04/M2-04b evidence exists.
- Files: `internal/kernel/runtime_attach.go`; `internal/kernel/runtime_registry.go`; `internal/kernel/snapshot.go`; `internal/kernel/runtime_attach_test.go`; `cmd/tabula/status.go`; `cmd/tabula/status_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Current `tabula serve` still passes no runtime PID, so `tabula status --json` will continue to show `pid: 0` until M2-07 wires managed child process ownership; the first focused test run failed due a new test fixture omitting capabilities, then passed after fixture correction.
- Open issues: M2-07 still owns managed local runtime child startup/token-file runtime config wiring and plugin dispatch cutover after M2-04/M2-04b prerequisites; M2-08 must exercise `tabula status --json` through installed layout; M2-04/M2-04b companion repos must provide plugin worker-protocol/schema validation before cutover.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 15 — [CONTRIBUTE]
- Role: M2 runtime config strictness hardener
- Work: Read the active handoff, M2-07/M2-04 issue text, amendments, daemon transport creative handoff, and actual runtime config/status/attachment files. Hardened `cmd/tabula-runtime/config.Load` to reject undecoded TOML fields instead of silently ignoring stale or speculative config, and added focused tests for stale `token`, plural `[[kernels]]`, and top-level `runtime_id` fields.
- Addressed: Mitigated a pre-M2-07 config drift risk while preserving the M2-07 gate: managed child startup/plugin dispatch cutover remains blocked on M2-04/M2-04b, but runtime-side config now refuses aliases before that wiring depends on it.
- Files: `cmd/tabula-runtime/config/config.go`; `cmd/tabula-runtime/config/config_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: Strict config rejection may surface previously ignored local experimental runtime.toml keys, but that matches the program's no-alias/no-legacy rule and amendment C4/M3 expectations.
- Open issues: M2-07 still owns managed local runtime child startup/token-file runtime config generation and plugin dispatch cutover after M2-04/M2-04b evidence; M2-08 must exercise `tabula status --json` through installed layout.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 16 — [CONTRIBUTE]
- Role: Final pre-M2-07 config-registry drift hardener
- Work: Read the active handoff, latest BUILD log, M2-07/M2-04 issue text, amendments, daemon transport creative handoff, and actual runtime config/status/attachment files. Added an explicit runtime-config regression proving the runtime-side loader rejects the kernel-side `[[runtime]]` registry shape, so M2-07 managed-child config generation cannot accidentally copy later kernel `global.toml` examples into `runtime.toml`.
- Addressed: Strengthened Agent 15's strict config guard by covering the remaining C4 split-risk between runtime-side `[[kernel]]` and kernel-side `[[runtime]]`; preserved the M2-07 gate by not starting managed child/plugin dispatch cutover before M2-04/M2-04b evidence exists.
- Files: `cmd/tabula-runtime/config/config_test.go`; `memory-bank/tasks.md`; `memory-bank/progress.md`; `memory-bank/activeContext.md`
- Risks: No new code-path risk found; unknown/unsupported runtime config fields now intentionally fail fast, which is expected under the no-alias/no-legacy rule.
- Open issues: M2-07 still owns managed local runtime child startup/token-file runtime config generation and plugin dispatch cutover after M2-04/M2-04b evidence; M2-08 must exercise `tabula status --json` through installed layout.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### SECURITY Phase — Attempt 1 (2026-05-02)
- Verdict: PASSED
- Blocking findings: 0
- Warning findings: 1
- Degraded checks: dependency review for Go module delta (no allowed Go vulnerability audit command; `.opencode` npm audit findings are outside product delta)
- Report: `memory-bank/security/security-execute-remote-runtime-program.md`

### 2026-05-02 — REFLECT progress
- Timestamp: 2026-05-02
- Requirement: Complete Level 4 strategic reflection for the active remote-runtime implementation scope and verify it against plan, BUILD evidence, and SECURITY findings.
- Approach: Validated task metadata, read the active handoff/progress/security report and relevant creative/source evidence, then created `memory-bank/reflection/reflection-execute-remote-runtime-program.md` with outcome, process, architecture, implementation, testing, security, residual-risk, and follow-up analysis.
- Files modified: `memory-bank/reflection/reflection-execute-remote-runtime-program.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/tasks.md`.
- Verification notes: Reflection confirms M1 and M2 pre-cutover foundation are complete for the current BUILD scope; SECURITY passed on attempt 1 with no blockers; M2-07/M2-08 and M3-M6 remain follow-up program work. Structured lesson append/rotation is deferred pending L4 second-opinion approval per CR-B.

### ARCHIVE Phase — DONE (2026-05-02)
- ✅ Archive artifact created: `memory-bank/archive/archive-execute-remote-runtime-program.md`
- ✅ tasks.md reset to "No active tasks"
- ✅ activeContext.md reset to "No active context"
- ✅ progress.md preserved as historical log
- ✅ Creative and reflection artifacts preserved as permanent references
