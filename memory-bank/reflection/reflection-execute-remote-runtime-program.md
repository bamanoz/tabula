# Strategic Reflection: Remote Runtime Program Foundation
Date: 2026-05-02

## System Summary
The completed BUILD scope established the Tabula remote-runtime foundation for the larger M1 → M6 program. It fully implements the M1 behavior-free Runtime API contract spine and advances M2 through codec/unix transport, `tabula-runtime` daemon/config/dialer, runtime-side plugin manifest discovery, worker pool and bare worker policy, bearer-token runtime attachment, kernel runtime read model, and `tabula status --json` readiness surfaces.

This reflection treats the completed work as the approved BUILD scope recorded in `memory-bank/progress.md` Agents 1-16, not as completion of the entire M1 → M6 program. The full remote-runtime program remains open beyond this phase: M2-07 plugin dispatch cutover, M2-08 installed-layout smoke, M3 skill unification, M4 tenancy enforcement, M5 workspace decomposition, and M6 remote backends/operator docs are follow-up program work.

## 1. Overall Outcome
- **Scope coverage**: Strong coverage for M1 and the pre-cutover M2 foundation. Evidence includes `internal/runtime/wire/` typed frames and canonical error roster, `internal/runtime/codec/` WebSocket JSON codec, `internal/runtime/transport/unixsock/`, `cmd/tabula-runtime/**`, `internal/runtime/auth/`, kernel runtime attachment/read model files, and `cmd/tabula/status.go`.
- **Requirement coverage assessment**:
  - M1-01 → M1-06 are complete in `tabula`: wire/worker/interface/mock/contract-test scaffolding with no production dispatch change.
  - M2-01, M2-02, M2-03, M2-05, and M2-06 are materially implemented or hardened in the `tabula` repo.
  - M2-04/M2-04b cross-repo companion migrations, M2-07 atomic plugin dispatch cutover/deletion, and M2-08 installed-layout runtime smoke remain unbuilt.
  - M3-M6 remain future program slices.
- **Quality attribute achievement**:
  - **Architecture separation**: Runtime contract and process hosting moved into `internal/runtime/**` and `cmd/tabula-runtime/**`; kernel additions are read-model/auth/status surfaces rather than tool-hosting replacement paths.
  - **Security posture**: SECURITY passed on attempt 1 with no blocking findings; local token generation uses random `rtk_` tokens and 0700/0600 file modes; unauthorized responses and logs are sanitized.
  - **Config correctness**: Runtime config accepts singular `[[kernel]]` and `token_file`, rejects stale aliases (`token`, `[[kernels]]`, top-level `runtime_id`, kernel-side `[[runtime]]`).
  - **Runtime observability**: `tabula status --json` can report kernel state, tenants, runtime attachment, capabilities, and a future managed-child PID path.

## 2. Process Effectiveness
- **Metrics**:
  - PLAN contributors: 6 pipeline agents.
  - CREATIVE contributors: 1 design handoff pass producing 5 required creative docs.
  - BUILD contributors: 16 pipeline agents.
  - SECURITY attempts: 1; verdict `PASSED`.
  - Blocking security findings: 0; warning findings: 1 degraded dependency-review warning.
  - Rework count: targeted hardening passes rather than rollback/re-entry. Examples: protocol-error handling (Agent 4), subprocess signal smoke (Agent 6), manifest schema parity (Agent 13), status PID readiness (Agent 14), config strictness/drift guards (Agents 15-16).
  - Timeline: all recorded phase progress is dated 2026-05-02; exact wall-clock durations were not recorded in Memory Bank.
- **Phase-by-phase analysis**:
  - **VAN**: Effective enough to classify Level 4 / implement / deep and establish required metadata. Metadata was valid for REFLECT.
  - **PLAN**: Strong. It normalized stale issue text against `AMENDMENTS.md`, resolved the `default` tenant contradiction, created a mechanical M1-M6 execution checklist, and identified cross-repo/deletion gates.
  - **CREATIVE**: Strong. It converted plan risk into five concrete design docs covering Runtime API, daemon transport, tenant/config registry, workspace decomposition, and remote-backend security.
  - **BUILD**: Effective for the selected slice. Agents preserved issue gates and did not prematurely execute M2-07 before M2-04/M2-04b companion evidence.
  - **SECURITY**: Effective. It audited the actual source delta and artifacts, found no blockers, and correctly scoped future M4/M6 security work as non-blocking for the current M1/M2 foundation.

## 3. Architectural Planning Review
- **Architectural principles followed**:
  - Runtime API is transport-independent (`internal/runtime/wire/types.go`, `internal/runtime/codec/codec.go`).
  - Runtime daemon owns worker execution (`cmd/tabula-runtime/policy/bare/bare.go`), while kernel additions are listener/auth/read-model/status surfaces.
  - No config aliases were added; strict runtime config rejects stale shapes.
  - Atomic deletion gates were respected: kernel stdio plugin transport and skill process manager were not deleted before prerequisites.
- **Alternatives evaluated**: CREATIVE docs explicitly rejected generic maps, transport-coupled message structs, local-only kernel-dial topology, and embedded fallback execution paths. The selected options align with ADR 0001.
- **Right decisions made**:
  - Typed envelopes and centralized validation prevented stale wire names from leaking into production code.
  - Kernel-listens/runtime-dials topology was implemented for local unix attachment and remains compatible with later WSS/SSH.
  - Runtime config strictness before M2-07 reduces cutover risk.
- **Architecture validation outcomes**:
  - `grep` evidence during reflection found stale wire/plugin-internal error strings only in negative tests/comments, not production runtime code.
  - `grep` found `exec.CommandContext` still in `internal/kernel/plugin/runtime.go`, which is expected before M2-07 and must not be misread as a regression for this pre-cutover scope.
  - `grep` found the new Go dependency `github.com/coder/websocket` in runtime/transport paths and tests, matching the SECURITY dependency delta.

## 4. Creative Phase Review
- **Required design phases executed**: all five required docs exist:
  - `memory-bank/creative/creative-runtime-api-contract.md`
  - `memory-bank/creative/creative-runtime-daemon-transport.md`
  - `memory-bank/creative/creative-tenant-config-registry.md`
  - `memory-bank/creative/creative-workspace-decomposition.md`
  - `memory-bank/creative/creative-remote-backends-security.md`
- **Quality of design decisions**: High. The docs preserve the accepted ADR, address amendment drift, define no-legacy boundaries, and include BUILD/SECURITY handoff checklists.
- **Design-to-implementation fidelity**:
  - High for M1 and M2 pre-cutover surfaces: typed frames, canonical errors, token handshake, unix transport, reconnect token reread, runtime config `[[kernel]]`/`token_file`, worker pool keying, and status JSON were implemented consistently.
  - Deferred by design for M4/M5/M6 surfaces: tenant whitelist, workspace decomposition, mTLS, SSH, token revocation, service install, and operator docs remain future work.

## 5. Implementation Review
- **Phase completion success**: Current BUILD scope completed and SECURITY passed without BUILD re-entry.
- **Milestone checkpoint effectiveness**:
  - M1 checkpoint is complete and well-tested, including contract and race tests.
  - M2 pre-cutover checkpoint is strong but intentionally incomplete: it stops before companion repo migrations and atomic plugin dispatch cutover.
- **Integration challenges**:
  - Kernel touch-points had to be introduced before the documented M2-07 cutover to support token issuance, runtime attachment, and status. This was kept generic and scoped.
  - Manifest schema parity required a dedicated hardening pass because runtime manifest parsing was implemented locally rather than importing kernel plugin packages.
  - Status PID reporting required a readiness path before the managed local child exists.
- **Performance outcomes**:
  - Runtime codec has a 1 MiB frame limit and bounded 60s read/write operations.
  - `conn` supports concurrent invoke routing, disconnect fail-fast, and pending-call drain.
  - Worker pool supports warm worker reuse and per-tenant/per-target keying for M2 plugin behavior.

## 6. Testing Review
- **Test coverage adequacy**:
  - Strong focused Go coverage for M1/M2 foundation: runtime wire/codec/conn/transport/auth, runtime daemon/config/dialer/manifest/pool/policy, kernel runtime attachment/read model, and status CLI.
  - Race tests were run repeatedly for new runtime packages and selected command/kernel paths.
  - End-to-end pre-cutover smoke exists over unix/dialer/RuntimeConn invoking a real Python NDJSON plugin worker.
- **Issues found at each stage**:
  - Agent 4 found and improved protocol-error response mapping and write deadlines.
  - Agent 6 closed a missing subprocess SIGTERM smoke gap.
  - Agent 13 closed manifest schema-parity risk.
  - Agent 14 fixed status PID path readiness.
  - Agent 15/16 closed runtime config drift risks.
  - SECURITY found no blocking security issues and one degraded dependency-review warning.
- **Testing process improvements needed**:
  - M2-08 must add installed-layout/testbed coverage for the runtime/status surfaces; unit tests are not enough for install/runtime behavior.
  - The SECURITY protocol should permit Go vulnerability tooling (for example, `govulncheck`) so Go module deltas are not degraded.
  - Pre-existing full-race failures in plugin/tool-dispatch tests should be triaged before later deletion/cutover work increases concurrency surface area.

## 7. Successes with Evidence
1. **Runtime API contract landed without stale aliases** — Evidence: `internal/runtime/wire/types.go` defines the canonical error roster only; negative tests reject `tenant_denied`, `target_not_authorized`, `fs_outside_root`, and `exec_denied` as wire errors.
2. **Local runtime auth and attachment became real, not only design** — Evidence: `internal/runtime/auth/token.go` generates 32-byte `rtk_` tokens, writes `$TABULA_HOME/run/runtime-token` with `0600` under `0700`, and `cmd/tabula/main.go` opens `$TABULA_HOME/run/runtime.sock` for authenticated runtime connections.
3. **Pre-cutover runtime daemon can discover and invoke plugin workers through the new path** — Evidence: progress Agent 9 recorded an end-to-end unix/dialer/RuntimeConn smoke invoking a Python NDJSON plugin worker through `daemon.Handler`, `pool`, and `bare` policy.
4. **Config drift was proactively blocked** — Evidence: `cmd/tabula-runtime/config/config.go` rejects undecoded TOML fields; tests reject stale `token`, plural `[[kernels]]`, top-level `runtime_id`, and kernel-side `[[runtime]]` in runtime config.
5. **Security review passed without re-entry** — Evidence: `memory-bank/security/security-execute-remote-runtime-program.md` verdict `PASSED`, 0 blocking findings, with one warning limited to degraded Go dependency review.

## 8. Challenges with Solutions
1. **Challenge: source issue/docs drift around errors and config shapes** → **Solution**: PLAN normalized against `AMENDMENTS.md`; CREATIVE encoded canonical shapes; BUILD added strict validation/tests → **Outcome**: production runtime code uses canonical errors and `[[kernel]]`/`token_file` only.
2. **Challenge: extracting runtime infrastructure without premature kernel cutover** → **Solution**: implement M1/M2 foundation behind generic runtime packages, attachment read model, and status surfaces while preserving M2-07 as the cutover gate → **Outcome**: runtime foundation is testable, but kernel stdio plugin path remains until prerequisites are satisfied.
3. **Challenge: runtime manifest parser could drift from current plugin authoring schema** → **Solution**: Agent 13 hardened the parser to preserve hooks/requires, enforce SemVer-like versions, and validate relative entries → **Outcome**: current `tabula` schema parity is covered, while companion repos still need validation before M2-07.
4. **Challenge: dependency security review tooling mismatch** → **Solution**: SECURITY manually identified the Go dependency delta and documented the limitation → **Outcome**: no blocker, but a process follow-up is needed to add allowed Go vulnerability review.

## 9. Strategic Technical Insights
- Typed Runtime API envelopes with centralized validation are a durable guardrail for multi-agent implementation: they prevented stale issue names and plugin-internal errors from becoming wire API surface.
- For atomic architecture migrations, stopping before a deletion gate can be the correct outcome when companion repos are not ready; preserving the gate is safer than forcing partial cutover.
- Runtime config should fail closed on unknown fields before a managed-child cutover; strict config parsing converts documentation drift into test failures instead of runtime ambiguity.
- Read-model/status surfaces can be introduced before full dispatch cutover if they are generic, loopback-guarded, and explicitly marked as readiness/diagnostic surfaces rather than fallback execution paths.

## 10. Process Improvement Insights
- Milestone evidence packages should be required at every closure point; the progress log was useful, but future M2-07/M2-08 handoffs need explicit companion repo SHA/PR evidence and installed-layout command output.
- SECURITY checklists must align with the implementation language. A Go module delta should have an allowed Go vulnerability-review command to avoid degraded dependency evidence.
- For long multi-milestone programs, phase status should distinguish “current approved slice complete” from “entire program complete” to avoid overclaiming when a task title spans M1 → M6.
- Targeted hardening agents were effective because each read real files and closed a named risk; this pattern should continue for cutover slices.

## 11. Business Impact
- **Value delivered**: Tabula now has a concrete, tested runtime-daemon foundation that reduces the technical risk of remote execution and multi-backend work. Operators/developers are not yet receiving the full remote-runtime product, but the core contract, daemon, auth, worker, and status primitives are in place.
- **Business metrics affected**:
  - Reduced architecture risk for remote execution program.
  - Improved security posture for runtime attachment foundations.
  - Improved future delivery predictability through explicit cutover gates and config drift tests.
  - No direct end-user remote backend adoption yet; M6 remains unbuilt.

## 12. Strategic Action Items
- **Priority 1**: Complete M2-04/M2-04b companion migrations, then execute M2-07 as an atomic managed-child/plugin dispatch cutover with SHA/PR evidence and no fallback.
- **Priority 2**: Add M2-08 installed-layout/testbed coverage for `tabula-runtime`, runtime token/socket layout, `tabula status --json`, and managed local child PID behavior.
- **Priority 3**: Prepare the next milestone wave (M3-M6) with the same evidence discipline: skill cutover/deletion, tenant registry/enforcement, workspace decomposition, and WSS/mTLS/SSH/service docs.

## 13. Follow-up Tasks
<!-- FOLLOW_UP_START -->
- [ ] Complete M2-04 and M2-04b companion worker-protocol migrations and capture repo SHA/PR evidence before cutover | Priority: high | Source: reflection-execute-remote-runtime-program
- [ ] Execute M2-07 managed local runtime child startup, runtime config generation, plugin dispatch cutover, and kernel stdio plugin transport deletion with no fallback | Priority: high | Source: reflection-execute-remote-runtime-program
- [ ] Add and run M2-08 installed-layout runtime/status smoke coverage, including `tabula status --json` and real installed `tabula-runtime` execution | Priority: high | Source: reflection-execute-remote-runtime-program
- [ ] Add an approved Go dependency vulnerability-review path to SECURITY so Go module deltas are not degraded | Priority: medium | Source: reflection-execute-remote-runtime-program
- [ ] Triage pre-existing plugin/tool-dispatch race-test failures before later runtime cutover broadens concurrency risk | Priority: medium | Source: reflection-execute-remote-runtime-program
- [ ] Continue M3-M6 implementation slices for skill runtime cutover, tenant registry/enforcement, workspace decomposition, and remote WSS/mTLS/SSH/service closure | Priority: high | Source: reflection-execute-remote-runtime-program
<!-- FOLLOW_UP_END -->

## Reflection Integration Notes
- `memory-bank/systemPatterns.md` was read for lesson context and currently has no structured `## Lessons Learned` block.
- Per L4 second-opinion protocol, structured lesson append and lessons rotation are deferred until the router records `REFLECT Second Opinion: APPROVED`.
- Archive readiness: this reflection is self-contained and references the SECURITY report, progress evidence, creative docs, and residual program gaps needed by ARCHIVE.
