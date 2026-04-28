# System Archive: Tabula Skill/Plugin Architecture Follow-ups
Date Archived: 2026-04-27
Status: CONDITIONALLY ARCHIVED — REPO-LOCAL LANES CLOSED; EXTERNAL EVIDENCE BLOCKERS OPEN

## 1. System Overview
- Description: Level 4 follow-up archive for the grouped Tabula skill/plugin architecture migration cleanup. The archived work completed the repo-local safe hardening lane: removed stale live kernel builtin tool advertisement, guarded the internal plugin diagnostics snapshot endpoint with locality checks, added boot/distro/plugin catalog validation, hardened malformed plugin reply handling, and aligned active docs/temporary SDK surfaces with the subagent-plugin-owned spawn design. The original grouped scope also included external `tabula-bundles` migrations, D1.11(b) spawn bridge deletion, and Phase 6 SDK/lib physical relocation; those remain intentionally preserved as open/evidence-gated blockers because required external evidence rows are still `unknown/blocker`.
- Complexity: Level 4
- Duration: 2026-04-27 to 2026-04-27
- Business objectives met:
  - Reduced risk of advertising removed kernel builtin process tools as live LLM tools.
  - Converted `/internal/snapshot/plugins` locality from an operational assumption into enforced request validation.
  - Strengthened migration safety for per-call skill manifests and long-lived plugin dynamic catalogs.
  - Prevented malformed plugin replies from becoming permissive hook/tool outcomes.
  - Cleaned active docs and temporary SDK contracts so they no longer promise stable generic `api.spawn`, common `TABULA_SPAWN_TOKEN`, or non-empty default kernel tools.
  - Preserved compatibility bridges rather than deleting them without external replacement evidence.

## 2. Architecture Documentation
- Architecture style: Evidence-gated incremental migration from kernel-owned builtins toward dynamic skill/plugin ownership, with a minimal kernel boundary and external subagent plugin ownership for child-spawn policy.
- Key architectural decisions:
  - Kernel builtin tool metadata is empty by default; dynamic skills/plugins are the live tool source.
  - Explicit stale `kernel_tools` boot selections warn and remain empty rather than re-advertising removed builtins.
  - `/internal/snapshot/plugins` is local-only by loopback `RemoteAddr` plus loopback/local `Host`; forwarded headers are not trusted.
  - Long-lived plugin `register`/`update_tools` catalogs are validated before registry, hook, or dispatch mutation.
  - Malformed or unknown plugin `event_reply` / `tool_result` inputs are rejected or released instead of synthesized into successful outcomes.
  - Subagent spawning is owned by the external subagent plugin under a replacement-auth model; no stable generic SDK `spawn` API was introduced.
  - D1.11(b) kernel spawn bridge and Phase 6 SDK support directories remain until external evidence gates turn green or are owner-approved not-applicable.
- Component structure:
  - `cmd/tabula`: boot filtering, skill descriptor validation, HTTP diagnostics guard, and tests.
  - `internal/kernel/plugin`: plugin catalog/protocol validation.
  - `internal/kernel`: dynamic tool registration, plugin runtime integration, hook/reply handling, and retained spawn bridge surfaces.
  - `tools/tabula-distro`: staged bundle skill manifest validation and atomic install behavior.
  - `skills/_pylib` and `skills/_tslib`: temporary compatibility SDK support dirs retained pending Phase 6 package artifacts.
  - `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`: external evidence gate source.
- Integration points:
  - Kernel boot config `skills`/legacy `tools` descriptors.
  - Distro bundle installs and `SKILL.md` advertised `tools[]` validation.
  - Long-lived plugin JSON-RPC `register`, `update_tools`, `event_reply`, and `tool_result` paths.
  - Internal HTTP diagnostics endpoint `/internal/snapshot/plugins`.
  - External `tabula-bundles` migration evidence for hook/MCP/driver-subagent/gateway plugins, per-call skills, SDK packages, and `_lib` install paths.

## 3. Design Decisions
- Summary of creative phase outcomes:
  - `creative-plugin-runtime.md` §13 superseded the older dead-code-keep target with a replacement-auth, subagent-plugin-owned model for child spawning.
  - Generic `api.spawn` is reserved/future-only and is not part of the packaged SDK baseline.
  - `TABULA_SPAWN_TOKEN` is not a common runtime/public SDK contract; any future literal use must be subagent-private and externally evidenced.
  - `before_spawn` / `after_spawn` are not kernel-emitted generic events after bridge removal; they are reserved/subagent-owned unless a concrete external plugin contract proves otherwise.
  - Temporary SDK surfaces may keep deprecated builtin literals only as compatibility strings, with `DEFAULT_KERNEL_TOOLS` empty.
  - SDK packaging target remains external wheel/tarball artifacts under `tabula-bundles/_lib`, but physical removal is blocked until artifacts and install validation exist.
- Links to creative documents:
  - `memory-bank/creative/creative-plugin-runtime.md`
  - `memory-bank/creative/creative-sdk-and-distro.md`
  - `memory-bank/creative/creative-plugin-protocol.md`
  - `memory-bank/creative/creative-manifest-schemas.md`

## 4. Implementation Details
- Phased implementation summary:
  - Phase 1: Repo-local safe hardening removed live stale builtin metadata, added warn-and-empty stale boot selection behavior, and guarded `/internal/snapshot/plugins`.
  - Phase 2: Boot and distro validation now reject advertised skill tool entries with missing/blank names or `exec` commands before advertisement/promotion.
  - Phase 3: Dynamic plugin catalog and inbound reply validation now fail closed or remain side-effect-free on invalid data.
  - Phase 4: Active authoring docs and temporary SDK public surfaces were aligned to the subagent-plugin-owned spawn decision.
  - Phase 5: Top-level docs were cleaned where safe without claiming external artifact completion.
  - Phase 6: Further deletion work was declined because external rows remained `unknown/blocker`; bridge/support-dir compatibility was intentionally retained.
- Primary components:
  - Kernel boot metadata and HTTP handlers in `cmd/tabula`.
  - Plugin runtime/catalog validation in `internal/kernel/plugin` and parent kernel plugin integration files.
  - Distro install validation in `tools/tabula-distro`.
  - Temporary Python/TypeScript SDK support dirs under `skills/_pylib` and `skills/_tslib`.
  - Active documentation in `README.md`, `tests/README.md`, `docs/ARCHITECTURE.md`, `docs/SKILL_AUTHORING.md`, and `docs/PLUGIN_AUTHORING.md`.
- Technology stack:
  - Go kernel/runtime and tests.
  - Python distro tooling and unittest validation.
  - TypeScript/Bun temporary SDK tests.
  - Markdown Memory Bank evidence and design artifacts.
- Key files and directories:
  - `cmd/tabula/kernel.tools.json`
  - `cmd/tabula/main.go`
  - `cmd/tabula/main_test.go`
  - `internal/kernel/plugin/catalog_validation.go`
  - `internal/kernel/plugin/*_test.go`
  - `internal/kernel/plugin_tools.go`
  - `internal/kernel/plugin_runtime.go`
  - `tools/tabula-distro/src/tabula_distro/install.py`
  - `tools/tabula-distro/tests/test_install.py`
  - `skills/_pylib/`
  - `skills/_tslib/`
  - `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`

## 5. Testing Documentation
- Testing strategy: Focused validation of completed repo-local lanes plus SECURITY review; deletion lanes were not tested as complete because they remained externally gated.
- Test coverage:
  - `go test ./cmd/tabula ./internal/kernel`
  - `go test ./cmd/tabula`
  - `go test ./internal/kernel/plugin ./internal/kernel`
  - `PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py`
  - `python3 -m unittest skills/_pylib/test_protocol.py`
  - `bun test` in `skills/_tslib`
  - Grep gates for active docs around `_pylib`/`_tslib`, `requires-kernel-tools`, spawn-token, and default-tool wording as reported in BUILD logs.
- Performance test results: No performance-specific test suite was required for the completed safe hardening lane; no performance regressions were reported.
- SECURITY Phase 4.7 report: `memory-bank/security/security-skill-plugin-architecture-followups.md`
- SECURITY verdict / attempts: PASSED, 1
- SECURITY evidence directory: `memory-bank/security/artifacts/skill-plugin-architecture-followups/attempt-1/`
- REFLECT second-opinion report: `memory-bank/reflection/reflection-skill-plugin-architecture-followups-second-opinion.md`
- REFLECT second-opinion verdict: APPROVED
- Known limitations:
  - External `tabula-bundles` matrix rows remain `unknown/blocker`.
  - D1.11(b) spawn-token/MaxChildren/depth bridge code and skipped kernel spawn tests remain intentionally present.
  - Phase 6 physical SDK/lib relocation remains incomplete; `skills/_pylib`, `skills/_tslib`, distro preserve behavior, and inline `_*` compatibility remain.
  - SECURITY dependency review was degraded for Python because `pip-audit` was unavailable; `.opencode` moderate npm audit findings pre-existed and were not introduced by BUILD.
  - Plugin/boot logging remains an unredacted trusted-boundary concern.

## 6. Deployment Information
- Deployment approach: Changes are repo-local source, tests, docs, and Memory Bank artifacts. The completed hardening applies when the kernel boots with the updated catalog/filtering, plugin validation, distro validation, and internal diagnostics guard.
- Configuration requirements:
  - Do not expose `/internal/snapshot/plugins` through public ingress; remote diagnostics require a future authenticated design.
  - Stale explicit `kernel_tools` selections should be treated as deprecated and non-advertising.
  - External bundle rows must be updated with concrete source/target paths, commands, and linked evidence before compatibility bridge deletion.
- Environment dependencies:
  - Go toolchain for kernel tests.
  - Python 3 for distro/support-dir tests.
  - Bun for TypeScript support-dir tests.
  - Future Phase 6 requires external Python wheel and TypeScript tarball artifacts in or linked from `tabula-bundles`.

## 7. Maintenance Guide
- Key operational procedures:
  - Use `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md` as the gate before removing retained compatibility bridges.
  - Keep `/internal/snapshot/plugins` bound to local-only/internal access; do not bypass the guard with trusted forwarded headers.
  - Treat `skills/_pylib` and `skills/_tslib` as temporary compatibility surfaces until packaged SDK artifacts are validated.
  - Preserve D1.11(b) skipped-test relocation mapping until plugin-side replacement tests exist.
- Monitoring points:
  - Warnings from stale/unknown `kernel_tools` boot selections.
  - Plugin registration/update validation failures and malformed reply warnings.
  - Distro install failures from invalid advertised `SKILL.md` tool descriptors.
  - SECURITY audit warnings for dependency scanners and unredacted plugin/boot logs.
- Common troubleshooting:
  - If plugin diagnostics return `403`, verify both `RemoteAddr` and `Host` are loopback/local; forwarded headers are intentionally ignored.
  - If tools do not appear after boot/install, validate non-empty unique `tools[].name` and non-empty `tools[].exec` for per-call skills.
  - If dynamic plugin tools disappear or updates fail, inspect `register`/`update_tools` catalog validation errors before assuming runtime failure.
  - Do not delete spawn bridge or SDK support dirs just because docs are aligned; require matrix rows to be green or owner-approved not-applicable.

## 8. Reflection & Strategic Insights
- Link: `memory-bank/reflection/reflection-skill-plugin-architecture-followups.md`
- Top strategic insights:
  1. Evidence-gated architecture migrations should make external rows first-class acceptance criteria rather than informal caveats.
  2. Sensitive diagnostics should be protected by testable locality/auth controls, not by documentation-only assumptions.
  3. Public SDK/docs surfaces must be cleaned before bridge deletion so accidental APIs are not frozen while replacement evidence is still pending.

## 9. Known Issues & Future Roadmap
- Deferred items:
  - Fill external `tabula-bundles` rows for hook plugins, MCP plugin, driver/subagent plugins, gateway plugins, per-call skills, Python SDK wheel, TypeScript SDK tarball, and bundle `_lib` install paths.
  - Relocate spawn/depth/MaxChildren/auth/lifecycle coverage to plugin-side tests, then remove D1.11(b) kernel bridge code and skipped kernel spawn tests.
  - Complete Phase 6 SDK/lib relocation after package artifacts are available: remove `skills/_pylib`, `skills/_tslib`, distro preserve behavior, inline sibling `_*` compatibility, and script/test compatibility references.
- Future enhancements:
  - Add release CI dependency auditing for Go, Python SDK artifacts, and JS/TS bundle lockfiles with a fallback when `pip-audit` is unavailable.
  - Define plugin/boot stderr and plugin `log` redaction guidance or runtime redaction controls for sensitive operator environments.
  - Design authenticated remote diagnostics if non-local plugin snapshots are required.
- Technical debt:
  - Retained D1.11(b) spawn-token/depth/MaxChildren bridge code and skip helpers.
  - Temporary in-repo SDK support directories and script/distro compatibility paths pending external SDK artifacts.
  - Historical Memory Bank and plan docs retain old migration context; active docs should remain the normative source for current behavior.

## 10. Archive Notes
- Task ID reused from `memory-bank/tasks.md`: `skill-plugin-architecture-followups`.
- Task Base Commit reused from `memory-bank/activeContext.md`: `48f84f8aa981c575259d42353557b48a305b3a0a`.
- Human docs update check: `git diff --name-only 48f84f8aa981c575259d42353557b48a305b3a0a..HEAD` returned no repo-root whitelist documentation path, and no complete soft-doc update request was needed during ARCHIVE. No non-Memory-Bank docs were edited in ARCHIVE.
- Archive disposition: conditionally archived. The Memory Bank task is closed for the completed repo-local hardening lanes, while unresolved external migration / D1.11(b) / Phase 6 SDK-lib deletion work is preserved as explicit open blockers and future roadmap items, not represented as completed deletion work.
