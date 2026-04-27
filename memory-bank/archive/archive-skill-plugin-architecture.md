# System Archive: Tabula Skill/Plugin Architecture
Date Archived: 2026-04-27
Status: COMPLETED & ARCHIVED

## 1. System Overview
- Description: Implemented the in-repository foundation for Tabula's split extension architecture: skills remain per-call tool providers, while plugins are long-lived supervised processes with their own manifest, runtime protocol, diagnostics, and distro-install support.
- Complexity: Level 4
- Duration: 2026-04-26 through 2026-04-27
- Business objectives met:
  - Reduced the default LLM-visible kernel builtin tool surface.
  - Added supervised plugin infrastructure for future hooks, MCP bridges, drivers, gateways, and long-lived integrations.
  - Added mixed skill/plugin distribution support with backward-compatible lock migration.
  - Added authoring documentation and a runnable Python reference plugin/SDK.

## 2. Architecture Documentation
- Architecture style: minimal kernel with explicit extension lifecycles; per-call skills and supervised plugin processes share a unified dispatch table but retain separate lifecycle/supervision semantics.
- Key architectural decisions:
  - Kernel builtin tools (`shell_exec`, `process_spawn`, `process_kill`, `process_list`) were removed from live dispatch; dynamic skill/plugin dispatch is the active path.
  - Kernel and plugin protocols are versioned separately via `ProtocolVersion` and `PluginProtocolVersion`.
  - Kernel↔plugin communication uses UTF-8 NDJSON over stdio with explicit `register_request`, `register`, `tool_call`, `tool_result`, `event`, `event_reply`, `send`, `log`, `update_tools`, and `shutdown` semantics.
  - `HookSubscriber` provides one lifecycle-neutral hook dispatch path for clients and plugin handles without making plugins pseudo-clients.
  - `Hub.RegisterPlugin` / `Hub.LoadPlugins` use a builder pattern, preserving `NewHub` callsite stability.
  - `CanSpawn` / spawn-token / MaxChildren code is intentionally dead-code-kept until the external subagent plugin restores those invariants.
- Component structure:
  - `internal/kernel/` — Hub integration, dynamic dispatch, hook subscriber abstraction, plugin registration, plugin diagnostics, boot integration.
  - `internal/kernel/plugin/` — manifest parsing, NDJSON protocol types, handle/registry, runtime spawning, process-group termination, supervisor/backoff.
  - `examples/plugin-sdk-python/` and `examples/plugin-hello/` — minimal SDK and live protocol reference.
  - `tools/tabula-distro/` — mixed skill/plugin install, bundle components, lock v2 migration, plugin update support.
  - `docs/` — updated authoring, architecture, distro, philosophy, and config docs.
- Integration points:
  - Boot config supports `skills` and `plugins`, with legacy `tools` fallback.
  - Plugins register tools into the shared dispatch table and subscriptions into hook dispatch.
  - `/internal/snapshot/plugins` exposes plugin lifecycle diagnostics.
  - Distro tooling reads `plugin.toml`, `SKILL.md`, and mixed `bundle.toml` component lists.

## 3. Design Decisions
- Summary of creative phase outcomes:
  - Protocol: NDJSON framing, explicit version handshake, config via `register_request`, negative-path handling, tool deadlines, restart policy, and plugin-owned child process cleanup.
  - Runtime: `HookSubscriber`, builder-style plugin loading, source-tagged tool dispatch map, separate plugin snapshot endpoint, shared reaper bookkeeping, and D1.11(b) dead-code migration bridge.
  - Manifests: `plugin.toml` required schema, flat `bundle.components`, `SKILL.md tools[].exec`, and removal of `requires-kernel-tools` as a meaningful contract.
  - SDK/distro: in-repo Python SDK/reference plugin for Phase 3, future bundled Python wheel/TS tarball path, lock v2 migrate-on-load, and metrics-via-log convention.
- Links to creative documents:
  - `memory-bank/creative/creative-plugin-protocol.md`
  - `memory-bank/creative/creative-plugin-runtime.md`
  - `memory-bank/creative/creative-manifest-schemas.md`
  - `memory-bank/creative/creative-sdk-and-distro.md`

## 4. Implementation Details
- Phased implementation summary:
  - Phase 1: kernel cleanup removed live builtin dispatch, introduced `SkillExec`, repaired dynamic skill coverage, and marked spawn-token/MaxChildren dead-code bridge sites.
  - Phase 2: plugin runtime, handle/registry, manifest parser, supervisor/backoff, Hub registration, hook/tool dispatch bridge, diagnostics, and plugin `send` bus routing landed.
  - Phase 3: Python SDK and `plugin-hello` reference plugin landed with live E2E coverage, including child-process cleanup evidence.
  - Phase 4: distro tooling gained standalone plugins, mixed bundle components, lock v2 with v1 migrate-on-load, and update targeting.
  - Phase 7: docs were updated for plugin authoring, skill authoring, architecture, distro, config, and philosophy alignment.
- Primary components:
  - Kernel dynamic tool dispatch and hook engine integration.
  - Plugin runtime/supervisor and diagnostics.
  - Python plugin SDK and reference plugin.
  - Distro manifest/install/lock/update support.
  - Human-facing docs for authors and maintainers.
- Technology stack:
  - Go kernel/runtime and tests.
  - Python SDK/reference plugin and `tabula-distro` tooling.
  - TOML parsing via `github.com/BurntSushi/toml v1.5.0`.
  - NDJSON stdio protocol.
  - Bun/TypeScript packaging strategy documented for external bundle migration.
- Key files and directories:
  - `internal/kernel/protocol.go`, `tool_service.go`, `process_manager.go`, `hook_subscriber.go`, `handle_hooksub.go`, `plugin_runtime.go`, `plugin_tools.go`, `snapshot.go`.
  - `internal/kernel/plugin/`.
  - `examples/plugin-sdk-python/`, `examples/plugin-hello/`.
  - `tools/tabula-distro/src/tabula_distro/` and tests.
  - `docs/PLUGIN_AUTHORING.md`, `docs/SKILL_AUTHORING.md`, `docs/ARCHITECTURE.md`, `docs/DISTROS.md`, `docs/distro-config.md`, `docs/PHILOSOPHY.md`.

## 5. Testing Documentation
- Testing strategy: incremental unit/integration coverage across kernel, plugin runtime, reference plugin, and distro tooling; final SECURITY review for Level 4 gate.
- Test coverage:
  - Kernel/plugin/cmd Go tests repeatedly passed during BUILD.
  - `go vet ./...` and `go build ./...` passed in BUILD validation.
  - Reference plugin live E2E validated real spawn/register/tool/event/send/snapshot/shutdown behavior.
  - Distro tooling passed 45 unittest cases plus compile checks after mixed component support.
- Performance test results: no quantitative benchmark collected; runtime safeguards include per-tool deadlines, line-size caps, restart/backoff windows, and hook timeout semantics.
- QA report: skipped by workflow (`QA: SKIPPED` in `tasks.md`); runtime evidence is in BUILD logs and SECURITY report.
- SECURITY Phase 4.7 report: `memory-bank/security/security-skill-plugin-architecture.md`
- SECURITY verdict / attempts: PASSED, 1
- SECURITY evidence directory: `memory-bank/security/artifacts/skill-plugin-architecture/attempt-1/`
- REFLECT second-opinion report: `memory-bank/reflection/reflection-skill-plugin-architecture-second-opinion.md`
- REFLECT second-opinion verdict: APPROVED
- Known limitations:
  - External `tabula-bundles` migrations remain outside this repository.
  - D1.11(b) spawn-token/MaxChildren dead-code bridge remains until subagent plugin GA.
  - In-repo `_pylib`/`_tslib` relocation/removal remains gated on external SDK packaging.
  - Dependency audit checks were degraded for npm/Python tooling availability.

## 6. Deployment Information
- Deployment approach: in-repo runtime/tooling foundation is merged into the kernel and installer paths; plugins are loaded from local boot configuration using `plugin.toml` manifests.
- Configuration requirements:
  - Boot config may declare `plugins` entries with manifest paths and config maps.
  - Skills should use `skills` entries with legacy `tools` fallback for one release cycle.
  - Plugin authors must provide valid `plugin.toml` fields and relative entry paths.
- Environment dependencies:
  - Go runtime/kernel build environment.
  - Python 3 for Python plugins and reference SDK.
  - Node/Bun strategy documented for future TS bundle consumers.
  - Local process-group semantics for plugin shutdown on POSIX; Windows codepaths are guarded where relevant.

## 7. Maintenance Guide
- Key operational procedures:
  - Inspect plugin status through `GET /internal/snapshot/plugins` only in trusted/local deployments.
  - Review plugin stderr/logging guidance before broad deployment to avoid secret leakage.
  - Use distro lock v2; v1 locks migrate on load.
  - Use `scripts/test-plugin-hello.sh` and Go kernel/plugin tests as smoke checks after runtime changes.
- Monitoring points:
  - Plugin status, PID, restart count, last error, registered tools, and subscriptions.
  - Supervisor restart/backoff events and terminal failed state.
  - Plugin tool deadlines/timeouts and malformed message thresholds.
  - Distro install/update lock output for mixed components.
- Common troubleshooting:
  - Register failure: check protocol version, plugin id mismatch, manifest validation, and entry path/runtime allowlist.
  - Missing tools: inspect `register`/`update_tools` output and `SnapshotPlugins()` registered tool catalog.
  - Hook behavior mismatch: verify plugin subscriptions and `HookSubscriber` registration.
  - Shutdown leaks: verify plugin child process group cleanup and SDK shutdown callbacks.

## 8. Reflection & Strategic Insights
- Link: `memory-bank/reflection/reflection-skill-plugin-architecture.md`
- Top strategic insights:
  1. A narrow lifecycle-neutral interface can integrate long-lived plugins without conflating them with user clients.
  2. Reference plugins are most valuable when they act as live protocol specs covering tool, hook, bus, diagnostics, and shutdown paths.
  3. Intentional dead-code migration bridges must have explicit TODOs, skipped tests, and owned follow-ups or they become permanent ambiguity.

## 9. Known Issues & Future Roadmap
- Deferred items:
  - Clean up stale legacy builtin metadata in `cmd/tabula/kernel.tools.json` and related tests, or quarantine it with clear non-runtime guardrails.
  - Authenticate or document strict locality for `GET /internal/snapshot/plugins`.
  - Add release CI dependency audits for Go, Python SDK artifacts, and JS/TS bundle lockfiles.
  - Define stderr/log redaction guidance and optional runtime redaction for plugin stderr and boot stderr.
- Future enhancements:
  - Complete external `tabula-bundles` migrations for hook plugins, MCP plugin, driver/subagent plugins, gateway plugins, and per-call skills with `tools[].exec`.
  - Implement subagent plugin-side spawn-token/MaxChildren/depth coverage, then remove D1.11(b) dead code and skipped kernel spawn tests.
  - Finish SDK/lib relocation: remove in-repo `skills/_pylib` and `skills/_tslib`, remove distro preserve behavior for those support dirs, and replace inline `_*` compatibility once bundled wheel/tarball packages exist.
  - Add provenance metadata convention for plugin-emitted `send` bus messages if downstream consumers require stable plugin identity.
- Technical debt:
  - Deprecated legacy builtin constants and metadata remain for compatibility/test references.
  - Some spawn-related tests remain skipped until external subagent plugin ownership lands.
  - Archive evidence reconstruction relied on long logs; future L4 tasks should maintain a requirement-to-test evidence matrix before SECURITY handoff.
