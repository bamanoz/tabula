# Strategic Reflection: Tabula Skill/Plugin Architecture
Date: 2026-04-27

## System Summary
The implementation split Tabula extension architecture into two explicit component types: **skills** as per-call tool providers and **plugins** as long-lived supervised processes. In this repository, the work reduced the kernel LLM-visible builtin tool surface, added a Go `PluginRuntime` with stdio NDJSON protocol support, added plugin manifest parsing and supervision, introduced mixed skill/plugin distro tooling, added a Python reference plugin/SDK, and refreshed the authoring and architecture documentation.

The original design doc (`docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`) also describes migrations of hooks, MCP, drivers, gateways, and bundle contents that live in external repositories (`tabula-bundles`, `tabula-distrib`). PLAN explicitly scoped those migrations out of this repo and treated this repo as the kernel/runtime/distro-tooling/documentation foundation that external component migrations can build on.

## 1. Overall Outcome
- **Implementation status**: Successful for the in-repo architecture foundation. BUILD and SECURITY are complete; SECURITY passed with warnings. This REFLECT revision addresses the L4 second-opinion evidence/security-integration requests; archive should wait for the second-opinion recheck to approve the revised primary reflection.
- **Requirement coverage against design scope**:
  - Kernel cleanup: completed at the live dispatch level. `DefaultKernelTools` is empty; `ToolService` dispatches through dynamic skill/plugin dispatch; `SkillExec` replaced the old `RunSkillTool` path. Legacy constants remain only as deprecated/test/dead-code references.
  - Two-tier supervision: implemented for plugin processes through process-group setup/termination, supervisor restart/backoff, ProcessSupervisor bookkeeping, diagnostics, and reference-plugin child cleanup smoke coverage.
  - PluginRuntime/protocol: implemented with `internal/kernel/plugin/` package, manifest parser, NDJSON reader/writer, runtime spawn/register flow, `register_request`, `register`, `tool_call`, `tool_result`, `event`, `event_reply`, `send`, `log`, `update_tools`, and `shutdown` semantics.
  - Plugin API/reference: implemented as a minimal Python SDK in `examples/plugin-sdk-python/` plus `examples/plugin-hello/` live tests.
  - Manifests/bundles: implemented `plugin.toml` validation and mixed skill/plugin `bundle.toml` support in `tools/tabula-distro`, including lock v2 and v1 migrate-on-load.
  - Docs: completed the main in-repo docs (`PLUGIN_AUTHORING.md`, `SKILL_AUTHORING.md`, `ARCHITECTURE.md`, `DISTROS.md`, `distro-config.md`, `PHILOSOPHY.md`), with non-plan docs cleaned of direct `_pylib`/`_tslib`/`requires-kernel-tools` references.
- **Intent alignment (`implement`)**: The task delivered a full happy-path integration into existing kernel, boot, installer, examples, and docs. Deferred work is mostly external repo migration or explicitly gated cleanup, not a missing in-repo core path.
- **Quality attributes achieved**:
  - Surface minimization: kernel no longer exposes built-in LLM tool catalog by default.
  - Composability: skills and plugins share a unified dispatch table while preserving distinct lifecycles.
  - Migration safety: boot `tools` fallback and lock v1 migrate-on-load reduce user breakage.
  - Observability: `SnapshotPlugins()` exposes plugin lifecycle state.
  - Supervision resilience: restart/backoff and PG termination are covered by unit/live tests.

## 2. Process Effectiveness
- **Metrics**:
  - Active dates: 2026-04-26 through 2026-04-27.
  - PLAN: 10 contributing agents plus decline consensus confirming completeness.
  - CREATIVE: 4 frozen design documents with explicit decision table in `tasks.md`.
  - BUILD: 12 major contribution passes plus final decline consensus; rework was contained to test-coverage repair, supervisor race stabilization, and documentation cleanup.
  - SECURITY: 1 attempt; verdict `PASSED`; 0 blocking findings; 4 warnings; 2 degraded dependency-audit checks.
- **Phase-by-phase analysis**:
  - **VAN**: Effective. Correctly classified the task as Level 4 / Intent `implement` / Category `deep`, which forced architectural planning and security review instead of a simple code pass.
  - **PLAN**: Very effective. It clarified repository boundaries early: kernel/runtime/distro/docs are in this repo, while component migrations live externally. This prevented BUILD from chasing unavailable `tabula-bundles` paths.
  - **CREATIVE**: Highly effective. The frozen decisions resolved protocol framing, config delivery, hook integration, dispatch unification, crash recovery, manifest schemas, lock migration, and SDK packaging before implementation began.
  - **BUILD**: Effective and incremental. The work landed in stable slices: kernel cleanup, HookSubscriber refactor, plugin handle/registry/runtime/supervisor, diagnostics, send routing, reference plugin, distro tooling, and docs. Each slice had scoped validation.
  - **SECURITY**: Effective as a final gate. It identified no blockers and surfaced residual operational risks that should be carried into archive/follow-up planning.

## 3. Architectural Planning Review
- **Architectural principles followed**:
  - Minimal kernel: LLM-visible builtin tools were removed from live dispatch.
  - Clear lifecycle separation: skills stay per-call; plugins are long-lived supervised processes.
  - No capabilities-based dispatch: component kind remains determined by manifest filename, consistent with the design doc.
  - Single dispatch source: `Hub.toolExec` became a source-tagged dispatch map for both skill and plugin tools.
- **Alternatives evaluated properly**:
  - Hook integration rejected pseudo-clients and separate dispatchers in favor of a narrow `HookSubscriber` interface.
  - Protocol framing chose NDJSON over Content-Length for debuggability and simplicity.
  - Lock migration chose migrate-on-load instead of a hard break.
  - SDK packaging chose offline-friendly wheel/tarball paths for future bundles work.
- **Right architectural decisions**:
  - The `HookSubscriber` interface avoided conflating clients and plugins while preserving hook-engine behavior.
  - The builder-style `Hub.RegisterPlugin`/`LoadPlugins` avoided destabilizing `NewHub` callsites.
  - Keeping `CanSpawn`/spawn-token code as dead code was a pragmatic migration bridge until the external subagent plugin restores those invariants.
- **Architecture validation outcomes**:
  - Tests in BUILD logs repeatedly passed `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s`, `go vet ./...`, and `go build ./...`.
  - Distro tooling passed 45 unittest cases and `compileall` after mixed plugin/skill installer support landed.
  - The live reference plugin exercised real spawn/register/tool/event/send/snapshot/shutdown behavior.

## 4. Creative Phase Review
- **Required design phases executed**: Yes. The four creative docs covered protocol, runtime architecture, manifest schemas, and SDK/distro packaging.
- **Quality of design decisions**: Strong. The docs converted PLAN risks into concrete decisions with rationale, alternatives, acceptance criteria, and rubric notes.
- **Design-to-implementation fidelity**:
  - High for kernel runtime, plugin protocol, supervision, manifest validation, lock v2, and documentation.
  - Partial by design for external bundle migrations, final packaged SDK relocation, and subagent plugin spawn-token ownership.
  - The implementation intentionally diverged from hard deletion by retaining deprecated constants and skipped spawn tests per D1.11(b), which was a CREATIVE-approved migration safety choice.

## 5. Implementation Review
- **Phase completion success**:
  - Phase 1 kernel cleanup: live builtin dispatch removed; dynamic skill tests repaired.
  - Phase 2 PluginRuntime: handle/registry/runtime/supervisor/Hub integration/snapshot/send routing completed.
  - Phase 3 reference plugin: Python SDK and `plugin-hello` live smoke completed, including child cleanup evidence.
  - Phase 4 distro tooling: mixed components, standalone plugins, lock v2, update targeting completed.
  - Phase 7 docs: authoring, architecture, distro, config, and philosophy docs updated.
- **Milestone checkpoint effectiveness**: The handoff logs were useful: later agents repeatedly narrowed open issues until the final BUILD agents declined additional work as unsafe or out-of-scope.
- **Integration challenges**:
  - Hook subscribers needed a small interface boundary to avoid plugin/client lifecycle conflation.
  - Plugin supervision needed restart/backoff state separate from live registry handles.
  - Test migration required replacing deleted builtin behavior with dynamic skill fixtures rather than simply skipping coverage.
  - Distro tooling needed to support mixed components without reviving the old `_`-prefix support-dir convention.
- **Performance outcomes**: No quantitative runtime performance benchmark was collected. Architectural overhead is bounded by per-tool deadlines, line size caps, restart windows, and existing hook timeout behavior. Future performance work should focus on plugin-heavy boot and MCP dynamic tool discovery.
- **Behavioral equivalence**:
  - Existing client wire protocol remained separate from plugin protocol.
  - Existing hook engine behavior was preserved via the `HookSubscriber` abstraction and regression tests.
  - Existing distro locks migrate instead of hard failing.
  - Process-spawn behavior was intentionally not preserved as an LLM-visible kernel tool; it is a designed breaking change and now a plugin-side responsibility.

## 6. Testing Review
- **Test coverage adequacy**: Adequate for the in-repo foundation. Evidence includes kernel/plugin/cmd Go tests, distro Python unittest suite, compile checks, go vet/build, and live reference plugin tests.
- **Issues found at each stage**:
  - Early BUILD left too many skipped builtin tests; later passes repaired dynamic skill hook coverage and narrowed remaining skips to D1.11(b) spawn-token/MaxChildren dead-code tests.
  - A fake-runtime race in a supervisor restart test was stabilized during reference-plugin hardening.
  - Distro update targeting needed a follow-up after mixed plugin/component lock entries landed.
  - SECURITY found warnings but no blockers.
- **Testing process improvements needed**:
  - Add release CI dependency-audit tooling for Go, Python SDK artifacts, and any JS/TS lockfiles that ship with bundles.
  - Convert the remaining D1.11(b) skipped spawn tests into external subagent-plugin integration tests when that plugin lands.
  - Add a final acceptance checklist that maps creative acceptance criteria to test names, reducing reflection-time evidence reconstruction.
  - Consider explicit tests for plugin diagnostics endpoint exposure assumptions if server binding/auth behavior changes.

### Security Review Integration
- **QA report status**: no `memory-bank/qa/qa-skill-plugin-architecture.md` report exists because the Level 4 workflow used SECURITY as the post-BUILD gate and `memory-bank/tasks.md` marks `QA: SKIPPED`. Runtime validation evidence is therefore captured through BUILD tests/live smoke logs and the SECURITY review rather than a separate QA artifact.
- **SECURITY verdict**: `PASSED` on attempt `1 / 3` for Category `deep` (`memory-bank/security/security-skill-plugin-architecture.md`). The security review found **0 blocking findings**, **4 warning findings**, and **2 degraded dependency-audit checks**. No BUILD reopen was required.
- **Warning 1 — unauthenticated plugin diagnostics endpoint**: `/internal/snapshot/plugins` exposes plugin ids, PID, restart count, last error, registered tools, and subscriptions if the server is network-exposed. Evidence: `cmd/tabula/main.go:66-73` registers the GET endpoint, and `internal/kernel/snapshot.go:75-145` serializes plugin diagnostics. Follow-up coverage: Section 13 item for authenticating or documenting strict locality for `GET /internal/snapshot/plugins`.
- **Warning 2 — degraded dependency audit tooling**: `npm audit --json` degraded with `ENOLOCK` because no npm lockfile exists, and `pip-audit` was unavailable. Evidence: security report checklist item 5 and degraded checks lines 34-36. Follow-up coverage: Section 13 item for release CI audits across Go, Python SDK artifacts, and JS/TS bundle lockfiles.
- **Warning 3 — plugin/boot stderr logging**: no direct secret logging was found, but plugin stderr and boot stderr can leak secrets if component authors print them. Evidence: security report checklist item 6 and warning finding. Follow-up coverage: Section 13 item for stderr/log redaction guidance and optional runtime redaction.
- **Warning 4 — stale legacy builtin metadata**: `cmd/tabula/kernel.tools.json` and `filterKernelTools` still contain legacy `shell_exec` / `process_*` metadata, even though runtime dispatch rejects those names unless registered as dynamic skill/plugin tools. Evidence: `cmd/tabula/main.go:686-713`, `cmd/tabula/kernel.tools.json`, and the security report configuration-drift warning. Follow-up coverage: Section 13 item for cleaning up or quarantining stale legacy builtin metadata.
- **Related auth/supervision residual risk**: the SECURITY AuthN/AuthZ checklist also notes the intentional D1.11(b) spawn-token/MaxChildren dead-code bridge pending subagent plugin migration. This is not a blocking issue for the in-repo implementation, but it remains a high-priority follow-up to implement plugin-side coverage and remove the dead code/skipped tests.

## 7. Successes with Evidence
1. **Kernel LLM-tool surface was reduced without losing dynamic dispatch coverage** — Evidence: `internal/kernel/protocol.go` now has empty `DefaultKernelTools`; `tool_service.go` uses `handleDynamicTool`; BUILD logs D1.13/D1.14 show synthetic skill dispatch tests replacing legacy `shell_exec` coverage.
2. **PluginRuntime became a real supervised runtime, not only a static interface** — Evidence: `internal/kernel/plugin/runtime.go`, `supervisor.go`, `plugin_runtime.go`, `snapshot.go`, `plugin_tools.go`, and `plugin_live_test.go` are all referenced in BUILD logs; `examples/plugin-hello` live E2E validates runtime behavior.
3. **Distro tooling now supports mixed skill/plugin components with backward-compatible locks** — Evidence: Phase 4 logs report `[bundle].components`, standalone `[[plugins]]`, lock v2 with `plugins`, v1 migrate-on-load, update targeting, and 45 passing distro tests.
4. **Security gate passed on the first attempt** — Evidence: `memory-bank/security/security-skill-plugin-architecture.md` reports `Verdict: PASSED`, 0 blocking findings, and 4 warnings.

## 8. Challenges with Solutions
1. **Challenge: hook dispatch was client-centric** → **Solution applied**: introduced `HookSubscriber` and kept one hook-engine path → **Evidence**: `internal/kernel/hook_subscriber.go` defines the lifecycle-neutral interface; `internal/kernel/handle_hooksub.go` adapts `plugin.Handle` without an import cycle; `internal/kernel/helpers.go:24-39` merges WebSocket clients and plugin handles; `internal/kernel/hooks.go:57` rebuilds the hook index from that union; `internal/kernel/plugin_tools_test.go` covers plugin hook subscriber integration. → **Outcome**: plugins can receive events without becoming pseudo-clients or duplicating dispatcher logic.
2. **Challenge: removing kernel `process_spawn` creates a spawn-token/MaxChildren invariant vacuum** → **Solution applied**: selected D1.11(b), keeping spawn-token/CanSpawn as dead code with TODOs and skipped regression tests until subagent plugin GA → **Evidence**: `memory-bank/tasks.md` D1.11/D1.14 document the chosen bridge and remaining skipped spawn tests; code-level markers exist in `internal/kernel/policy.go::CanSpawn`, `internal/kernel/kernel.go` `MaxSpawnDepth`/`MaxChildren`, `internal/kernel/connect.go::generateSpawnToken`, `internal/kernel/spawn_token_store.go`, and `internal/kernel/skip_helpers_test.go`; Section 13 tracks the required subagent-plugin follow-up. → **Outcome**: in-repo live kernel surface is clean while the migration risk remains explicit, test-marked, and bounded to the external subagent-plugin milestone.
3. **Challenge: external component migrations were named in the design but unavailable in this repo** → **Solution applied**: PLAN clarified repo boundaries and BUILD focused on runtime/tooling/docs foundations → **Evidence**: `memory-bank/tasks.md` lines 62-75 explicitly scope `tabula-bundles` and `tabula-distrib/ouroboros` work out of this repository; the source design remains `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`; Section 13 carries external migrations for hooks, MCP, drivers, gateways, and subagent ownership. → **Outcome**: archive can proceed for this repo after second-opinion approval while external migrations remain visible, prioritized, and not falsely claimed as completed in-repo.
4. **Challenge: docs could drift ahead of physical SDK relocation** → **Solution applied**: docs were updated to target architecture while BUILD handoff explicitly preserved Phase 6-gated `_pylib`/`_tslib` cleanup → **Evidence**: BUILD logs D7.6/D7.7 record `PLUGIN_AUTHORING.md`, `SKILL_AUTHORING.md`, `ARCHITECTURE.md`, `DISTROS.md`, `distro-config.md`, and `PHILOSOPHY.md` updates; `docs/ARCHITECTURE.md` and `docs/DISTROS.md` now describe `tabula_plugin_sdk` and mixed plugin bundles; the repository still contains `skills/_pylib/` and `skills/_tslib/`, and `memory-bank/tasks.md` D6.4/D6.6 plus Section 13 track the remaining relocation/removal work. → **Outcome**: author-facing docs are aligned with the intended architecture, but archive must preserve the compatibility-code caveat until external SDK packaging lands.

## 9. Strategic Technical Insights
- A small lifecycle-neutral interface (`HookSubscriber`) can be safer than registering long-lived processes as pseudo-clients. It preserved existing behavior while avoiding supervision-domain coupling.
- Splitting protocol version constants early prevents accidental coupling between client wire protocol and plugin stdio protocol. This is especially important when one surface is consumed by gateways and the other by plugin SDKs.
- Reference plugins are more valuable when they act as live protocol specs. `plugin-hello` validating tool calls, events, send/log, diagnostics, and child cleanup reduced ambiguity more than static docs alone.
- Migrate-on-load lock formats are worth the extra implementation cost for architecture shifts: they make installer evolution possible without forcing users through manual recovery.
- Keeping intentionally dead code must be paired with clear skip markers, TODOs, and follow-up ownership. Otherwise migration bridges become permanent ambiguity.

## 10. Process Improvement Insights
- For large architectural migrations, add a reflection-ready evidence matrix during BUILD: requirement → files → tests → residual risk. This would make SECURITY/REFLECT faster and reduce reliance on long agent logs.
- External repo boundaries should become explicit checklist categories, not just prose. This task benefited from scope clarification, but future archives should split “implemented here,” “contract-only here,” and “external PR required.”
- Dependency-audit tools should be available before SECURITY starts. Degraded audit checks are avoidable process debt.
- Final docs sweeps should include both forbidden-token greps and semantic drift checks for source-of-truth design docs versus current architecture docs.

## 11. Business Impact
- **Value delivered**: Tabula now has the foundation for a plugin ecosystem: long-lived integrations, hook handlers, gateways, MCP bridges, and drivers can move out of overloaded skill semantics and into supervised plugin processes.
- **Business metrics affected**:
  - Extension reliability: restart/backoff and diagnostics improve operational stability.
  - Distribution flexibility: mixed skill/plugin bundles let distros compose capabilities without kernel builtin assumptions.
  - Security posture: minimal kernel tool surface reduces default LLM-visible authority.
  - Developer experience: `PLUGIN_AUTHORING.md`, reference SDK, and `plugin-hello` lower the cost of authoring new plugins.
  - Upgrade safety: lock v2 migrate-on-load reduces breakage for installed users.

## 12. Strategic Action Items
- **Priority 1**: Complete external `tabula-bundles` migrations for hooks, MCP, drivers, gateways, and subagent plugin ownership of spawn-depth/MaxChildren semantics.
- **Priority 2**: Remove or quarantine stale legacy builtin metadata (`cmd/tabula/kernel.tools.json`, related filter tests) so deleted kernel tools cannot be accidentally advertised.
- **Priority 3**: Harden operational security around plugin diagnostics, audit tooling, and plugin/boot stderr logging before broad network-exposed or multi-user deployments.

## 13. Follow-up Tasks
<!-- FOLLOW_UP_START -->
- [ ] Clean up stale legacy builtin metadata in `cmd/tabula/kernel.tools.json` and related tests, or formally mark it non-runtime-only with guardrails | Priority: high | Source: skill-plugin-architecture
- [ ] Add/authenticate or document strict locality for `GET /internal/snapshot/plugins`; verify bind/origin assumptions for non-local deployments | Priority: high | Source: skill-plugin-architecture
- [ ] Add release CI dependency audits for Go (`govulncheck` or equivalent), Python SDK artifacts (`pip-audit`), and JS/TS bundle lockfiles once they are present | Priority: medium | Source: skill-plugin-architecture
- [ ] Define stderr/log redaction guidance and optional runtime redaction for plugin stderr and boot stderr | Priority: medium | Source: skill-plugin-architecture
- [ ] Complete external `tabula-bundles` migrations: hook plugins, MCP plugin, driver/subagent plugins, gateway plugins, and remaining per-call skills with `tools[].exec` | Priority: high | Source: skill-plugin-architecture
- [ ] Implement subagent plugin-side spawn-token/MaxChildren/depth coverage, then remove D1.11(b) dead code and skipped kernel spawn tests | Priority: high | Source: skill-plugin-architecture
- [ ] Finish Phase 6 SDK/lib relocation: remove in-repo `skills/_pylib` and `skills/_tslib`, remove distro `_pylib`/`_tslib` preserve behavior, and replace inline `_*` support-dir compatibility once bundled wheel/tarball packages exist | Priority: high | Source: skill-plugin-architecture
- [ ] Add provenance metadata convention for plugin-emitted `send` bus messages if downstream consumers need stable plugin identity | Priority: low | Source: skill-plugin-architecture
- [ ] Build a requirement-to-test evidence matrix for future L4 architecture tasks and include it before SECURITY handoff | Priority: medium | Source: skill-plugin-architecture
<!-- FOLLOW_UP_END -->

## Archive Readiness Notes
- Primary reflection has been revised to address the L4 second-opinion requests for Section 8 evidence references and explicit SECURITY integration.
- SECURITY passed with warnings; no unresolved blocking issue requires reopening BUILD.
- Archive should preserve the distinction between in-repo foundation completion and external component migrations.
- Archive should wait for the second-opinion recheck to approve this revised primary reflection.
- Structured lesson append to `memory-bank/systemPatterns.md` is deferred until the L4 second-opinion step approves the primary reflection, per CR-B.
