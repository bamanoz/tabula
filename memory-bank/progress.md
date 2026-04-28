# Progress

Implementation progress is tracked here across all tasks.

## skill-plugin-architecture-followups

### CREATIVE Phase — Current-Cycle Refinement (2026-04-27)
- Status: DONE for CREATIVE design refinement; phase status remains router-managed.
- Design artifacts updated: `memory-bank/creative/creative-plugin-runtime.md` §14, `memory-bank/creative/creative-sdk-and-distro.md` §9, and `memory-bank/systemPatterns.md` evidence-gated compatibility removal pattern.
- Runtime decision: selected **regression-preservation plus matrix-gated conditional cleanup**. BUILD must preserve already-landed repo-local hardening and may delete D1.11(b) spawn-token/MaxChildren/depth bridges only after green or owner-approved not-applicable driver/subagent matrix evidence proves replacement child-auth, depth, lifecycle, cleanup, and fail-closed security invariants.
- SDK/distro decision: selected **keep compatibility until package evidence is green**. Physical removal of `skills/_pylib`, `skills/_tslib`, distro preserve behavior, and inline sibling `_*` compatibility remains blocked until Python wheel, TypeScript tarball, and bundle `_lib` install-path rows have concrete artifact and consumer-test evidence.
- Diagnostics decision: no remote authenticated diagnostics design is selected for this task; `/internal/snapshot/plugins` remains local-only with loopback remote plus local/loopback host checks and no trust in proxy/forwarded headers.
- BUILD handoff: first re-check the external matrix; if no new evidence exists, perform focused regression/evidence verification only and carry blockers forward. Do not turn rows green from archived Memory Bank prose or target architecture text.

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: Regression-preservation and evidence-gate verifier
- Work: Captured Task Base Commit `48f84f8aa981c575259d42353557b48a305b3a0a`, re-verified the canonical external matrix remains entirely `unknown/blocker`, and performed the safe first BUILD pass as focused regression verification rather than bridge deletion. Confirmed stale builtin metadata/local diagnostics baselines, D1.11(b) bridge surfaces, and Phase 6 SDK/distro compatibility surfaces remain in their intended gated states; updated task checkboxes for the two already-satisfied repo-local lanes.
- Addressed: N/A — first agent
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: No new repo-source risk introduced; the standing risk is accidental deletion or completion claims based on historical repo-local hardening rather than concrete external `tabula-bundles` PR/artifact evidence.
- Open issues: All external matrix rows remain `unknown/blocker`; do not remove kernel spawn-token/MaxChildren/depth bridge code, skipped kernel spawn tests, `skills/_pylib`, `skills/_tslib`, distro preserve behavior, or inline sibling `_*` compatibility until required rows turn green or owner-approved not-applicable with evidence.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read required Memory Bank context, the current-cycle BUILD handoff, the external migration matrix, and the actual working-set files for diagnostics/stale kernel tools, D1.11(b) spawn-token/depth/MaxChildren bridges, and Phase 6 SDK/distro compatibility surfaces. Ran scoped verification searches for matrix blocker rows, spawn/token/event bridge symbols, and `_pylib`/`_tslib` preserve/support-dir references. Declined because no evidence-safe implementation gap was found: Agent 1's regression-preservation state is accurate and remaining destructive cleanup is blocked by external evidence rows that are still `unknown/blocker`.
- Addressed: Confirmed metadata is valid (`Intent=implement`, `Category=deep`); confirmed all external matrix rows remain `unknown/blocker`; confirmed `/internal/snapshot/plugins` remains guarded by loopback `RemoteAddr` plus local/loopback `Host`; confirmed stale `kernel_tools` selections warn-and-empty; confirmed D1.11(b) bridge code and skipped test references remain present by design; confirmed distro `_pylib`/`_tslib` preserve behavior and inline sibling `_*` support-dir compatibility remain intentionally retained until package evidence exists.
- Files: none
- Risks: none new; the standing risks remain accidental deletion or completion claims for the D1.11(b) spawn bridge and Phase 6 SDK/lib relocation without concrete external `tabula-bundles` PR/artifact, Python wheel, TypeScript tarball, and bundle `_lib` install evidence.
- Open issues: All external matrix rows remain `unknown/blocker`; later BUILD agents should only update the matrix from concrete external PR/artifact evidence or perform additional evidence-safe verification, and must not remove kernel spawn-token/MaxChildren/depth or SDK support-dir compatibility bridges yet.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 3 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, latest BUILD handoff, and canonical external migration matrix, then inspected the scoped working-set surfaces from a blocker-safety angle: local plugin diagnostics, stale kernel-tool filtering, D1.11(b) spawn/depth/MaxChildren bridge code, and Phase 6 SDK/distro support-dir compatibility. Declined because the current repo state still matches the evidence-gated design: all external rows remain `unknown/blocker`, and the only remaining destructive cleanup would be unsafe without concrete external artifacts.
- Addressed: Confirmed metadata is valid (`Intent=implement`, `Category=deep`); confirmed `/internal/snapshot/plugins` is still protected by `internalDiagnosticsGuard` with loopback remote and local/loopback host checks; confirmed `filterKernelTools` keeps an empty live catalog while warning on stale selections; confirmed `generateSpawnToken`, `TABULA_SPAWN_TOKEN`, `CanSpawn`, `MaxChildren`, and `skipKernelBuiltinRemoved` callers remain present by design; confirmed distro `_pylib`/`_tslib` preserve behavior and inline sibling `_*` materialization remain gated by SDK/package evidence.
- Files: none
- Risks: none new; standing risks remain accidental D1.11(b) spawn bridge deletion or Phase 6 SDK/lib/support-dir removal before driver/subagent plugin, Python wheel, TypeScript tarball, and bundle `_lib` rows turn green or owner-approved not-applicable with evidence.
- Open issues: All external matrix rows remain `unknown/blocker`; later BUILD agents should only update the matrix from concrete external PR/artifact evidence or run additional evidence-safe verification, and must not delete kernel spawn-token/MaxChildren/depth bridges, skipped kernel spawn tests, `skills/_pylib`, `skills/_tslib`, distro preserve behavior, or inline support-dir compatibility yet.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 4 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, current BUILD handoff, canonical external migration matrix, and scoped working-set files from a fourth-pass evidence-gate angle. Declined because I agree with Agents 1–3: all external bundle/SDK rows remain `unknown/blocker`, repo-local hardening remains preserved, and the remaining implementation would be destructive bridge removal without required external proof.
- Addressed: Confirmed metadata is valid (`Intent=implement`, `Category=deep`); confirmed the matrix still has no concrete external paths, validation commands, commits, PRs, wheel/tarball artifacts, or bundle `_lib` install evidence; confirmed `/internal/snapshot/plugins` remains guarded by loopback remote plus local/loopback Host checks; confirmed stale `kernel_tools` selections still warn-and-empty; confirmed D1.11(b) spawn-token/depth/MaxChildren code and skipped tests remain intentionally retained; confirmed Phase 6 `_pylib`/`_tslib` preserve/support-dir compatibility and deprecated SDK constants remain gated with `DEFAULT_KERNEL_TOOLS` empty.
- Files: none
- Risks: none new; standing risks remain accidental completion claims or deletion of kernel spawn-token/MaxChildren/depth bridges, skipped kernel spawn tests, physical SDK support dirs, distro preserve behavior, or inline support-dir compatibility before driver/subagent plugin and packaged SDK evidence turns green or owner-approved not-applicable.
- Open issues: All external matrix rows remain `unknown/blocker`; later BUILD agents should only update the matrix from concrete external PR/artifact/release evidence or decline after evidence-safe verification, and must not remove gated compatibility bridges yet.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

### Build Progress

- Requirement: Start reopened BUILD by preserving already-landed repo-local hardening while enforcing external evidence gates for destructive cleanup.
- Approach: Read required Memory Bank context, captured the base commit, re-checked the external migration matrix, ran scoped searches for diagnostics, stale kernel tools, D1.11(b) spawn/depth bridge code, and Phase 6 SDK/distro compatibility surfaces, then ran the focused regression suite without deleting gated bridges.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Testing results: `go test ./cmd/tabula ./internal/kernel ./internal/kernel/plugin`, `PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py`, `python3 -m unittest skills/_pylib/test_protocol.py`, and `bun test` in `skills/_tslib` all passed on 2026-04-27.

### SECURITY Phase — Attempt 1 (2026-04-27)
- Verdict: PASSED
- Blocking findings: 0
- Warning findings: 4
- Degraded checks: Python dependency audit (`pip-audit` unavailable)
- Report: memory-bank/security/security-skill-plugin-architecture-followups.md
- Notes: Re-audited the current reopened BUILD/regression-preservation scope. No dependency manifests changed; `.opencode` npm audit moderate findings remain pre-existing and unrelated to BUILD dependency changes. External `tabula-bundles` rows remain `unknown/blocker`, accepted only because BUILD did not delete gated D1.11(b) or Phase 6 compatibility bridges.

### REFLECT Second Opinion (2026-04-27)
- Verdict: APPROVED
- Revisions consumed: 0 / 1
- Report: memory-bank/reflection/reflection-skill-plugin-architecture-followups-second-opinion.md

### REFLECT Phase — Primary L4 Reflection (2026-04-27)
- Status: DONE for primary reflection; phase status updated to `REFLECT: DONE`; archive readiness remains BLOCKED unless unresolved external lanes are formally split/descoped.
- Report: `memory-bank/reflection/reflection-skill-plugin-architecture-followups.md`
- Summary: BUILD and SECURITY phase gates were verified complete (`BUILD: DONE`, `SECURITY: DONE`; SECURITY verdict `PASSED`, 0 blockers, 4 warnings). Reflection concludes the repo-local safe hardening lane is complete: stale builtin metadata is no longer advertised, `/internal/snapshot/plugins` is local-only guarded, boot/distro/plugin catalog validation and fail-closed reply handling landed, and active docs/temporary SDK surfaces align with CREATIVE §13.
- Blockers: active task completion remains partial. External `tabula-bundles` migration rows are still `unknown/blocker`; D1.11(b) spawn-token/MaxChildren/depth bridge deletion and skipped kernel spawn-test removal remain blocked; Phase 6 physical SDK/lib relocation (`skills/_pylib`, `skills/_tslib`, distro preserve behavior, inline `_*` compatibility) remains blocked by missing Python wheel, TS tarball, and bundle `_lib` evidence.
- Follow-up integration: L4 second-opinion is approved; structured lesson appended to `memory-bank/systemPatterns.md`; lessons rotation not triggered.
- Recommended next action: either obtain external evidence and return to BUILD for gated deletion lanes, or formally split/descope unresolved external deliverables before/during archive.

### SECURITY Phase — Attempt 1 (2026-04-27)
- Verdict: PASSED
- Blocking findings: 0
- Warning findings: 4
- Degraded checks: Python dependency audit (`pip-audit` unavailable); existing `.opencode` npm audit moderate findings are unrelated to BUILD dependency changes
- Report: memory-bank/security/security-skill-plugin-architecture-followups.md

- 2026-04-27 — CREATIVE: Completed the Level 4 design supersession for subagent/plugin spawn ownership. Updated `creative-plugin-runtime.md` with §13 selecting the replacement-auth, subagent-plugin-owned model; marked the old §7 dead-code-keep decision and §8 spawn-event decision as superseded; reconciled `creative-sdk-and-distro.md` so generic SDK `spawn` is reserved/future-only; added a system pattern for kernel-vs-subagent spawn ownership; advanced phase status to `CREATIVE: DONE` and handed off to BUILD.

- 2026-04-27 — BUILD Agent 1: Completed the first repo-local safe hardening lane by removing stale embedded builtin tool metadata from live advertisement, making explicit stale `kernel_tools` boot selections warn-and-empty, and adding a local-only guard plus operational docs for `/internal/snapshot/plugins`. Verified with `go test ./cmd/tabula ./internal/kernel`.

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: Repo-local safe hardening implementer
- Work: Captured Task Base Commit `48f84f8aa981c575259d42353557b48a305b3a0a` and implemented the first BUILD lane. Replaced `cmd/tabula/kernel.tools.json` with an empty catalog, changed `filterKernelTools` to return no metadata by default and warn-and-empty explicit stale/unknown `kernel_tools`, threaded listener host into HTTP handler registration, guarded `/internal/snapshot/plugins` by loopback `RemoteAddr` plus loopback/local `Host`, and documented the endpoint locality/metadata exposure in `docs/PLUGIN_AUTHORING.md`.
- Addressed: N/A — first agent
- Files: modified `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `cmd/tabula/kernel.tools.json`, `docs/PLUGIN_AUTHORING.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The diagnostics guard intentionally rejects public/proxied hosts even when `RemoteAddr` is loopback; deployments behind reverse proxies need a future authenticated diagnostics design. Explicit `kernel_tools` config now logs one warning per stale/unknown entry; later agents should confirm warning volume is acceptable in production boot logs.
- Open issues: Implement strict boot/distro `tools[].exec` validation, plugin dynamic catalog validation, and inbound plugin reply fail-closed behavior. Keep D1.11(b) spawn bridge deletion and Phase 6 SDK/lib removal blocked until external matrix rows for driver/subagent, Python wheel, TS tarball, and bundle `_lib` are green.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Per-call skill validation implementer
- Work: Implemented strict boot skill descriptor validation before advertising tools or constructing dispatch entries, requiring trimmed non-empty `name`/`exec` and rejecting duplicate names while preserving non-exec metadata. Added distro-side `SKILL.md` frontmatter validation for advertised `tools[]` entries during staged installs, with negative tests for missing/blank fields, invalid shape, and atomic bundle-install failure that leaves current generation/lock/runtime surfaces unchanged.
- Addressed: Agent 1 open issue for boot/distro `tools[].exec` validation; strengthened the external per-call skill migration matrix so repo-side validation exists but external migrated-manifest evidence remains required before green status.
- Files: modified `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `tools/tabula-distro/src/tabula_distro/install.py`, `tools/tabula-distro/tests/test_install.py`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: The distro validator intentionally uses a minimal frontmatter parser scoped to top-level `tools[].name`/`tools[].exec`; unusual YAML forms outside the documented authoring shape may need a future parser dependency if external bundles require them. `python -m pytest` was unavailable in this environment, so equivalent `unittest` execution was used with `PYTHONPATH`.
- Open issues: Implement plugin dynamic `register`/`update_tools` catalog validation and inbound `event_reply`/`tool_result` fail-closed behavior. Keep D1.11(b) spawn bridge deletion and Phase 6 SDK/lib removal blocked until external matrix rows are green.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Dynamic plugin protocol validation implementer
- Work: Added reusable plugin catalog normalization for dynamic `register` and `update_tools` payloads, including trimmed non-empty/unique tool names, non-negative deadlines, and non-empty subscription events. Wired kernel-side register validation into runtime spawn and lifecycle installation so invalid catalogs/subscriptions fail non-restartably and do not mutate registry, hook index, or dispatch state; made invalid `update_tools` atomic; rejected ambiguous/empty `tool_result` and invalid/unknown `event_reply` without converting security hooks to pass.
- Addressed: Agent 2 open issue for plugin dynamic catalog validation and inbound reply fail-closed behavior; updated the external plugin migration matrix to reflect repo-side validation while leaving actual external plugin rows blocked pending migrated-path evidence.
- Files: created `internal/kernel/plugin/catalog_validation.go`; modified `internal/kernel/plugin/handle.go`, `internal/kernel/plugin/runtime.go`, `internal/kernel/plugin/handle_test.go`, `internal/kernel/plugin/runtime_test.go`, `internal/kernel/plugin_tools.go`, `internal/kernel/plugin_tools_test.go`, `internal/kernel/plugin_runtime.go`, `internal/kernel/plugin_runtime_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: `before_spawn`/`after_spawn` still exist in `HookEvents` as reserved/historical names per creative §13, so subscription validation currently accepts them until docs/event-surface cleanup chooses a stricter active list. Invalid `event_reply`/`tool_result` releases pending plugin call IDs to avoid resource leaks; callers observe the existing timeout/cancel fail-safe rather than a synthetic plugin success.
- Open issues: Align active docs/SDK surfaces for reserved `api.spawn`, `TABULA_SPAWN_TOKEN`, `before_spawn`/`after_spawn`, stale builtin constants, and `DEFAULT_KERNEL_TOOLS`; keep D1.11(b) spawn bridge deletion and Phase 6 SDK/lib removal blocked until external matrix rows are green.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Active docs and SDK public-surface alignment implementer
- Work: Removed the active generic plugin `api.spawn` promise and common `TABULA_SPAWN_TOKEN` runtime variable from `docs/SKILL_AUTHORING.md`, marked `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md` as historical/superseded for spawn-token examples, removed the TypeScript public `tabulaSpawnToken()` helper, and updated Python/TypeScript temporary SDK protocol contracts so `DEFAULT_KERNEL_TOOLS` is empty while stale kernel tool names remain deprecated compatibility strings only. Updated tests and matrix notes to preserve deletion gates.
- Addressed: Agent 3 open issue for docs/SDK surface cleanup around reserved spawn/token/default-tool surfaces; reduced the active-doc grep to reserved/historical `before_spawn`/`after_spawn` wording and explicitly historical plan examples.
- Files: modified `docs/SKILL_AUTHORING.md`, `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`, `skills/_pylib/protocol.py`, `skills/_pylib/test_protocol.py`, `skills/_pylib/SKILL.md`, `skills/_tslib/src/protocol.ts`, `skills/_tslib/src/paths.ts`, `skills/_tslib/tests/protocol.test.ts`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: Deprecated `TOOL_SHELL_EXEC` / `TOOL_PROCESS_*` constants still exist in temporary support dirs until Phase 6 package artifacts are green or consumers migrate; `before_spawn` / `after_spawn` remain accepted/reserved names, so SECURITY should verify docs do not imply kernel emission.
- Open issues: D1.11(b) spawn bridge deletion remains blocked by green driver/subagent external evidence; Phase 6 physical removal of `skills/_pylib` / `skills/_tslib` and distro/script compatibility remains blocked by Python wheel, TS tarball, and bundle `_lib` rows. Later agents should continue top-level docs/scripts cleanup only when package-artifact evidence is green or marked target-state with explicit caveats.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Top-level docs grep-gate cleanup implementer
- Work: Updated active top-level docs that were safe to align without deleting compatibility bridges: `README.md` now describes zero default kernel tools, plugin-owned subagents, and packaged SDK distribution instead of a runtime `skills/_pylib` contract; `tests/README.md` now frames the legacy Python support-dir contract test as temporary until packaged SDK evidence is green; `docs/ARCHITECTURE.md` now describes subagent depth/child-auth ownership as target/gated by external driver-subagent evidence rather than completed bridge removal. Verified grep gates for README/tests/architecture and reran the Python protocol contract test.
- Addressed: Agent 4 open issue to continue top-level docs cleanup only where package-artifact evidence is not required or wording can be explicitly caveated; reduced active top-level `_pylib`/`_tslib` references without claiming Phase 6 completion.
- Files: modified `README.md`, `tests/README.md`, `docs/ARCHITECTURE.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Some active scripts, distro installer code, `conftest.py`, and temporary SDK support dirs still contain legacy support-dir references by design; they must not be removed until Python wheel, TS tarball, and bundle `_lib` rows are green. `docs/ARCHITECTURE.md` still lists reserved `before_spawn`/`after_spawn` names aligned with CREATIVE §13.
- Open issues: D1.11(b) spawn bridge deletion remains blocked by green driver/subagent evidence; Phase 6 physical SDK/lib removal and script/distro compatibility cleanup remain blocked by Python wheel, TS tarball, and bundle `_lib` matrix rows. Later agents should avoid deleting these bridges unless external rows change state.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, latest BUILD handoff, and actual files in the current working set, then ran scoped verification searches for spawn/token/event surfaces, SDK legacy support-dir references, and external matrix state. Declined because the remaining open work is intentionally evidence-gated: all external rows are still `unknown/blocker`, active docs/temporary SDK surfaces already align with CREATIVE §13, and removing D1.11(b) or Phase 6 compatibility bridges would violate the documented gates.
- Addressed: Confirmed Agent 5's handoff remains accurate: `README.md`, `tests/README.md`, and `docs/ARCHITECTURE.md` have no active `_pylib`/`_tslib`/`requires-kernel-tools` grep hits; `api.spawn` / `TABULA_SPAWN_TOKEN` hits are limited to historical Memory Bank/plan evidence and aligned active caveats; `before_spawn` / `after_spawn` active hits remain reserved/subagent-owned wording only. Confirmed matrix rows for driver/subagent plugins, Python SDK wheel, TypeScript SDK tarball, and bundle `_lib` paths remain blockers.
- Files: none
- Risks: none new; existing risks remain the gated D1.11(b) spawn bridge and Phase 6 SDK/lib compatibility removals pending external evidence.
- Open issues: External matrix rows remain `unknown/blocker`; do not remove kernel spawn-token/MaxChildren/depth bridge code, skipped kernel spawn tests, `skills/_pylib` / `skills/_tslib`, distro preserve behavior, or script/distro SDK compatibility until required rows turn green or not-applicable with owner-approved rationale.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 7 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, active BUILD handoff, external migration matrix, and the actual files named in the working set, then ran scoped verification searches for spawn/token/event surfaces, SDK/default-tool surfaces, support-dir references, and matrix blocker state. Declined because I agree with Agent 6: the remaining actionable implementation is gated by external `tabula-bundles` evidence rows that are still `unknown/blocker`, while repo-local docs/SDK surfaces already match CREATIVE §13 caveats.
- Addressed: Confirmed the external rows for driver/subagent plugins, Python SDK wheel, TypeScript SDK tarball, and bundle `_lib` paths remain `unknown/blocker`; confirmed active top-level docs and authoring docs avoid stable `api.spawn` / common `TABULA_SPAWN_TOKEN` promises; confirmed temporary SDK support dirs keep deprecated constants with `DEFAULT_KERNEL_TOOLS` empty and no public `tabulaSpawnToken()` helper.
- Files: none
- Risks: none new; deleting D1.11(b) spawn-token/MaxChildren/depth bridge code, skipped kernel spawn tests, physical `skills/_pylib` / `skills/_tslib`, distro preserve behavior, or script/distro SDK compatibility remains unsafe until the required external rows turn green or not-applicable with owner-approved rationale.
- Open issues: External matrix rows remain `unknown/blocker`; later BUILD agents should only perform evidence-safe verification or update matrix rows from concrete external PR/artifact evidence, not remove compatibility bridges from this repo.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 8 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, latest BUILD handoff, external migration matrix, and scoped working-set files for active docs and temporary SDK surfaces. Ran focused searches for `api.spawn` / `TABULA_SPAWN_TOKEN` / `before_spawn` / `after_spawn`, default-kernel-tool constants, support-dir references, and matrix blocker state. Declined because no new evidence-safe implementation gap emerged: repo-local hardening/docs/SDK cleanup are already aligned, while remaining deletion lanes are externally gated.
- Addressed: Confirmed active authoring and plugin docs avoid stable generic spawn or common spawn-token promises, plan docs are explicitly historical/superseded, `before_spawn` / `after_spawn` wording is reserved/subagent-owned, temporary SDK protocol constants keep `DEFAULT_KERNEL_TOOLS` empty and no public TS spawn-token helper, and all required external rows remain `unknown/blocker`.
- Files: none
- Risks: none new; D1.11(b) spawn bridge deletion and Phase 6 physical SDK/lib plus script/distro compatibility removal remain unsafe without green or owner-approved not-applicable external evidence for driver/subagent plugins, Python wheel, TypeScript tarball, and bundle `_lib` paths.
- Open issues: External matrix rows remain `unknown/blocker`; subsequent BUILD agents should continue evidence-safe verification or update the matrix only from concrete external artifacts, and must not remove kernel spawn-token/MaxChildren/depth or SDK support-dir compatibility bridges from this repo yet.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 9 — [DECLINE]
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context, latest BUILD handoff, external migration matrix, and scoped working-set docs/SDK files. Ran a focused reference check across active docs, temporary SDK protocol/path tests, and the matrix for spawn/token/event surfaces, SDK support-dir references, default-kernel-tool contracts, and blocker row state. Declined because I agree with Agents 6–8: all safe repo-local hardening/docs/SDK cleanup is already implemented, while remaining D1.11(b) and Phase 6 deletion lanes are blocked by external `tabula-bundles` evidence rows still marked `unknown/blocker`.
- Addressed: Confirmed `README.md`, `tests/README.md`, `docs/SKILL_AUTHORING.md`, `docs/PLUGIN_AUTHORING.md`, and `docs/ARCHITECTURE.md` align with CREATIVE §13 by avoiding a stable generic `api.spawn` or common `TABULA_SPAWN_TOKEN` contract; confirmed active `before_spawn`/`after_spawn` wording is reserved/subagent-owned; confirmed temporary Python/TypeScript SDK surfaces keep `DEFAULT_KERNEL_TOOLS` empty and no public `tabulaSpawnToken()` helper.
- Files: none
- Risks: none new; deleting kernel spawn-token/MaxChildren/depth bridge code, skipped kernel spawn tests, physical `skills/_pylib` / `skills/_tslib`, distro preserve behavior, or script/distro SDK compatibility remains unsafe until required external rows turn green or owner-approved not-applicable with evidence.
- Open issues: External matrix rows remain `unknown/blocker`; later BUILD agents should only perform evidence-safe verification or update matrix rows from concrete external PR/artifact evidence, not remove compatibility bridges from this repo.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

### Build Progress

- Requirement: Start BUILD with repo-local safe hardening that does not depend on external `tabula-bundles` evidence.
- Approach: Used the CREATIVE supersession to choose the safe lane first: remove live kernel builtin metadata, preserve compatibility with warnings rather than hard errors for stale `kernel_tools`, enforce local-only plugin diagnostics, and update focused tests/docs.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `cmd/tabula/kernel.tools.json`, `docs/PLUGIN_AUTHORING.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Testing results: `go test ./cmd/tabula ./internal/kernel` passed on 2026-04-27.

- Requirement: Add repo-side validation that migrated per-call skills cannot be silently advertised or installed without executable tool commands.
- Approach: Validated boot `skills`/legacy `tools` descriptors before appending to `allTools`, and validated distro `SKILL.md` advertised `tools[]` entries inside staging before component copies promote. No-tool placeholder skills remain compatible but do not count as migrated per-call skill evidence.
- Files modified: `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `tools/tabula-distro/src/tabula_distro/install.py`, `tools/tabula-distro/tests/test_install.py`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`.
- Testing results: `go test ./cmd/tabula` passed on 2026-04-27; `python -m pytest tools/tabula-distro/tests/test_install.py` could not run because `python`/`pytest` were unavailable, then `PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py` passed 34 tests on 2026-04-27.

- Requirement: Add repo-side validation that long-lived plugin registrations, dynamic tool updates, and inbound replies cannot silently install invalid catalogs or fail open.
- Approach: Normalized plugin dynamic catalogs before state mutation, rejected unsupported subscription events during kernel registration, made invalid `update_tools` side-effect-free, and rejected malformed/ambiguous plugin replies without delivering permissive hook/tool outcomes.
- Files modified: `internal/kernel/plugin/catalog_validation.go`, `internal/kernel/plugin/handle.go`, `internal/kernel/plugin/runtime.go`, `internal/kernel/plugin/handle_test.go`, `internal/kernel/plugin/runtime_test.go`, `internal/kernel/plugin_tools.go`, `internal/kernel/plugin_tools_test.go`, `internal/kernel/plugin_runtime.go`, `internal/kernel/plugin_runtime_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`.
- Testing results: `go test ./internal/kernel/plugin ./internal/kernel` passed on 2026-04-27.

- Requirement: Align active docs and temporary SDK public surfaces with the CREATIVE §13 spawn/auth/default-tool decisions without deleting external-evidence-gated compatibility paths.
- Approach: Removed generic spawn-token promises from active authoring docs and TS path helpers, marked plan-doc spawn examples as historical, kept `before_spawn`/`after_spawn` only as reserved/subagent-owned wording, and changed Python/TS protocol tests to prove empty default kernel tools while retaining stale tool-name literals as deprecated compatibility strings.
- Files modified: `docs/SKILL_AUTHORING.md`, `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`, `skills/_pylib/protocol.py`, `skills/_pylib/test_protocol.py`, `skills/_pylib/SKILL.md`, `skills/_tslib/src/protocol.ts`, `skills/_tslib/src/paths.ts`, `skills/_tslib/tests/protocol.test.ts`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`.
- Testing results: `python3 -m unittest skills/_pylib/test_protocol.py`, `bun test` in `skills/_tslib`, and `go test ./cmd/tabula ./internal/kernel` passed on 2026-04-27.

- Requirement: Clean active top-level docs where wording could be aligned to the target architecture without claiming external package artifacts or deleting compatibility paths.
- Approach: Rewrote README runtime/tool/subagent/SDK prose to remove active `skills/_pylib` contract claims and built-in kernel tool descriptions, caveated test-contract documentation as a temporary legacy support-dir check, and clarified architecture subagent policy ownership as target/gated until external driver/subagent evidence is green.
- Files modified: `README.md`, `tests/README.md`, `docs/ARCHITECTURE.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Testing results: grep checks for `_pylib`/`_tslib`/`requires-kernel-tools` in `README.md`, `tests/README.md`, and `docs/ARCHITECTURE.md` returned no matches; `python3 -m unittest skills/_pylib/test_protocol.py` passed on 2026-04-27.

### Creative Progress

- Decision: child spawning is not a stable generic plugin/SDK capability for this task. The subagent plugin owns any spawn API, replacement child credential/channel, depth derivation, MaxChildren accounting, parent/session scoping, lifecycle/list/kill/cancel, and cleanup.
- Surface disposition: `api.spawn` is reserved/future-only; `TABULA_SPAWN_TOKEN` and `tabulaSpawnToken()` are not common runtime/public SDK contracts; `before_spawn`/`after_spawn` are not kernel-emitted and are reserved/subagent-owned unless later plugin evidence makes them concrete; stale builtin constants are deprecated compatibility strings only; `DEFAULT_KERNEL_TOOLS` should be empty/absent in active SDK packages.
- Deletion gates: repo-local safe hardening can proceed independently; D1.11(b) bridge deletion requires green driver/subagent external evidence and replacement-auth invariant tests; Phase 6 SDK/lib deletion requires green Python wheel, TS tarball, and bundle `_lib` rows.
- Validation handoff: BUILD should preserve boot/distro `tools[].exec` validation, dynamic plugin register/update catalog validation, inbound plugin reply fail-closed behavior, local-only `/internal/snapshot/plugins`, and active docs/SDK grep gates.
- Files modified: `memory-bank/creative/creative-plugin-runtime.md`, `memory-bank/creative/creative-sdk-and-distro.md`, `memory-bank/systemPatterns.md`, `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`.
- Open blockers: external `tabula-bundles` evidence remains unknown/blocker for D1.11(b) and Phase 6 deletion; if the external subagent plugin retains kernel-side token validation, BUILD must keep/document minimal residual kernel connection responsibility.

- 2026-04-27 — PLAN Agent 1: Validated metadata (`Intent=implement`, `Category=deep`) and established the first planning baseline for grouped skill/plugin architecture follow-ups. Verified current code surfaces for legacy builtin metadata, internal plugin diagnostics endpoint exposure, absence of in-repo `tabula-bundles`, D1.11(b) spawn bridge, distro runtime-surface preserve behavior, and legacy SDK directories. Added sequenced workstreams, gates, validation strategy, and risk framing to `memory-bank/tasks.md`.

### Pipeline Plan Log

#### Agent 1 — [CONTRIBUTE]
- Role: Scope architect / dependency-sequencing planner
- Work: Created the initial PLAN baseline for the Level 4 grouped follow-up. Decomposed the task into six workstreams: pre-flight boundary/evidence audit, legacy builtin metadata cleanup/quarantine, plugin diagnostics locality/security hardening, external bundle migration evidence, subagent invariant relocation/kernel bridge removal, and Phase 6 SDK/lib relocation cleanup. Grounded the plan in direct reads/greps of the active Memory Bank context and scoped project files rather than archived pipeline assumptions alone.
- Addressed: N/A — first agent
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: `kernel.tools.json` cleanup may break dev boot expectations if replacements are not verified; `/internal/snapshot/plugins` may require enforcement rather than docs-only locality; D1.11(b) spawn bridge must not be deleted before subagent-plugin replacement coverage; SDK/lib deletion depends on packaged wheel/tarball availability and consumer script rewrites; external `tabula-bundles` completion cannot be proven from this repo alone.
- Open issues: Later PLAN agents should refine the concrete locality/auth design for internal HTTP diagnostics; decide remove-vs-quarantine for embedded builtin metadata; produce an external migration evidence matrix; map D1.11(b) skipped kernel tests to plugin-side replacement tests; verify all top-level docs/scripts affected by SDK relocation.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Acceptance-criteria and risk-gate planner
- Work: Refined the baseline plan into concrete acceptance criteria for the five open decision areas. Verified the current code surfaces for `filterKernelTools`, `kernel.tools.json` tests, `/internal/snapshot/plugins`, WebSocket-only origin checks, `SnapshotPlugins` payload, D1.11(b) skipped tests/dead code, distro `_pylib`/`_tslib` preserve behavior, and the frozen creative decisions that govern spawn-bridge and SDK relocation gates. Added explicit preferred targets, required tests, and evidence artifacts to `memory-bank/tasks.md`.
- Addressed: Agent 1 open issues by choosing an enforcement-first diagnostics endpoint direction, preferring zero default builtin metadata advertisement, specifying an external bundle migration matrix, mapping D1.11(b) skip classes to plugin-side replacement coverage, and tightening SDK artifact gates.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Locality guard design still needs exact implementation details for proxied/non-loopback local deployments; external bundle evidence may remain unavailable inside this repo and must stay a blocker for bridge deletion; SDK docs already describe target packaged imports while physical legacy directories remain until artifacts exist.
- Open issues: Later PLAN agents should decide whether diagnostics guard needs config/env opt-in semantics; verify whether stale `kernel_tools` boot configs should warn-and-empty or hard-error; refine where the external migration matrix artifact belongs and who owns each row; confirm whether any skipped spawn lifecycle tests should be deleted rather than relocated because product behavior moved entirely to plugins.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Implementation-choice and ownership planner
- Work: Closed the Agent 2 open decisions by selecting local-only diagnostics with no env opt-in for this task, choosing warn-and-empty stale `kernel_tools` compatibility, assigning the external migration matrix location/row ownership, classifying D1.11(b) skipped tests into relocate/delete/convert paths, and defining concrete SDK wheel/tarball artifact proof before distro cleanup.
- Addressed: Agent 2 risks around proxied diagnostics locality, stale boot-config behavior, matrix ownership ambiguity, skipped spawn-test disposition, and SDK artifact availability.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Same-host/listener-host detection may need care for wildcard binds and IPv6 bracketed hosts during BUILD; warn-and-empty stale `kernel_tools` behavior needs an observable deprecation warning without reintroducing live metadata; external matrix rows may remain blockers if `tabula-bundles` evidence is inaccessible.
- Open issues: Later PLAN agents can refine exact helper/test names for diagnostics locality and draft the initial matrix artifact if useful; otherwise the main implementation choices are now actionable.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: BUILD-readiness and evidence-artifact planner
- Work: Converted the remaining diagnostics-locality naming gap into a concrete helper/test checklist, including wildcard bind, IPv6, and forwarded-header constraints. Seeded the canonical external bundle migration matrix artifact with required blocker rows for hook/MCP/driver-subagent/gateway plugins, `tools[].exec` skills, SDK packages, and `_lib` install paths.
- Addressed: Agent 3 open issues for exact diagnostics helper/test naming and initial external migration matrix drafting; mitigated the wildcard/IPv6 listener-host risk with explicit BUILD parsing constraints.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`; created `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: Matrix rows are intentionally `unknown/blocker` until external `tabula-bundles` evidence is supplied; helper names may be adjusted during BUILD if handler signatures change, but acceptance semantics should remain.
- Open issues: Later PLAN agents may add a D1.11(b) skipped-test relocation table or confirm docs references for SDK cleanup; otherwise PLAN is increasingly CREATIVE-ready.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Coverage-relocation and cleanup-gate planner
- Work: Added a granular D1.11(b) relocation/deletion table for each current `skipKernelBuiltinRemoved` caller and the direct spawn-token tests, tying deletion of kernel spawn bridge code to concrete plugin-side lifecycle, scoping, auth, timeout, and shutdown coverage. Expanded Phase 6 cleanup gates to include active scripts/release packaging surfaces and the remaining non-plan docs grep hit, so final SDK relocation has strict acceptance checks beyond `tools/tabula-distro`.
- Addressed: Agent 4 open issues for a more granular skipped-test relocation table and SDK/docs final grep gates. Confirmed external matrix remains a blocker until `tabula-bundles` evidence is supplied.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The relocation table depends on final subagent plugin policy/auth design, so CREATIVE/BUILD must still decide whether spawn tokens survive or are replaced; final grep could fail on intentional target-architecture prose unless docs are rewritten to avoid active legacy names.
- Open issues: External matrix rows remain `unknown/blocker`; later PLAN agents can refine artifact/update mechanics or declare PLAN CREATIVE-ready if no new gaps appear.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Creative-handoff and auth-policy boundary planner
- Work: Refined the remaining subagent spawn-policy gap into an explicit CREATIVE design fork: token-retained plugin model vs replacement-auth model. Verified current source/doc surfaces for token consumption (`PolicyEngine.CanConnect`), issuance (`generateSpawnToken`), env propagation (`ProcessManager.Spawn`), deprecated tool constants, active docs that still mention generic `api.spawn`, `TABULA_SPAWN_TOKEN`, and reserved `before_spawn`/`after_spawn`. Added a docs alignment gate so BUILD does not keep stale stable API/environment promises after bridge deletion.
- Addressed: Agent 5 risk that D1.11(b) relocation depends on final plugin policy/auth design; tightened the handoff so CREATIVE must decide ownership, invariants, message/env surface, replacement tests, and deletion list before BUILD removes kernel bridge code.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The selected subagent auth model may require external `tabula-bundles` design evidence that is unavailable in this repo; active docs may need careful wording to avoid promising a generic SDK `api.spawn` if only the subagent plugin owns child spawning.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE should produce the focused subagent plugin spawn auth/policy ownership decision before BUILD starts D1.11(b) deletion.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 7 — [CONTRIBUTE]
- Role: Build-sequencing and evidence-governance planner
- Work: Added a four-lane BUILD sequence separating repo-local safe hardening from external evidence collection, D1.11(b) kernel bridge deletion, and Phase 6 SDK/lib deletion so blocked external work cannot stall safe fixes or enable unsafe partial removals. Tightened the external migration matrix state-change criteria and added a CREATIVE readiness checklist that requires `creative-plugin-runtime.md` §7's old dead-code-keep decision to be superseded or retained explicitly before any spawn bridge deletion.
- Addressed: Agent 6 open issue that external matrix rows remain blockers and CREATIVE must make the subagent auth/policy decision actionable; clarified how BUILD should proceed if external evidence stays unavailable and how API/env/event docs must follow a single CREATIVE decision.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Repo-local hardening can proceed independently, but D1.11(b) and SDK deletion lanes may remain incomplete if `tabula-bundles` evidence or packaged SDK artifacts are unavailable; CREATIVE must avoid contradictory guidance between older archived decisions and this follow-up's deletion goals.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE should decide and document subagent auth/policy ownership, `api.spawn`/`TABULA_SPAWN_TOKEN`/`before_spawn` surface status, and a symbol-level deletion checklist.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: Creative-supersession and active-doc alignment planner
- Work: Verified the external migration matrix remains seeded with blocker rows and read the frozen plugin runtime creative doc plus active authoring/architecture docs around `api.spawn`, `TABULA_SPAWN_TOKEN`, and `before_spawn`/`after_spawn`. Added a CREATIVE supersession protocol requiring the old §7 dead-code-keep decision to be explicitly superseded or retained, plus an active-doc cleanup inventory/table so BUILD has concrete doc actions after the design decision.
- Addressed: Agent 7 risk that CREATIVE could leave contradictory frozen design text and stale stable API/env/event promises; made the docs-alignment gate more actionable by naming current active evidence and required disposition for each surface.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: CREATIVE may still decide to retain D1.11(b) as a blocker if external subagent-plugin evidence is unavailable; docs/plans may need explicit historical/superseded marking before final grep exclusions are defensible.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE should now be able to proceed with a focused supersession addendum, subagent auth/policy decision, event/API/env surface disposition, and symbol-level deletion checklist.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 9 — [CONTRIBUTE]
- Role: Cross-document consistency and BUILD wiring edge-case planner
- Work: Verified the seeded external matrix, frozen runtime/protocol/SDK creative docs, active authoring/architecture docs, and current `cmd/tabula/main.go` handler/filter wiring. Added plan refinements requiring CREATIVE to supersede the SDK creative doc's generic `api.spawn` promise as well as runtime §7, and clarified diagnostics guard listener-host wiring plus testability for warn-and-empty `kernel_tools` behavior in both `serveCmd` and `runCmd`.
- Addressed: Agent 8 open issue that `api.spawn`/token/event surfaces need a single authoritative decision; closed a hidden gap where `creative-sdk-and-distro.md` still lists SDK `spawn`, and strengthened BUILD guidance for the current `registerKernelHTTPHandlers(mux, hub)` signature and `filterKernelTools` return shape.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: If CREATIVE chooses stable generic `api.spawn`, it must expand design/test obligations substantially; same-host diagnostics allowances can be over-permissive unless BUILD keeps public-looking `Host` values rejected even with loopback `RemoteAddr`.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE should produce the supersession addendum and decide whether `api.spawn` is removed/reserved, subagent-private, or stable with full auth/policy semantics.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 10 — [CONTRIBUTE]
- Role: Top-level documentation and test-matrix cleanup planner
- Work: Followed the latest SDK/doc grep gate and found active `_pylib` references outside the prior docs/scripts scope in `README.md` and `tests/README.md`. Added Phase 6 cleanup requirements for top-level runtime-layout prose, skill-authoring/versioning wording, and the contract test matrix so packaged SDK migration does not leave stale `skills/_pylib/test_protocol.py` guidance.
- Addressed: Strengthened Agent 5/9 final grep gates by including top-level README/test docs and clarifying that internal `skills/_pylib`/`skills/_tslib` docs are compatibility evidence only until the directories are removed after green SDK rows.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Top-level docs can drift ahead of actual package artifacts if rewritten too early; keep README/test-matrix updates gated on Python SDK wheel / contract-test replacement evidence or mark target-state caveats explicitly.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs the subagent auth/API/env/event supersession decision, and BUILD must later apply top-level doc cleanup with the packaged SDK contract-test command.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 11 — [CONTRIBUTE]
- Role: SDK public-surface and contract-test cleanup planner
- Work: Verified the remaining active SDK support-dir surfaces and example packaged SDK baseline. Added a plan gate for stale public `DEFAULT_KERNEL_TOOLS` / `TOOL_PROCESS_*` constants and protocol tests in `skills/_pylib` and `skills/_tslib`, plus a specific `TABULA_SPAWN_TOKEN` helper decision for `skills/_tslib/src/paths.ts` so SDK packages do not preserve removed kernel builtin or generic child-auth promises by accident.
- Addressed: Strengthened Agent 9/10 doc/API cleanup by extending the final grep and contract gate from docs/top-level tests into SDK source and SDK parity tests; clarified that `examples/plugin-sdk-python` currently has no generic `spawn` helper and can serve as the minimal packaged-SDK baseline unless CREATIVE selects stable spawn semantics.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Legacy support dirs may need temporary deprecated constants until packaged SDK artifacts are green; BUILD must avoid creating Python parity for the TS spawn-token helper unless CREATIVE retains a stable env-token contract.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs to decide `api.spawn`/`TABULA_SPAWN_TOKEN`/event ownership and now should include SDK public constants/helpers in that decision table.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 12 — [CONTRIBUTE]
- Role: Manifest validation and external-evidence gate planner
- Work: Verified `creative-manifest-schemas.md` requires `tools[].exec` validation in both distro install and kernel boot, then checked current boot/distro code surfaces. Added a plan gate requiring strict boot skill descriptor validation for non-empty names/exec commands, duplicate-name failure, distro-side `SKILL.md` validation for installed skills/bundle components, and test coverage for invalid descriptors instead of relying on `NewHub`'s defensive empty-command skip. Updated the external migration matrix row so per-call skill migration cannot turn green from path migration alone; it also needs repo-side kernel boot or distro install validator evidence.
- Addressed: Strengthened the external `tools[].exec` migration gate from Agent 4/7 and closed a hidden gap where migrated per-call skills could still be silently absent if `exec` is missing or blank.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: Distro-side `SKILL.md` parsing may need a minimal YAML/frontmatter parser or reuse of an existing boot-generation path; BUILD should avoid requiring richer schemas than current authoring docs while still rejecting missing execution commands.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs the subagent auth/API/env/event/constants supersession decision. Later PLAN agents can likely declare PLAN CREATIVE-ready if no additional manifest/package gates appear.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 13 — [CONTRIBUTE]
- Role: Validator implementation-risk and test-fixture planner
- Work: Verified the current boot parser, distro install path, distro tests, and manifest creative doc around `tools[].exec`. Added a plan refinement requiring validation before boot tool advertisement, a reusable distro `SKILL.md` frontmatter/tool validator, intentional distro fixture migration, negative validation tests, and an atomic install-failure check so invalid bundle skills cannot partially promote plugins or lock updates.
- Addressed: Agent 12 risk that distro-side `SKILL.md` parsing might be under-specified and that migrated per-call skills could still pass from path/file presence alone. Tightened the external matrix rule to distinguish no-tool legacy placeholders from migrated per-call skills with explicit `tools[].name`/`tools[].exec` evidence.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: Minimal YAML/frontmatter parsing can be brittle if BUILD attempts to support all YAML forms without a dependency; fixture updates must avoid over-constraining no-tool/user-invocable legacy skills while still failing advertised tool entries with missing exec.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs the subagent auth/API/env/event/constants supersession decision. Later PLAN agents can likely declare PLAN CREATIVE-ready unless they find a new cross-surface gate.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 14 — [CONTRIBUTE]
- Role: Dynamic plugin catalog validation planner
- Work: Verified the current long-lived plugin registration/update path (`MarkRegistered`, `replacePluginTools`, `handlePluginProtocolMessage`, manifest validation, runtime tests) and found a matching validation gap for plugin-provided catalogs. Added a BUILD gate requiring reusable validation for initial `register` and `update_tools` payloads: non-empty/unique tool names, deadline handling, subscription event validation, register-time fail-closed behavior, and atomic rejection of invalid dynamic updates without dropping prior tools. Updated the external migration matrix so hook/MCP/driver-subagent/gateway plugin rows require catalog/subscription validation evidence, not just path migration or process-start smoke tests.
- Addressed: Extended Agents 12/13 `tools[].exec` validation concerns to the plugin side of the same dynamic tool-discovery boundary and closed a hidden risk where invalid external plugins could appear registered while silently skipping tools or partially mutating `Hub.toolExec`.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: BUILD must reconcile current last-writer-wins-with-WARN plugin collision behavior with the new validation gate; if retained, it needs explicit tests/log assertions so collisions are intentional rather than silent. The final subscription event validator depends on the CREATIVE decision for `before_spawn`/`after_spawn` reserved vs plugin-emitted vs removed semantics.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs the subagent auth/API/env/event/constants supersession decision. Later PLAN agents can likely declare PLAN CREATIVE-ready after confirming the new plugin catalog validation gate is sufficient.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 15 — [CONTRIBUTE]
- Role: Plugin protocol negative-path and fail-closed planner
- Work: Verified actual plugin runtime/handle/protocol/dispatch code and the frozen protocol negative-path spec after Agent 14's catalog gate. Added a plan gate requiring register catalog failures to be non-restartable and state-clean, and requiring post-register inbound message validation for `event_reply`, `tool_result`, `send`, `log`, and `update_tools` so malformed plugin replies do not become permissive hook/tool outcomes. Updated the external matrix rows and gating summary to require negative inbound-message validation evidence before long-lived plugin migrations can go green.
- Addressed: Strengthened Agent 14's dynamic catalog validation by covering adjacent inbound protocol response paths, especially the current risk that unknown `event_reply.action` logs a warning but defaults to pass after delivering the pending hook callId.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`
- Risks: BUILD will need to choose exact mechanics for invalid event replies (explicit deny vs allowing existing timeout fail-closed path) without double-delivering hook results; register validation may need to live partly in `internal/kernel/plugin` and partly in parent `kernel` if subscription event validation depends on `HookEvents` after CREATIVE resolves `before_spawn`/`after_spawn`.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE still needs the subagent auth/API/env/event/constants supersession decision and should include subscription/event validation semantics in the final event-surface table. Agent 16 can likely declare PLAN CREATIVE-ready unless it finds a new cross-surface gate.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 16 — [CONTRIBUTE]
- Role: PLAN closeout and security-handoff planner
- Work: Reviewed the current handoff, latest plan log, seeded external matrix, and referenced creative/source files for remaining contradictions. Added a final CREATIVE-ready contract to `tasks.md` that names the required supersession decisions, BUILD lane gates, validation expectations, and task-specific SECURITY checks so the next phase can proceed without another broad PLAN pass.
- Addressed: Closed Agent 15's “can likely declare PLAN CREATIVE-ready” handoff by confirming the remaining work is a CREATIVE design-decision problem, not another PLAN discovery gap. Preserved external matrix rows as blockers for D1.11(b)/SDK deletion and added explicit SECURITY review targets for diagnostics locality, plugin fail-closed behavior, SDK/doc spawn-token surfaces, atomic distro validation, and matrix-dependent cleanup.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: CREATIVE may still retain D1.11(b) or SDK/lib deletion as blockers if external evidence remains unavailable; SECURITY checks may need to degrade with rationale if BUILD only completes the repo-local hardening lane.
- Open issues: External matrix rows remain `unknown/blocker`; CREATIVE must now produce the dated supersession addendum and selected subagent auth/API/env/event/constants design before BUILD can delete bridge code or SDK compatibility paths.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 1 — [CONTRIBUTE] (reopened PLAN cycle)
- Role: Reopened-scope baseline planner / evidence-gate auditor
- Work: Validated active metadata (`Intent=implement`, `Category=deep`) and re-established the PLAN baseline against the current repo snapshot rather than relying only on historical logs. Confirmed repo-local hardening already present for empty builtin metadata, local-only plugin diagnostics, boot/distro and plugin catalog validation, and active doc/SDK spawn-surface cleanup; converted these into regression targets and separated them from still-blocked external evidence lanes.
- Addressed: N/A — first agent for this reopened PLAN cycle
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The task reuses the same task ID as an archived historical cycle, so later agents must distinguish current reopened planning notes from prior completed logs; external matrix rows remain `unknown/blocker`, making D1.11(b) bridge deletion and Phase 6 SDK/lib removal unsafe without new external artifacts; diagnostics locality must not be weakened for proxied/public deployments.
- Open issues: Later PLAN agents should refine any deltas from the archived cycle only if new external evidence exists, otherwise prepare CREATIVE/BUILD handoff that preserves repo-local regression tests and keeps deletion lanes blocked; verify whether the existing external matrix should be updated with new evidence or explicitly carried forward as still blocker.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE] (reopened PLAN cycle)
- Role: External-evidence governance and BUILD triage planner
- Work: Re-read the required Memory Bank context, the reopened Agent 1 handoff, and scoped actual files for diagnostics locality, stale kernel-tool filtering, D1.11(b) skipped/dead-code surfaces, distro SDK preservation, and the external bundle migration matrix. Added a current-cycle carry-forward decision to `tasks.md`: the existing external matrix remains canonical, no row may change from `unknown/blocker` without exact external path/command/commit or release evidence, and BUILD must treat no-new-evidence runs as regression-preservation only.
- Addressed: Agent 1 open issue to verify whether the existing external matrix should be updated or explicitly carried forward. Confirmed no external paths/artifacts were present in the matrix, so D1.11(b) and Phase 6 deletion lanes remain blocked while repo-local hardening stays a regression target.
- Files: modified `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: none new; the main risk remains accidentally using archived completed work as evidence for current external migrations. BUILD should update the matrix before deleting any compatibility bridge and should not weaken local-only diagnostics for proxy/header trust.
- Open issues: External matrix rows remain `unknown/blocker`; later PLAN agents should only add new deltas or newly supplied external evidence. Without either, proceed toward compact CREATIVE/BUILD handoff preserving blockers.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [DECLINE] (reopened PLAN cycle)
- Role: N/A
- Work: 2026-04-27 — Re-read required Memory Bank context and the reopened PLAN entries, then performed scoped actual-file checks for the external matrix, diagnostics locality guard, stale `kernel_tools` filtering, D1.11(b) skipped/dead-code surfaces, and distro/SDK compatibility preservation. Declined because Agent 2's current-cycle triage remains accurate: no new external evidence or repo-local planning gap emerged, and the remaining work is intentionally evidence-gated.
- Addressed: Confirmed metadata is complete (`Intent=implement`, `Category=deep`), the matrix rows are still `unknown/blocker`, `/internal/snapshot/plugins` remains guarded by loopback `RemoteAddr` plus local/loopback `Host`, stale kernel-tool selections warn-and-empty, D1.11(b) bridge code/skipped tests remain present by design, and distro `_pylib`/`_tslib` plus inline `_*` compatibility surfaces are still preserved until package evidence exists.
- Files: none
- Risks: none new; the main risk remains accidental deletion or completion claims based on archived repo-local work rather than current external `tabula-bundles` artifacts.
- Open issues: External matrix rows remain `unknown/blocker`; later PLAN agents should only add a new delta or concrete external evidence, otherwise keep the compact CREATIVE/BUILD handoff with D1.11(b) and Phase 6 deletion lanes blocked.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 4 — [DECLINE] (reopened PLAN cycle)
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context and reopened PLAN entries, then performed scoped verification of the external migration matrix, diagnostics guard/filtering code, D1.11(b) skipped/dead-code surfaces, and distro SDK compatibility preservation. Declined because no new external artifact evidence or unresolved planning gap emerged beyond the already documented gates.
- Addressed: Confirmed Agent 2/3 current-cycle triage remains valid: all required external matrix rows are still `unknown/blocker`; `/internal/snapshot/plugins` remains local-only guarded with wildcard/IPv6/public-host test coverage; stale `kernel_tools` selections still warn-and-empty; kernel spawn-token/MaxChildren/depth bridge code and skipped tests are retained by design; distro `_pylib`/`_tslib` preserve behavior and inline sibling `_*` support-dir compatibility remain present until SDK/package rows turn green.
- Files: none
- Risks: none new; the standing risk remains accidental D1.11(b) or Phase 6 deletion/completion claims based on archived repo-local hardening rather than current external `tabula-bundles` PRs, release artifacts, or validation commands.
- Open issues: External matrix rows remain `unknown/blocker`; later PLAN agents should only contribute if they have concrete new external evidence or a new current-repo delta, otherwise keep the compact CREATIVE/BUILD handoff with D1.11(b) and Phase 6 deletion lanes blocked.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 5 — [DECLINE] (reopened PLAN cycle)
- Role: N/A
- Work: 2026-04-27 — Re-read the required Memory Bank context and latest reopened PLAN entries, then performed only scoped checks named by the handoff: the external migration matrix, diagnostics/kernel-tool surfaces, D1.11(b) spawn bridge/skipped-test surfaces, and distro SDK preservation code. Declined because the current-cycle plan is already actionable and no new external artifact evidence or current-repo planning delta appeared.
- Addressed: Confirmed Agent 2/4 carry-forward triage remains accurate: every external matrix row is still `unknown/blocker`; `/internal/snapshot/plugins` and stale `kernel_tools` handling remain regression targets rather than planning gaps; D1.11(b) bridge code/skipped tests and Phase 6 SDK/distro compatibility surfaces remain intentionally preserved until required external rows turn green or owner-approved not-applicable.
- Files: none
- Risks: none new; the standing risk remains unsafe deletion or completion claims based on archived repo-local hardening instead of concrete external `tabula-bundles` paths, commits/PRs, release artifacts, and validation commands.
- Open issues: External matrix rows remain `unknown/blocker`; later PLAN agents should only contribute for concrete new external evidence or a new current-repo delta, otherwise preserve the compact CREATIVE/BUILD handoff with D1.11(b) and Phase 6 deletion lanes blocked.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

### Planning Progress

- Requirement: Reopen PLAN for grouped skill/plugin architecture follow-ups while preserving historical completed work and current evidence gates.
- Approach: Read required Memory Bank files first, validated metadata, then scoped project reads/searches for `cmd/tabula/kernel.tools.json`, `/internal/snapshot/plugins`, `filterKernelTools`, spawn-token/MaxChildren dead-code surfaces, distro runtime preserve behavior, legacy SDK directories, and the external bundle migration matrix. Added a current-cycle PLAN baseline to `tasks.md` that distinguishes already-landed repo-local hardening from externally gated deletion lanes.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: `tasks.md` Phase Status block remains router-managed (`PLAN: IN_PROGRESS`); no optional visual rules were needed; no project source files were modified during PLAN.

- Requirement: Close the reopened Agent 1 external-matrix carry-forward question and prevent unsafe deletion from archived evidence.
- Approach: Verified the canonical external matrix still contains only `unknown/blocker` rows, then added current-cycle triage rules requiring matrix evidence updates before D1.11(b) bridge deletion or Phase 6 SDK/lib removal. Scoped source reads confirmed the guarded diagnostics/filtering baselines and preserved compatibility surfaces are still present.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: `tasks.md` Phase Status block remains router-managed (`PLAN: IN_PROGRESS`); no optional visual rules were needed; no project source files were modified during PLAN.

- Requirement: Plan the post-reflection skill/plugin architecture follow-ups without modifying project source during PLAN.
- Approach: Used required Memory Bank files first, then scoped reads/searches of `cmd/tabula`, `internal/kernel`, docs, distro installer, SDK legacy directories, and selected reflection evidence. Added a task-scoped implementation sequence and validation gates to `tasks.md`.
- Files modified: `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`.
- Verification notes: Confirmed `tasks.md` phase status remains router-managed (`PLAN: IN_PROGRESS`) and task metadata is complete; no optional visual rules were needed; no project source files were changed.

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: Repo-local safe hardening implementer
- Work: Emptied stale `cmd/tabula/kernel.tools.json`, changed legacy `kernel_tools` handling to warn-and-empty, and added local-only enforcement for `/internal/snapshot/plugins` with focused tests and docs. Verified with `go test ./cmd/tabula ./internal/kernel`.
- Addressed: Implemented the first safe BUILD lane and closed the stale builtin metadata and unauthenticated plugin snapshot exposure gaps.
- Files: modified `cmd/tabula/kernel.tools.json`, `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `docs/PLUGIN_AUTHORING.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Evidence-gated deletion lanes remain blocked by external `tabula-bundles` rows; active docs now reflect local-only snapshot exposure.

#### Agent 2 — [CONTRIBUTE]
- Role: Boot/distro validator implementer
- Work: Implemented boot and distro `tools[].exec` validation so advertised skill/tool catalogs are validated before dispatch and during staging, with atomic failure coverage. Verified with `go test ./cmd/tabula` and `PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py`.
- Addressed: Closed the per-call skill validation lane and strengthened distro install safety for advertised tool entries.
- Files: modified `cmd/tabula/main.go`, `cmd/tabula/main_test.go`, `tools/tabula-distro/src/tabula_distro/install.py`, `tools/tabula-distro/tests/test_install.py`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`, `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`

#### Agent 3 — [CONTRIBUTE]
- Role: Plugin protocol validator
- Work: Added dynamic plugin catalog validation so `register`/`update_tools` payloads are normalized and rejected before unsafe mutation, and malformed `event_reply`/`tool_result` paths no longer deliver permissive outcomes. Verified with `go test ./internal/kernel/plugin ./internal/kernel`.
- Addressed: Closed the plugin protocol validation lane and reduced unsafe mutation/dispatch risk for runtime catalogs.
- Files: modified `internal/kernel/plugin/*`, `internal/kernel/*`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`

#### Agent 4 — [CONTRIBUTE]
- Role: SDK/documentation surface aligner
- Work: Removed stable generic `api.spawn`/`TABULA_SPAWN_TOKEN` promises from active docs and temporary SDK surfaces, removed the public TS `tabulaSpawnToken()` helper, and aligned tests so `DEFAULT_KERNEL_TOOLS` is empty while stale builtin names remain deprecated compatibility strings only. Verified with `python3 -m unittest skills/_pylib/test_protocol.py`, `bun test` in `skills/_tslib`, and `go test ./cmd/tabula ./internal/kernel`.
- Addressed: Reconciled active docs and temporary SDK contracts with the CREATIVE spawn-auth decision.
- Files: modified `README.md`, `tests/README.md`, `docs/ARCHITECTURE.md`, `docs/SKILL_AUTHORING.md`, `docs/PLUGIN_AUTHORING.md`, `skills/_pylib/*`, `skills/_tslib/*`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`

#### Agent 5 — [CONTRIBUTE]
- Role: Active-doc cleanup implementer
- Work: Cleaned top-level docs to match zero-default kernel tools, plugin-owned subagents, and packaged SDK distribution while preserving evidence-gated blockers. Verified grep checks plus `python3 -m unittest skills/_pylib/test_protocol.py`.
- Addressed: Finished the active top-level docs cleanup lane without touching gated compatibility bridges.
- Files: modified `README.md`, `tests/README.md`, `docs/ARCHITECTURE.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`

#### Agent 6 — [DECLINE]
- Role: Evidence gate verifier
- Work: Re-verified the build handoff and declined further changes because remaining D1.11(b)/Phase 6 work is externally gated.
- Addressed: Confirmed no safe additional BUILD edits remained in the repo-local lane.

#### Agent 7 — [DECLINE]
- Role: External-evidence blocker confirmer
- Work: Re-read the required Memory Bank context and external matrix and agreed the remaining bridge deletions are blocked by `unknown/blocker` rows.
- Addressed: Confirmed the D1.11(b) and Phase 6 deletion lanes remain blocked.

#### Agent 8 — [DECLINE]
- Role: External-evidence blocker confirmer
- Work: Re-verified the handoff, matrix, and scoped files and declined because remaining bridge deletions are still blocked by unknown external evidence.
- Addressed: No source changes were made; evidence gating remains intact.

#### Agent 9 — [DECLINE]
- Role: External-evidence blocker confirmer
- Work: Re-read the required context and verified active docs/SDK surfaces and matrix blocker state, then declined because deletion work remains blocked.
- Addressed: Confirmed no safe additional BUILD changes beyond the completed lanes.

### ARCHIVE Phase — DONE (2026-04-27)
- ✅ Archive artifact created: `memory-bank/archive/archive-skill-plugin-architecture-followups.md`
- ✅ tasks.md reset to "No active tasks"
- ✅ activeContext.md reset to "No active context"
- ✅ progress.md preserved as historical log
- ✅ Creative and reflection artifacts preserved as permanent references

### ARCHIVE Phase — DONE (2026-04-27, conditional archive correction)
- ✅ Archive artifact created/updated: `memory-bank/archive/archive-skill-plugin-architecture-followups.md`
- ✅ tasks.md reset to "No active tasks"
- ✅ activeContext.md reset to "No active context"
- ✅ progress.md preserved as historical log
- ✅ Creative and reflection artifacts preserved as permanent references
- ⚠️ Archive disposition is conditional: repo-local hardening lanes are closed, while external `tabula-bundles`, D1.11(b) bridge deletion, and Phase 6 SDK/lib relocation remain evidence-gated blockers for future tasks.
