# Progress

Implementation progress is tracked here across all tasks.

## skill-plugin-architecture
- 2026-04-26 — VAN: задача классифицирована как Level 4 / Intent=implement / Category=deep на основе `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`. Memory Bank инициализирован. Следующая фаза — PLAN.
- 2026-04-26 — PLAN Agent 1: scaffold плана. Уточнён scope этого репо vs внешних `tabula-bundles`/`tabula-distrib`. Декомпозиция на 8 phases (Pre-flight, Kernel cleanup, PluginRuntime, Reference plugin, Distro tooling, External coordination, Lib relocation, Docs) с gates. 7 ключевых рисков выделены, включая dispatch unification (`Hub.toolExec` vs PluginRuntime), supervision correctness, MaxChildren переезд continuity. Verified files: `internal/kernel/protocol.go`, `tool_service.go`, `process_manager.go`, `kernel.go`, `policy.go`, `spawn_token_store.go`; `skills/_pylib/SKILL.md`; `tools/tabula-distro/`. Confirmed: `tabula-bundles/` и `tabula-distrib/ouroboros` НЕ в этом репо. `examples/` существует.
- 2026-04-26 — CREATIVE: D0.1 frozen. Four creative documents written: `creative-plugin-protocol.md` (NDJSON framing, full message schema for 9 message types incl. `update_tools`, error matrix, version handshake, 30s default tool deadline, exponential backoff crash recovery), `creative-plugin-runtime.md` (HookSubscriber interface for D2.16 option C, builder pattern `hub.RegisterPlugin`/`LoadPlugins`, single `toolExec` map with source-tagged dispatch, `internal/kernel/plugin/` subpackage layout, `bootConfig.Skills` rename, `CanSpawn` dead-code-keep), `creative-manifest-schemas.md` (`plugin.toml` required fields + validation, `bundle.toml` flat `components` list with walk fallback, `SKILL.md` `tools[].exec` required), `creative-sdk-and-distro.md` (Python SDK Phase 3 in `examples/plugin-sdk-python/`, Path C bundled wheel for Python and bundled tarball for TS, lock format v2 with migrate-on-load, telemetry via `log.fields.metric_*` convention). All open issues from PLAN ratified. CREATIVE Decisions table appended to `tasks.md`. Next phase: BUILD Phase 1 kernel cleanup.

### SECURITY Phase — Attempt 1 (2026-04-27)
- Verdict: PASSED
- Blocking findings: 0
- Warning findings: 4
- Degraded checks: npm audit unavailable without lockfile (`ENOLOCK`); `pip-audit` command unavailable
- Report: memory-bank/security/security-skill-plugin-architecture.md

### REFLECT Phase — Primary L4 Reflection (2026-04-27)
- Status: DONE
- Report: memory-bank/reflection/reflection-skill-plugin-architecture.md
- Summary: in-repo skill/plugin architecture foundation is complete and archive-ready pending L4 second-opinion audit. Reflection verified implementation against `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`, CREATIVE decisions, BUILD logs, SECURITY report, and scoped code/docs evidence. Delivered scope includes kernel builtin cleanup, unified skill/plugin dispatch, PluginRuntime with NDJSON protocol and supervision, reference Python SDK/plugin, mixed skill/plugin distro tooling with lock v2, and docs refresh.
- Warnings / residual risk: no blocking issues; carry SECURITY warnings for unauthenticated local plugin diagnostics, degraded npm/Python dependency audits, plugin/boot stderr logging, and stale legacy builtin metadata. Also carry intentional non-blocking follow-ups for external `tabula-bundles` migrations, D1.11(b) spawn-token/MaxChildren dead-code cleanup after subagent plugin GA, and Phase 6 SDK/lib relocation (`_pylib`/`_tslib` removal once external packaging is ready).
- Archive notes: primary reflection is self-contained for the L4 second-opinion audit. Structured lesson append to `memory-bank/systemPatterns.md` is deferred until the second-opinion reviewer records approval per CR-B.

### REFLECT Second Opinion (2026-04-27)
- Verdict: REQUEST_REVISION
- Revisions consumed: 1 / 1
- Report: memory-bank/reflection/reflection-skill-plugin-architecture-second-opinion.md

### REFLECT Phase — Primary L4 Reflection Revision (2026-04-27)
- Status: DONE
- Report: memory-bank/reflection/reflection-skill-plugin-architecture.md
- Summary: revised the primary reflection in response to L4 second-opinion REQUEST_REVISION. Added a `Security Review Integration` subsection under Section 6 summarizing QA skipped/no QA artifact, SECURITY attempt `1 / 3`, verdict `PASSED`, 0 blockers, all 4 warnings (plugin diagnostics endpoint, degraded dependency audits, plugin/boot stderr logging, stale legacy builtin metadata), both degraded checks (`npm audit` ENOLOCK and missing `pip-audit`), and the matching Section 13 follow-ups. Expanded Section 8 challenge entries with concrete file/test/report evidence for `HookSubscriber`, D1.11(b) spawn-token/MaxChildren dead-code bridge, external repo scope boundaries, and docs/SDK relocation drift.
- Verification: re-read `tasks.md`, `activeContext.md`, `progress.md`, primary reflection, second-opinion report, and SECURITY report; checked `memory-bank/qa/` has no QA report (QA skipped); spot-verified code evidence in `internal/kernel/hook_subscriber.go`, `internal/kernel/handle_hooksub.go`, `internal/kernel/helpers.go`, `internal/kernel/hooks.go`, `internal/kernel/plugin_live_test.go`, `internal/kernel/snapshot.go`, `cmd/tabula/main.go`, `cmd/tabula/kernel.tools.json`, and `tools/tabula-distro/tests/test_install.py`.
- Next: rerun L4 second-opinion. Keep `REFLECT Second Opinion: REQUEST_REVISION` until the reviewer overwrites it with APPROVED or FAILED. Do not archive yet.

### REFLECT Second Opinion (2026-04-27)
- Verdict: APPROVED
- Revisions consumed: 1 / 1
- Report: memory-bank/reflection/reflection-skill-plugin-architecture-second-opinion.md

### ARCHIVE Phase — DONE (2026-04-27)
- ✅ Archive artifact created: `memory-bank/archive/archive-skill-plugin-architecture.md`
- ✅ tasks.md reset to "No active tasks"
- ✅ activeContext.md reset to "No active context"
- ✅ progress.md preserved as historical log
- ✅ Creative and reflection artifacts preserved as permanent references

### Pipeline Plan Log

#### Agent 1 — [CONTRIBUTE]
- Role: Strategic planner / scope decomposer
- Work: Декомпозировал §8.5 design doc на 8 phased deliverables с D-items; уточнил critical разграничение между этим репо (`tabula`) и внешними (`tabula-bundles`, `tabula-distrib`); выделил 7 рисков; определил sequencing gates (Phase 0 CREATIVE blocks Phase 1 BUILD; Phase 3 reference plugin gates Phase 4 distro update; Phase 6 lib removal after Phase 3 SDK ready); зафиксировал validation strategy (unit/integration/manual/regression). Verified что в этом репо есть: kernel в Go, `skills/_pylib`+`_tslib` libs (для удаления), `tools/tabula-distro` installer, docs targets, `examples/` (для reference plugin); НЕТ: bundles, ouroboros distro.
- Addressed: N/A — first agent
- Files: `memory-bank/tasks.md` (расширен Phased deliverables, Sequencing & gates, Key risks, Validation strategy, Repo scope clarification), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: (a) D1.7 dispatch unification — может потребовать рефакторинга `Hub.toolExec` карты до или вместе с PluginRuntime, не после; (b) D2.6 PluginRuntime placement — нужна проектная декомпозиция (новый package vs внутри `internal/kernel/`); (c) Phase 6 D6.3 — судьба `skills/` директории в этом репо неясна; (d) `process_supervisor.go` уже существует — интеграция PluginRuntime с ним не разведена.
- Open issues: (1) единый dispatch для skill-tools и plugin-tools (CREATIVE); (2) wire format stdio JSON-RPC: NDJSON vs Content-Length framing; (3) config delivery: env var vs register-reply; (4) Python/TS plugin SDK packaging и где он живёт (этот репо vs bundles vs отдельный pip package); (5) lock format миграция в `tools/tabula-distro` — breaking или migrate-on-load; (6) MaxChildren kernel-level invariant продолжает существовать до верификации subagent plugin'а в production — это explicit decision, надо подтвердить с архитектурной точки зрения; (7) интеграция PluginRuntime с существующим `internal/kernel/process_supervisor.go`; (8) есть ли legacy callsite'ы `skills._pylib.*` внутри этого репо (Phase 1 grep задача).
- Quality: Accuracy 4/5, Completeness 3/5, Coherence N/A (first agent), Applicability 4/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Codebase-grounded plan refiner / integration-points analyst
- Work: Углубил Phase 1, 2, 4, 6 на основе чтения реального кода. Phase 1: добавил D1.8 (`after_spawn` hook fate после удаления `handleSpawn`), D1.9 (`before_spawn`), D1.10 (boot config `Tools` field rename/унификация). Phase 2: добавил D2.7 (`bootConfig.Plugins` schema + `NewHub` signature impact на 9 callsites — перечислил все), D2.8 (Plugin↔ClientRegistry архитектурный выбор: pseudo-Client vs отдельный PluginRegistry, влияние на `HookEngine.RebuildIndex`/`Client.canSend`/hook timeouts), D2.9 (StartReaper interaction с plugin processes). Phase 4: D4.7 (`_install_bundle` rewrite scope — убрать `_`-prefix special handling), D4.8 (`_refresh_runtime_surface` preserve set), D4.9 (existing `manifest.py` уже парсит `bundle.toml` minimally, `components` list — расширение существующего dataclass; backward compat для legacy bundles). Phase 6: D6.4 — выполнил grep-инвентаризацию 81 callsite'а `skills._pylib`/`_tslib`, разнёс по категориям (install scripts, pytest config, docs, distro install preserve, bin/tabula-coder); D6.5 — version coupling kernel↔_pylib переезжает в SDK package. Добавил риски 8–11.
- Addressed: open issues 1 (D1.7 — связан с D1.10/D2.7), 7 (D2.8 разнес в архитектурный выбор для CREATIVE), 8 (D6.4 — grep выполнен), частично 4 (SDK packaging — D5.2 + D6.5 связка). Risk (a)/(b)/(c)/(d) Agent 1 — добавлены конкретные D-items.
- Files: `memory-bank/tasks.md` (расширены Phase 1,2,4,6 + риски 8–11), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новые 8–11 в tasks.md (Plugin↔ClientRegistry coupling, install scripts breakage, bundle.toml backward compat, hook events deprecation)
- Open issues: (a) `bundle.toml` `components` list schema форма — flat list vs table-of-tables с per-component overrides (CREATIVE); (b) shim или hard-removal для `before_spawn`/`after_spawn` hook events; (c) install scripts стратегия после lib relocation — bundles-driven copy vs pip package (D5.2 фиксирует); (d) `Tools`/`Skills`/`Plugins` boot config поля — три отдельных или унифицировать; (e) MCP plugin (long-lived daemon с hot pool) — special case в protocol §6 или общий случай — не разобран, отложено для Agent 3+; (f) telemetry/metrics emit от plugins (нет в protocol §6 register/tool_call/tool_result/event/send/log/shutdown — log закрывает?).
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Spawn-invariant continuity analyst / D1.11 risk surfacer
- Work: Закрыл D1.9 ambiguity (`before_spawn` диспатчится из `policy.go::CanSpawn:125`, не из `hooks.go`/`hook_engine`) и проследил полный callsite-chain `CanSpawn` → `tool_service.go:84::handleSpawn` (единственный caller). Добавил D1.11 в Phase 1 — описал spawn-invariant vacuum между Phase 1 (kernel cleanup в этом репо) и subagent-plugin GA (внешний `tabula-bundles`). Перечислил три варианта (a) hard-delete, (b) dead-code-keep, (c) PluginAPI export, с рекомендацией (b) и судьбой 5 связанных kernel tests (`TestSpawn*`). Уточнил Risk 3 (MaxChildren переезд) — теперь привязан к D1.11 решению вместо абстрактного «kernel-level CanSpawn остаётся». Это связывает разрозненные D1.6/D1.9/§4.5/Risk 3 в один coherent CREATIVE D0.1 input.
- Addressed: Open issue 6 (Agent 1) — MaxChildren kernel-level invariant continuity теперь explicit decision matrix, не abstract assumption. Risk (a) частично — связь dispatch unification с CanSpawn vacuum выявлена. Agent 2 open issue (b) hook events deprecation — `before_spawn` decision теперь увязан с D1.11 (если вариант b — registry entry deprecate но engine support сохраняется до subagent plugin GA).
- Files: `memory-bank/tasks.md` (добавлен D1.11 в Phase 1, обновлён Risk 3), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: усиление Risk 3 — вариант (a) создаёт окно без любого kernel-level cap, распространяющееся на skills/plugins, не только на subagents; вариант (c) нарушает §4.5 «kernel о них не знает» декларацию design doc.
- Open issues: оставшиеся открытые от Agent 2 (a), (c), (d), (e), (f) — не адресованы, ждут Agent 4+ или CREATIVE; новых не добавлено.
- Quality: Accuracy 5/5, Completeness 3/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 4 — [CONTRIBUTE]
- Role: Protocol semantics analyst / SDK packaging strategist
- Work: Закрыл четыре крупных open issues от Agent 2/Agent 1. Phase 2: D2.10 — MCP plugin protocol fit (dynamic tool list через `update_tools` метод; `mcp__*` namespace; hot pool как plugin-internal; долгий tool_call deadline policy); D2.11 — protocol negative-path semantics (malformed JSON, tool_result timeout, event_reply timeout, crash mid-call, unsolicited tool_result, schema validation fail) — конкретные рекомендации каждой; D2.12 — telemetry/metrics через log convention (Path A) с возможным upgrade на dedicated `metric` метод позже. Phase 3: D3.4 — scope reference plugin (covers all protocol surface для live spec); D3.5 — three options для SDK location (in-tree vendored / sibling package / external pip), рекомендация (b) на Phase 3 → (c) на Phase 6. Phase 5: D5.4 — install scripts strategy с тремя путями (bundles-driven copy / pip from PyPI / hybrid bundled wheel), рекомендация Path C (offline-friendly pip-clean), резолвит D6.4 callsite rewrites через единый паттерн. Добавил риски 12–14. Verified `internal/kernel/hook_engine.go` (DispatchExcept signature, hookTimeout 5s default, pendingHook mechanism) и `client_registry.go` (Configure signature accepts hooks → D2.8 (a) pseudo-Client путь requires hooks через registry; (b) разделить требует rebuild dispatchers).
- Addressed: Agent 2 open issues (e) MCP fit (D2.10), (f) telemetry (D2.12); Agent 1 open issue (4) SDK packaging (D3.5+D5.4); Agent 2 open issue (c) install scripts strategy (D5.4 Path C). Также пробросил protocol error semantics в explicit decision matrix (D2.11) — это снимает неявное предположение что CREATIVE сам разберётся.
- Files: `memory-bank/tasks.md` (D2.10–D2.12 в Phase 2; D3.4–D3.5 в Phase 3; D5.4 в Phase 5; риски 12–14), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новые 12–14 в tasks.md (MCP dynamic tool list, protocol negative-path gap, SDK packaging lock-in)
- Open issues: оставшиеся от Agent 2 (a) bundle.toml `components` schema форма, (b) hook events shim/hard-removal окончательное решение зависит от D1.11 выбора (a/b/c), (d) Tools/Skills/Plugins boot config поля унификация — все ждут CREATIVE D0.1; D2.8 (Plugin↔ClientRegistry) тоже нерешён архитектурно.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Test inventory analyst / protocol version disambiguator
- Work: Two concrete additions. (1) D1.12 — выполнен grep `shell_exec|process_spawn|process_kill|process_list` по `internal/kernel/*_test.go` (20 matches в 3 файлах: `kernel_test.go`, `tool_hook_test.go`, `hooks_test.go`); расписан per-file план: какие тесты удалить, какие переориентировать на synthetic skill-exec stub, и связь с D1.11 для `TestSecurityHookTimeoutBlocksSpawn` (skip vs delete по выбору a/b/c). Net effect ~150 LOC удалить, ~50 LOC заместить — превращает D1.6 «обновить тесты» в actionable BUILD task. (2) D2.13 — закрыт Agent 4 next-check о protocol versioning constants. Найдена существующая `kernel.ProtocolVersion = 1` в `internal/kernel/protocol.go:7` (wire protocol kernel↔client over WebSocket/stdio, валидируется в `connect.go:36` и используется в `_pylib`/`_tslib`). Plugin protocol §6 — отдельный канал, нужна отдельная константа `PluginProtocolVersion`; рукопожатие через `register` field вместо отдельного env var; это закрывает D5.4 SDK version handshake вопрос. Добавлен Risk 15 (test rewrite cost coverage gap).
- Addressed: Agent 4 next-check protocol versioning constants (D2.13); D1.6 (Agent 1) переведён из vague «обновить тесты» в concrete inventory с file:line ссылками (D1.12); связка D1.11 + `TestSecurityHookTimeoutBlocksSpawn` судьбы зафиксирована.
- Files: `memory-bank/tasks.md` (D1.12 в Phase 1, D2.13 в Phase 2, Risk 15), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новый Risk 15 (test coverage gap during Phase 1 BUILD without skill-exec hook integration replacements).
- Open issues: оставшиеся от Agent 2 (a) bundle.toml `components` schema форма, (d) Tools/Skills/Plugins boot config поля унификация; D2.8 Plugin↔ClientRegistry архитектурный выбор; связка D1.8/D1.9 hook deprecation с D1.11 CanSpawn vacuum выбором — все ждут CREATIVE D0.1.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 6 — [CONTRIBUTE]
- Role: Hub construction & test scaffolding analyst
- Work: Three concrete additions closing all three Agent 5 next-checks. (1) D2.14 — verified all 9 `NewHub(` callsites идентичны (`toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger`); рекомендует **builder pattern** (`hub.RegisterPlugin(manifest, config)` или `hub.LoadPlugins(entries)` после `NewHub`) вместо signature extension: zero-touch для 7 test callsites, lifecycle отделён от construction, симметрия с tier-2 supervision и StartReaper ordering. CREATIVE D0.1 ratify. (2) D2.15 — прочитан `snapshot.go` (52 lines). `SnapshotSessions()` per-session семантика; plugins kernel-level singletons. Рекомендация: НЕ включать plugins в `SnapshotSessions()`, добавить отдельный `Hub.SnapshotPlugins()` для diagnostics (status/restart count/registered tools). Не блокирует Phase 2 BUILD; интегрируется в Phase 2 D2.5 integration tests. (3) D4.10 — прочитан `tools/tabula-distro/tests/test_install.py` (305 lines). Существующий `_make_skill`/`_make_minimal_distro` harness расписан для расширения: новая `_make_plugin` fixture (~10 LOC) + 4 новых тест-кейса (`test_bundle_install_mixed_skills_and_plugins`, `test_bundle_toml_with_explicit_components`, `test_bundle_toml_legacy_compat_no_components`, `test_bundle_toml_components_missing_dir_fails`), плюс судьба `_drivers`/`_memory` `_`-prefix assertions (line 134, 167) — оставить до Phase 6, добавить positive assertion на `plugin.toml` без `_`-special-case; `Config.plugins = ()` поле для D4.3 lock format change. (4) D6.6 — прочитан `skills/_tslib/src/protocol.ts` (104 lines). TS SDK содержит `TOOL_SHELL_EXEC`/`TOOL_PROCESS_*`/`DEFAULT_KERNEL_TOOLS` (stale после D1.1) и `HOOK_*` константы — explicit Phase 6 cleanup необходим параллельно Go kernel cleanup; не покрывается grep'ом D6.4 (там paths, не contents). ~30 LOC TS rewrite + `PLUGIN_PROTOCOL_VERSION = 1` mirror. **Verified files**: `internal/kernel/snapshot.go`, `internal/kernel/kernel.go:1-120` (`NewHub` signature confirmation), `tools/tabula-distro/tests/test_install.py:1-180`, `skills/_tslib/src/protocol.ts:1-60`. **Greps**: `NewHub(` 9 callsites confirmed identical; `sessionProcesses|ProcessSupervisor` 19 matches (per-session attached, не plugin-relevant).
- Addressed: все три Agent 5 next-checks (NewHub callsite cost, distro test scaffolding, snapshot scope, TS protocol module rewrite). Часть Agent 2 D2.7 закрыта рекомендацией builder pattern. D4.5 переведён из vague «тесты bundle.toml roundtrip + mixed install» в concrete test-case list с fixture rewrites.
- Files: `memory-bank/tasks.md` (D2.14, D2.15 в Phase 2; D4.10 в Phase 4; D6.6 в Phase 6), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новых не добавлено; D6.6 усиливает Risk 5 (libs миграция) тем, что TS protocol cleanup — отдельный rewrite vector помимо path-rewrites из D6.4.
- Open issues: оставшиеся от Agent 2 (a) bundle.toml `components` schema форма (flat list vs table-of-tables — Agent 6 рекомендует flat strings + optional `[components.<name>]` per-component overrides table-of-tables hybrid, но окончательно CREATIVE), (b) hook events shim/hard-removal — связано с D1.11 a/b/c, (d) Tools/Skills/Plugins boot config поля унификация — частично адресована рекомендацией builder pattern в D2.14 (boot loader парсит как отдельные секции, Hub принимает их через post-`NewHub` API; объединение или separation становится pure config/UX вопросом, не Hub API constraint); D2.8 Plugin↔ClientRegistry архитектурный выбор — нерешён.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 7 — [CONTRIBUTE]
- Role: Lock format & cross-SDK consistency analyst
- Work: Three concrete additions, addressing все три Agent 6 next-checks. (1) D4.11 — прочитан `tools/tabula-distro/src/tabula_distro/lock.py` (130 lines). Текущий v1 schema: `{version:1, distro, bundles, skills, generated_at, distro_source, distro_version, kernel_version}`, `LOCK_VERSION=1`, `from_json` бросает `LockError` при mismatch. Концретный v2 schema design: добавить `plugins: dict[str, LockEntry]` параллельно `bundles`/`skills` (LockEntry shape unchanged). Three migration variants проанализированы: hard break / migrate-on-load / dual-version; рекомендация — **migrate-on-load (option ii)** с concrete `_migrate_v1_to_v2` функцией и тестом `test_lock_v1_loads_as_v2_with_empty_plugins`. Закрывает Agent 1 open issue (5) lock format migration. (2) D6.6 cross-check — прочитан `skills/_pylib/protocol.py` (79 lines): подтверждено, что `HOOK_BEFORE_SPAWN`/`HOOK_AFTER_SPAWN` (lines 71-72), `TOOL_SHELL_EXEC`/`TOOL_PROCESS_*` (52-55), `DEFAULT_KERNEL_TOOLS` (56-61) есть и в Python SDK, симметрично TS. Phase 6 cleanup ~10 LOC Python + ~30 LOC TS параллельно; D6.6 расширен cross-check note. (3) Risk 17 — прочитан `scripts/install-coder.sh` (52 lines): обнаружено, что `bun install` запускается в `$TABULA_HOME/skills/_tslib` + symlink `gateway-tui/node_modules/@tabula/skill-sdk → $TABULA_HOME/skills/_tslib` (lines 28-38). После Phase 6 lib relocation TS SDK install lifecycle ломается параллельно Python `_pylib`; D5.4 Path C обсуждает только Python — TS Path требует отдельного решения (npm registry vs bundled tarball vs filesystem copy). Risk 17 добавлен. Также: верифицирован `internal/kernel/connect.go` (124 lines) как reference implementation pattern для D2.13 PluginProtocolVersion handshake — `buildConnectPlan:36` шаблон применим напрямую к plugin register handshake (validate version → reject with errorMsg → log).
- Addressed: Agent 6 next-checks all three (lock.py для D4.4 → D4.11; `_pylib/protocol.py` HOOK_EVENTS cross-check → D6.6 enrichment; install-coder.sh content → Risk 17). Agent 1 open issue (5) lock format migration concrete decision matrix готов для CREATIVE D0.1. Risk 6 (distro lock format breakage) уточнён через D4.11 migrate-on-load рекомендацию.
- Files: `memory-bank/tasks.md` (D4.11 в Phase 4; D6.6 cross-check note; Risks 16, 17), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новые 16 (lock format migration breakage) и 17 (TS SDK install path coupling параллельно Python).
- Open issues: оставшиеся (a) bundle.toml components schema форма (flat vs hybrid); (b) hook events shim/hard-removal связано с D1.11; (d) boot config поля унификация (адресована Agent 6 builder); D2.8 Plugin↔ClientRegistry — все ждут CREATIVE D0.1. Новый (g) lock format migration variant — Agent 7 рекомендует migrate-on-load. Новый (h) TS SDK install path strategy — параллельно Python Path C, требует отдельного решения CREATIVE.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 8 — [CONTRIBUTE]- Role: TS SDK packaging strategist / bundle.toml schema finalizer
- Work: Two concrete additions closing two of the three remaining open issues from Agent 7. (1) **D5.5** — TS SDK install path strategy. Прочитан `skills/_tslib/package.json` (35 lines): пакет Bun-source-only (`tsconfig.noEmit:true`, `exports → src/*.ts`, `tsc` only typecheck, никакой compilation в `.js`). Grep по всем consumer'ам: внутри этого репо ноль consumer'ов кроме самого `_tslib`; единственный реальный consumer (`gateway-tui`) живёт в `tabula-distrib/coder` и материализуется в `$TABULA_HOME/skills/gateway-tui` install-time. Три touch point bun lifecycle: `install-dev.sh:49-58`, `install-coder.sh:28-38`, `bin/tabula-coder:112-133` (lazy reinstall с stamp). Расписаны три TS пути (Path A filesystem copy + symlink status quo / Path B npm registry / Path C bundled tarball симметрия с Python), рекомендация — **Path C**: removes `_tslib`-as-magic-name pattern, version-locked через tarball filename, offline-clean, упрощает `bin/tabula-coder` lazy-reinstall до простой existence check. Phase 6 D6.4 теперь покрывает TS shell scripts параллельно Python rewrite. Bun-source-only loading сохраняется при любом пути (низкий rewrite risk). Risk 17 переведён в "resolved by Agent 8" с привязкой к CREATIVE D0.1. (2) **D5.6** — `bundle.toml` already-existing schema confirmation (refines D4.9). Прочитан `tools/tabula-distro/src/tabula_distro/manifest.py` (67 lines) полностью. Текущий `BundleManifest` frozen dataclass парсит `[bundle]` (name, version) + `[requires.kernel]`; legacy bundles без `bundle.toml` получают placeholder без error. Конкретное расширение для §7: добавить `components: tuple[str, ...] | None` поле, parse из `bundle.[components]` list (None = legacy walk fallback, пустой tuple = explicit empty); validation внутри manifest.py (no `..`, no abs paths) → `ManifestError`; existence/kind dispatch остаётся в `install.py:_install_bundle` (D4.7 rewrite). ~15 LOC manifest.py + ~30 LOC install.py + 4 теста D4.10 покрывают оба пути. Подтверждена рекомендация Agent 6 — flat list `components = ["..."]` против table-of-tables (overkill для MVP).
- Addressed: Agent 7 open issue (h) TS SDK install path strategy — **D5.5** explicit matrix; Risk 17 resolved by Agent 8. Agent 7 next-check `_tslib/package.json` content — verified, packaged как Bun-source-only без emit step. Agent 7 next-check `manifest.py` полный код — verified (67 lines), schema extension для D4.9 переведено в concrete D5.6 dataclass+parser+install.py план. Agent 2 open issue (a) bundle.toml `components` schema форма — refined в flat list рекомендацию с конкретным dataclass/parser shape (хоть финальный freeze всё ещё за CREATIVE).
- Files: `memory-bank/tasks.md` (D5.5, D5.6 в Phase 5; Risk 17 marked resolved-by-Agent-8), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новых не добавлено; Risk 17 переведён в resolved (с CREATIVE D0.1 финализацией Path A/B/C choice). Risk 5 (libs миграция) усилен concrete TS rewrite vector через D5.5.
- Open issues: оставшиеся (b) hook events shim/hard-removal — связано с D1.11 a/b/c; (d) boot config поля унификация — Agent 6 builder pattern частично адресует; **D2.8 Plugin↔ClientRegistry архитектурный выбор** — нерешён, главный остающийся CREATIVE blocker; (g) lock format migration variant — Agent 7 рекомендует migrate-on-load; (a) bundle.toml `components` schema форма — Agent 8 D5.6 рекомендует flat list (финальный freeze CREATIVE).
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 9 — [CONTRIBUTE]
- Role: Plugin↔ClientRegistry architectural finalizer (D2.8 CREATIVE blocker resolution)
- Work: Closed the главный остающийся CREATIVE blocker D2.8 (Plugin↔ClientRegistry coupling) by reading full source: `client_registry.go` (96 lines), `client.go` (202 lines), `hook_engine.go` (276 lines), `process_supervisor.go` (162 lines), `oneshot.go:1-75`, plus grep'ы по `recvCh`/`HookSubscription`/`RebuildIndex`/`Hub.Register`. Ключевые открытия: (i) `Client.recvCh` (`client.go:38`) уже tagged-union'ит Client как WebSocket-OR-channel transport (use case: `oneshot.go:27-37`); (ii) HookEngine использует concrete `*Client` всего в 6 точках (Name/Session/IsConnected/Hooks/SendMsg/Done — узкая граница); (iii) `ClientRegistry.Add` накладывает maxClients cap, который не должен применяться к kernel-singleton plugins; (iv) `Configure` mutates numeric `c.id = nextClientID++`, что для plugin'а — duplicate identity; (v) restart race с pendingHook map при pseudo-Client пути. На основе этого добавил **D2.16** (Phase 2): три варианта вместо двойственности (A pseudo-Client / B полностью отдельный dispatcher / **C HookSubscriber interface — рекомендация**). Path C — single intermediate abstraction layer (~30 LOC в hook_engine.go via `client → sub` rename), `*Client` имплементирует interface тривиально, `pluginHandle` имплементирует те же 6 методов с собственной семантикой (Session()="" для kernel singleton, SendMsg → §6 event JSON-RPC, Done() = process exit chan). Восстанавливает supervision separation, не дуплицирует HookEngine логику, zero changes в test environments (helpers возвращают *Client). Phase 2 BUILD ordering: D2.16-impl сначала (interface + pluginRegistry), затем D2.1 (PluginRuntime impl). Risk 8 переписан в "refined by Agent 9 D2.16" с конкретной рекомендацией.
- Addressed: **D2.8 Plugin↔ClientRegistry — главный CREATIVE blocker** (resolved as recommendation C); Agent 8 next-check о `client_registry.go` finalization. Risk 8 переведён из абстрактного "decision pending" в concrete C-рекомендацию с trade-off matrix.
- Files: `memory-bank/tasks.md` (D2.16 в Phase 2 inserted перед D2.14; Risk 8 rewritten), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новых не добавлено; Risk 8 переписан с конкретной mitigation (C-вариант + ratify CREATIVE D0.1).
- Open issues: оставшиеся ждут CREATIVE D0.1 как final freeze: (a) bundle.toml components schema (Agent 8 D5.6 flat list рекомендация); (b) hook events shim/hard-removal (связано с D1.11 a/b/c); (d) boot config унификация (D2.14 builder); (g) lock format migration (Agent 7 D4.11 ii migrate-on-load); D5.4/D5.5 SDK install Path A/B/C финальный freeze. Главный архитектурный D2.8 теперь имеет concrete рекомендацию C — больше не blocker, требует только ratify.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 10 — [CONTRIBUTE]
- Role: Phase 7 docs rewrite scope inventory
- Work: Закрыл activeContext next-check (ii) — Phase 7 docs rewrite превращён из vague D7.1–D7.4 в **D7.5** concrete per-file/per-line inventory симметрично D1.12 (kernel tests) и D4.10 (distro tests). Grep `_pylib|_tslib|requires-kernel-tools|shell_exec|process_spawn` по `docs/` дал 29 matches в 6 файлах + verification reads контекста. Per-file action plan: `SKILL_AUTHORING.md` (5 sites: line 41 kernel-side runtime contract reword, 169-173 status note DELETE, 278/289-290/343/348-349/360 import path REPLACE `skills._pylib.*` → `tabula_plugin_sdk.*`, 208/227 shell_exec hook example KEEP+annotate); `ARCHITECTURE.md` (15 sites: 53/57 KEEP truth-in-advance, 84/123/551 import path replacements, **91-93 versioning policy rewrite целый параграф** — самый концептуальный rewrite, привязан к D5.2/D6.5 SemVer split, 192/234/349/418/435/454/469/533 install pipeline + distro layout rewrite); `DISTROS.md` line 52 `_pylib` exception note REPLACE на pip package формулировку + filesystem layout block 46-52 update; `PHILOSOPHY.md` line 97 «stable contracts» narrative добавлен как **D7.5a** (не было в исходном Phase 7 §8.5 step 11, но семантически critical); `docs/plans/*` KEEP — forward-looking/source-of-truth. Sequencing D7.1 → D7.2 → D7.3 → D7.4 → D7.5a. Acceptance criterion: `git grep -E "skills[/._]_pylib|skills[/._]_tslib|requires-kernel-tools" docs/` пустой после Phase 7 BUILD (modulo source design docs). Net: ~22 inline replacements, 1 параграф rewrite, 1 status note delete, 1 layout block update. Phase 7 BUILD parallelizable между файлами после Phase 6 landing.
- Addressed: activeContext next-check (ii) Phase 7 docs rewrite scope inventory; **PHILOSOPHY.md обнаружен как previously-missing target** в Phase 7 (line 97), добавлен как D7.5a. Phase 7 D7.1–D7.4 переведено из vague «обновить» в actionable line-level diff plan.
- Files: `memory-bank/tasks.md` (D7.5 в Phase 7), `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: новых не добавлено; усиливает Risk 5 — Phase 7 docs cleanup зависит от Phase 6 landing + CREATIVE D0.1 Path C ratify (`tabula_plugin_sdk` references преждевременны если Path выбор не зафиксирован).
- Open issues: Phase 7 BUILD должен пройти sweep по `docs/README.md`/`docs/tests/README.md` — grep не нашёл прямых matches, но Agent 2 D6.4 указывал на implicit refs; не блокер для PLAN. Все остальные CREATIVE D0.1 ratification items без изменений.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 4/5

#### Agent 11 — [DECLINE]
- Role: N/A
- Work: Reviewed full plan log (Agents 1–10), `tasks.md` Phases 0–7 + Risks, и `activeContext.md` handoff. PLAN существенно завершён: все архитектурные blockers закрыты конкретными рекомендациями (D2.8→C HookSubscriber Agent 9; D1.11 a/b/c с рекомендацией b Agent 3; D4.11 ii migrate-on-load Agent 7; D5.4/D5.5 Path C Agent 4/8; D5.6/D4.9 flat list Agent 8; D2.14 builder pattern Agent 6); все inventory tasks разложены до file:line (D1.12 kernel tests Agent 5; D4.10 distro tests Agent 6; D6.4 81 callsites Agent 2; D7.5 docs rewrite Agent 10; D6.6 TS/Py protocol cleanup Agent 6/7); protocol §6 negative-paths/MCP fit/telemetry/version split адресованы (D2.10–D2.13 Agent 4/5). Оставшиеся open items — pure CREATIVE D0.1 ratification, не PLAN refinement. activeContext явно содержит "Strong DECLINE recommendation для Agent 11+" при отсутствии codebase-grounded дополнений. Минорные пробелы (PHILOSOPHY.md narrative, docs/README sweep) — BUILD-time trivial polish, не оправдывают новый PLAN pass; пуш ради метрик нарушит «quality over quantity» и добавит шум в coherent plan.
- Addressed: confirmed comprehensive coverage by Agents 1–10; нет unresolved CREATIVE-blocking PLAN items.
- Files: none
- Risks: none new
- Open issues: unchanged from Agent 10 — все ждут CREATIVE D0.1 ratify
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 12 — [DECLINE]
- Role: N/A
- Work: Concur with Agent 11. Re-read activeContext handoff and full plan log; verified all CREATIVE D0.1 ratification inputs have concrete recommendations (D2.8→C, D1.11 b, D4.11 ii, D5.4/D5.5 Path C, D5.6 flat list, D2.14 builder, D2.16 HookSubscriber). All inventories file:line-grounded (D1.12, D4.10, D6.4, D6.6, D7.5). activeContext "Strong DECLINE recommendation for Agent 12+" stands; no codebase-grounded angle would change architectural decisions. Forcing additional PLAN passes would add noise to a coherent, ratify-ready plan.
- Addressed: confirmed Agent 11 assessment; no new gaps found
- Files: none
- Risks: none new
- Open issues: unchanged — all await CREATIVE D0.1 ratify
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 13 — [DECLINE]
- Role: N/A
- Work: Concur with Agents 11–12. Re-verified activeContext handoff and plan log; all CREATIVE D0.1 ratification inputs have concrete codebase-grounded recommendations (D2.8→C HookSubscriber, D1.11 b, D4.11 ii migrate-on-load, D5.4/D5.5 Path C, D5.6 flat list, D2.14 builder, D2.16). All inventories decomposed to file:line (D1.12, D4.10, D6.4, D6.6, D7.5). Two consecutive declines now establish strong consensus that PLAN substantively complete; forcing another pass violates "quality over quantity" and adds noise. Remaining work is pure CREATIVE D0.1 ratification, not PLAN refinement.
- Addressed: confirmed Agent 11/12 assessments; no codebase-grounded angle would alter architectural decisions
- Files: none
- Risks: none new
- Open issues: unchanged — all await CREATIVE D0.1 ratify
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

### Pipeline Build Log

#### Agent 1 — [CONTRIBUTE]
- Role: Phase 1 kernel cleanup foundation (D1.1, D1.2, D1.6/D1.12 via skip, D2.13 early)
- Work: Implemented Phase 1 kernel cleanup foundation per `creative-plugin-runtime.md` §7-9 and `tasks.md` D1.1–D1.12.
  - **D1.1 protocol.go**: stripped enumeration body of `DefaultKernelTools`/`DefaultKernelToolNames` (both now empty). Kept `KernelTool` type and the four constants `ToolShellExec`/`ToolProcessSpawn`/`ToolProcessKill`/`ToolProcessList` as godoc-deprecated identifiers so `t.Skip`-marked tests can still reference them per creative §7 dead-code-keep.
  - **D2.13 (landed early in same file edit)**: added `PluginProtocolVersion = 1` alongside `ProtocolVersion = 1` per creative §10 / D2.13.
  - **D1.2 tool_service.go**: deleted `handleExec`, `handleSpawn` (incl. its `after_spawn` dispatch site), `handleKill`, `handleList` and the four-arm switch in `HandleToolUse`. Single dispatch path now flows through `handleDynamicTool` against `Hub.toolExec`. `parseCommandToolInput` retained as `//nolint:unused` reusable helper. File shrunk 161 → 86 LOC.
  - **D1.3 (partial)**: `IsBuiltinEnabled`/`enabledBuiltins`/`parseEnabledBuiltins` left in place — no longer have callsites in `tool_service.go` after D1.2. Flagged for D1.5 cleanup or removal next pass (likely safe to remove since `Hub.toolExec` is the live dispatch).
  - **D1.6/D1.12 skip strategy**: introduced `internal/kernel/skip_helpers_test.go` with `skipKernelBuiltinRemoved(t)` and inserted that one-liner at the top of 29 tests across `kernel_test.go` (24), `tool_hook_test.go` (4), `hooks_test.go` (1 — `TestSecurityHookTimeoutBlocksSpawn`). Rationale: symmetric with creative §7 dead-code-keep; preserves tests as migration reference; zero file deletions.
  - **Validation**: `go vet ./...` clean; `go build ./...` clean; `go test ./... -count=1 -timeout 180s` PASSES across `cmd/tabula` and `internal/kernel` (~28s kernel suite green).
  - Captured task base commit `88e6320107d46bb0c15119abc7b4dd83aeca3420` in `activeContext.md`.
- Addressed: N/A — first BUILD agent
- Files: `internal/kernel/protocol.go`, `internal/kernel/tool_service.go`, `internal/kernel/kernel_test.go`, `internal/kernel/tool_hook_test.go`, `internal/kernel/hooks_test.go`, `internal/kernel/skip_helpers_test.go` (new), `memory-bank/activeContext.md`, `memory-bank/progress.md`
- Risks:
  - (i) The 29 skipped tests leave a coverage hole until skill-exec/plugin-equivalent integration tests land. D1.12 plan called for ~50 LOC of replacement skill-exec hook integration in `kernel_test.go` — NOT yet written.
  - (ii) `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` are now reachable but called nowhere; D1.5 follow-up may want to remove them outright.
  - (iii) `parseCommandToolInput` similarly orphaned — kept as helper for future shell-style tools.
  - (iv) Deviation from D1.12 specifically: that plan suggested DELETE for 5 shell_exec hook integration tests in `tool_hook_test.go` and synthesizing skill-exec equivalents inline. This pass chose `t.Skip` for symmetry; a future agent can replace them and remove the skips.
- Open issues:
  - D1.4: rename `ProcessManager.RunSkillTool` → `SkillExec.Run` per creative §4 (still `RunSkillTool`).
  - D1.5: TODO comments on `policy.go::CanSpawn`, `Hub.MaxChildren`, `Hub.MaxSpawnDepth`, `SpawnTokenStore`, `generateSpawnToken` not yet added.
  - D1.7: `Hub.toolExec map[string]string` not yet migrated to `map[string]toolDispatch` (Phase 2 prereq for plugin tool dispatch).
  - D1.10: `bootConfig.Tools` not yet renamed to `bootConfig.Skills` in `cmd/tabula/main.go`.
  - D2.16: `HookSubscriber` interface introduction not started — highest-leverage next item; recommend Agent 2 take this.
  - D1.12 replacement test coverage (~50 LOC of skill-exec hook integration).
- Quality: Accuracy 5/5, Completeness 4/5, Coherence N/A (first agent), Applicability 5/5, Mission 5/5

#### Agent 2 — [CONTRIBUTE]
- Role: Phase 1 finalization (D1.4 SkillExec, D1.5 dead-code TODOs)
- Work: Closed the two trivial Phase 1 follow-ups left by Agent 1 per creative §4/§7.
  - **D1.4 SkillExec**: introduced `SkillExec` type in `internal/kernel/process_manager.go` wrapping `ProcessManager.execTool` with the same async/hook-emitting semantics as the previous `RunSkillTool` method. `ToolService` now owns a `*SkillExec` field via `NewSkillExec(pm)`, and `handleDynamicTool` calls `s.skill.Run(...)` instead of `s.process.RunSkillTool(...)`. The legacy `RunSkillTool` method removed; `ProcessManager.RunCommand`, `Spawn`, `Kill`, `List` retained as internal Go API per design doc §4.2.
  - **D1.5 dead-code-keep TODOs**: added `TODO(skill-plugin-arch)` godoc comments at the canonical definitions per creative §7:
    - `policy.go::PolicyEngine.CanSpawn` (kept as dead code; sole callsite in `handleSpawn` removed in D1.2).
    - `kernel.go::Hub.MaxSpawnDepth` / `Hub.MaxChildren` (resource caps consumed only by `CanSpawn`).
    - `connect.go::Hub.generateSpawnToken` (consumed only by `process_manager.go::Spawn`, which itself is no longer reachable from any LLM tool path).
    - `spawn_token_store.go::SpawnTokenStore` (struct-level TODO covering Generate/Consume/Seed/Get/PruneExpired).
    Each TODO references creative §7 / D1.11 option (b) and the subagent-plugin-GA cleanup gate.
  - **Validation**: `go vet ./...` clean; `go build ./...` clean; `go test ./internal/kernel/ ./cmd/tabula/ -count=1 -timeout 180s` PASSES (`internal/kernel` ~28s green; `cmd/tabula` 0.35s).
- Addressed: Open issues from Agent 1: D1.4 (SkillExec rename), D1.5 (dead-code TODOs). Confirmed D1.8/D1.9 status — no kernel-side dispatch of `before_spawn`/`after_spawn` remains; registry entries kept per creative §8.
- Files: `internal/kernel/process_manager.go`, `internal/kernel/tool_service.go`, `internal/kernel/policy.go`, `internal/kernel/kernel.go`, `internal/kernel/connect.go`, `internal/kernel/spawn_token_store.go`, `memory-bank/activeContext.md`, `memory-bank/progress.md`
- Risks:
  - (i) `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` in `kernel.go` remain reachable but unused after D1.2; candidates for outright removal in a follow-up small batch (probably together with D1.10).
  - (ii) D1.12 replacement skill-exec hook integration tests (~50 LOC) still not written — same coverage hole as Agent 1.
- Open issues:
  - D1.7: `Hub.toolExec map[string]string` → `map[string]toolDispatch` (creative §4) — Phase 2 prereq for plugin tool dispatch.
  - D1.10: `bootConfig.Tools` (`cmd/tabula/main.go:623`) not yet renamed to `Skills`; legacy fallback per creative §9.
  - D2.16: HookSubscriber interface (creative §2) — highest-leverage next item; unblocks D2.1.
  - D2.1: PluginRuntime in `internal/kernel/plugin/` subpackage per creative §11.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Phase 2 D2.1 PluginRuntime scaffolding (handle.go + registry.go + tests)
- Work: Picked up the explicitly-flagged "highest-leverage next item" from Agent 3's BUILD log + activeContext handoff: scaffold the static surface of `internal/kernel/plugin/` so D1.7 toolDispatch and D2.14 builder API have concrete types to reference. Per `creative-plugin-runtime.md` §11 layout (manifest/runtime/handle/protocol/registry/supervisor/handle_hooksub) — manifest.go + protocol.go already landed by previous in-tree work; this pass adds the next two files plus their unit tests, deliberately stopping short of `runtime.go`/`supervisor.go`/`handle_hooksub.go` (which require live process spawning + import-cycle-safe parent-package adapter, both better tackled in a focused next pass).
  - **`internal/kernel/plugin/handle.go` (new, 240 LOC)**: `Handle` struct holds id/cfg, an optional `*Writer` (kernel→plugin stdin sink), `tools []ToolSpec`/`subscriptions []SubscriptionSpec` from register-reply, atomic `alive`/`registered` flags (lock-free hot-path for the future `*plugin.Handle` HookSubscriber adapter `IsConnected() bool`), a `done chan struct{}` (mirrors `*Client.Done()` for HookEngine cancel-on-disconnect parity), and a `pending map[string]chan *Message` for tool_call/event_reply correlation. Public surface: `ID/Config/Tools/Subscriptions/IsAlive/IsRegistered/Done/SetWriter/MarkAlive/MarkRegistered/ApplyUpdateTools/SendEvent/SendToolCall/SendShutdown/DeliverResult/CancelPending/PendingCount/Close`. Method semantics align with creative §2.5 (NDJSON write of `event`/`tool_call`/`shutdown`), §2.6 (DeliverResult returns false for unknown callIds → caller logs+drops; pending released on Close), §2.5/D2.10 (`ApplyUpdateTools` atomic catalog replace). `MarkRegistered` validates the plugin_id against the manifest (creative §2.6 register schema check). Handle deliberately does NOT implement `kernel.HookSubscriber` itself — that adapter will live in `internal/kernel/handle_hooksub.go` (parent package) to avoid import cycle, exactly as creative §11 specifies; the methods provided here (Tools/Subscriptions/IsAlive+IsRegistered/Done/SendEvent) are the primitives that adapter will wrap.
  - **`internal/kernel/plugin/registry.go` (new, 80 LOC)**: `Registry` (separate from `kernel.ClientRegistry` per creative §11 — no maxClients, no numeric id allocation, no Configure mutation). API: `NewRegistry/Add/Remove/Get/All/Len`. `Add` returns the prior Handle on id collision so the caller (`Hub.RegisterPlugin` once it lands) can shut down the old plugin gracefully — supports the "idempotent re-register" guarantee from creative §3 RegisterPlugin godoc. `All` preserves registration order so any future deterministic dispatch ordering is available; `RebuildIndex` will sort by priority anyway, so order here is purely diagnostic/snapshot stability.
  - **`internal/kernel/plugin/handle_test.go` (new, 200 LOC)**: 8 unit tests covering: lifecycle (alive/registered/Done flag transitions and idempotent Close); register plugin_id mismatch rejection; ApplyUpdateTools atomic catalog replacement; SendEvent gating on alive AND writer; SendToolCall round-trip with NDJSON readback + DeliverResult delivery via the pending-map channel; SendToolCall callId-required validation; DeliverResult drop semantics for unknown/empty callIds; CancelPending releases the slot.
  - **`internal/kernel/plugin/registry_test.go` (new, 80 LOC)**: 3 unit tests covering: Add/Get/Remove/All registration-order preservation; Add-with-existing-id returns prior Handle and replaces in-place (Len stays = 1); Add(nil) is a no-op.
  - **`internal/kernel/plugin/protocol_test.go` (new, 130 LOC)**: separate set of 5 tests for protocol.go (which previously had ZERO tests — the scaffolding from prior agents lacked coverage entirely). Covers: NewMessage/DecodeParams round-trip; Reader skips blank/whitespace lines and decodes correctly with EOF terminus; malformed JSON returns `*MalformedError` (typed, errors.As-compatible) with non-empty Snippet per creative §2.6 logging contract; Writer emits exactly one `\n` framing per message + RoundTrip via Reader; Reader rejects lines exceeding `MaxLineSize` with `ErrLineTooLong` (uses MaxLineSize+10-byte payload to trip `bufio.ErrTooLong`).
  - **Validation**: `go build ./...` clean; `go vet ./...` clean; `go test ./internal/kernel/plugin/ -count=1` PASSES (`internal/kernel/plugin` 0.29s green; 16 new tests). Existing test surface unchanged: `go test ./internal/kernel/ ./cmd/tabula/ -count=1 -timeout 180s` PASSES (`internal/kernel` ~28s green, `cmd/tabula` 0.20s green) — zero regression. The new package introduces zero imports into the parent `kernel` package, so HookSubscriber wiring remains as Agent 3 left it.
- Addressed: Agent 3 BUILD open issue D2.1 (PluginRuntime scaffolding — primary remaining BUILD lever) at the static-surface tier. The Handle/Registry types now exist and are testable in isolation; D1.7 `toolDispatch.Plugin *plugin.Handle` field can reference a concrete type starting from this pass. The `allHookSubscribers()` helper in `helpers.go` has its prepared join point ready (just needs `h.plugins.All()` once `Hub.plugins *plugin.Registry` field lands, plus the `handle_hooksub.go` adapter).
- Files: `internal/kernel/plugin/handle.go` (new), `internal/kernel/plugin/registry.go` (new), `internal/kernel/plugin/handle_test.go` (new), `internal/kernel/plugin/registry_test.go` (new), `internal/kernel/plugin/protocol_test.go` (new), `memory-bank/activeContext.md`, `memory-bank/progress.md`
- Risks:
  - (i) Handle does not yet implement `kernel.HookSubscriber` — until the parent-package adapter `handle_hooksub.go` lands, plugins cannot participate in hook dispatch even if registered. Acceptable per creative §11 (adapter is in parent package by design); next pass should land it together with `Hub.plugins` field + `allHookSubscribers()` extension to `append(subs, asHookSub(h.plugins.All()...)...)`.
  - (ii) `Handle.SendToolCall` returns the pending channel without an explicit deadline — caller (kernel side) is responsible for setting a timer per `register.tools[].deadline_ms` (creative §2.7) and calling `CancelPending` on expiry. The runtime/supervisor in the next pass owns this loop.
  - (iii) No `runtime.go` yet — there is no entry point that actually spawns a plugin process. Manifest parsing (TOML) is also still a stub (the `Manifest` struct exists but no parser). These are explicit next-pass items; this pass deliberately landed only the in-memory primitives so subsequent BUILD passes have clear, narrow seams to fill.
  - (iv) Interface equality `entry.sub == exclude` in HookEngine (Agent 3 risk ii) will work for `*plugin.Handle` once the adapter wraps it — both implementers will be pointer types so `==` reduces to pointer comparison. Documented in handle.go godoc.
- Open issues:
  - D2.1 next steps: `internal/kernel/plugin/runtime.go` (PluginRuntime interface + default impl with `os/exec` spawn + stdin/stdout pipe wiring), `internal/kernel/plugin/supervisor.go` (restart/backoff loop per creative §2.8), `internal/kernel/plugin/manifest.go` TOML parser (BurntSushi/toml dep needs adding to go.mod), `internal/kernel/handle_hooksub.go` (parent-package adapter implementing kernel.HookSubscriber over *plugin.Handle).
  - D1.7: `Hub.toolExec map[string]string` → `map[string]toolDispatch` (creative §4) — now unblocked since `*plugin.Handle` exists.
  - D1.10: `bootConfig.Tools` rename → `Skills` with legacy fallback per creative §9.
  - D2.14: `Hub.RegisterPlugin`/`LoadPlugins` builder API in `kernel.go` once `runtime.go` exists.
  - D1.12 replacement skill-exec hook integration tests (~50 LOC) still not written.
  - `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` in `kernel.go` unused post D1.2 — Agent 1/2 BUILD risks (ii)/(i) still standing; defer to a small follow-up batch with D1.10.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Phase 2 D2.16 HookSubscriber interface introduction
- Work: Landed the highest-leverage Phase 2 lever per creative `creative-plugin-runtime.md` §2 / D2.16 (option C). Sequence:
  - **New file `internal/kernel/hook_subscriber.go`**: declares the `HookSubscriber` interface (Name/Session/IsConnected/Hooks/SendMsg/Done — exactly the 6 surface methods identified by Agent 9 PLAN analysis). Godoc cites creative §2 and notes both `*Client` and the future `*plugin.Handle` are intended implementers.
  - **`client.go` adapter additions**: added trivial getter methods `Name()`, `Session()`, `Hooks()` on `*Client` to complete HookSubscriber satisfaction (`IsConnected`, `SendMsg`, `Done` already existed). Zero behavior change.
  - **`hook_engine.go` rewrite (full file)**: `hookEntry.client *Client` → `hookEntry.sub HookSubscriber` (with renamed `subscrip HookSubscription` field to disambiguate from `sub`). `RebuildIndex(clients []*Client)` → `RebuildIndex(subs []HookSubscriber)`. `DispatchExcept(..., exclude *Client)` → `DispatchExcept(..., exclude HookSubscriber)`. All `entry.client.X` → `entry.sub.X()` method calls. Internal `c := entry.client` shadow in `sendAndWait` renamed to `s := entry.sub`. Priority-sort, pendingHook map, injectSession, void/modifying/claiming dispatch logic untouched (per creative §2 "no duplication of priority/pending logic").
  - **`hooks.go` Hub-level wiring**: `h.dispatchHookExcept` signature widened to accept `exclude HookSubscriber`; `rebuildHookIndex()` now calls `h.allHookSubscribers()` instead of `h.allClients()`.
  - **`helpers.go`**: added `(h *Hub) allHookSubscribers() []HookSubscriber` returning the Client union (cast through interface). Godoc explicitly notes Phase 2 D2.1 will extend this with `h.plugins.All()` once the plugin registry lands — single merge point for the future plugin handle adapter.
  - **`policy.go` follow-on**: `dispatchHookExcept` signature change propagated naturally — `*Client` satisfies `HookSubscriber` so existing `sender` arguments at `policy.go:102` (before_tool_call) and `policy.go:132` (before_spawn) compile without callsite edits.
  - **Validation**: `go build ./...` clean; `go vet ./...` clean; `go test ./internal/kernel/ ./cmd/tabula/ -count=1 -timeout 180s` PASSES (`internal/kernel` ~28s green; `cmd/tabula` 0.5s). All 29 t.Skip-marked tests still skipped per Phase 1; non-skipped hook integration tests (`hooks_test.go`'s broad coverage of before_message/before_tool_call modify+block, session_start/session_end, claim, timeout, priority ordering, exclude semantics) all pass against the refactored engine — interface change is fully behavior-preserving.
- Addressed: Agent 2 BUILD open issue D2.16 (HookSubscriber interface — the explicitly-flagged "highest-leverage next item"). Unblocks Agent 9 PLAN's D2.16 option C ratification at the code level. The `allHookSubscribers()` helper is the prepared join point for D2.1 plugin handle adapter.
- Files: `internal/kernel/hook_subscriber.go` (new, 21 LOC), `internal/kernel/hook_engine.go` (rewritten, 276 LOC), `internal/kernel/client.go` (3 getter methods added), `internal/kernel/hooks.go` (`exclude` typed widened), `internal/kernel/helpers.go` (`allHookSubscribers` added, 11 LOC), `memory-bank/activeContext.md`, `memory-bank/progress.md`
- Risks:
  - (i) Test environments in `hooks_test.go`/`tool_hook_test.go` build their own `*Client` via local helpers and don't touch `*HookSubscriber` directly — so today there's no in-tree mock implementation of HookSubscriber. When D2.1 PluginRuntime lands, the plugin handle adapter will be the second implementer; until then the interface is single-implementer. Acceptable per creative §2 — interface introduction is the prep step, not the value delivery.
  - (ii) Interface equality `entry.sub == exclude` works because both `*Client` and (future) `*plugin.Handle` are pointer types — interface comparison reduces to pointer comparison. Documented implicitly in creative §2 (HookEngine pendingHook map remains id-based).
- Open issues:
  - D2.1: PluginRuntime in `internal/kernel/plugin/` subpackage per creative §11 — main remaining BUILD lever; once `*plugin.Handle` implements HookSubscriber, just one line in `allHookSubscribers()` to merge plugin registry.
  - D1.7: `Hub.toolExec map[string]string` → `map[string]toolDispatch` per creative §4 (Phase 2 prereq for plugin tool dispatch).
  - D1.10: `bootConfig.Tools` rename → `Skills` with legacy fallback per creative §9.
  - D2.14: `Hub.RegisterPlugin`/`LoadPlugins` builder API once `internal/kernel/plugin/` exists.
  - D1.12 replacement skill-exec hook integration tests (~50 LOC) still not written.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Phase 2 plugin dispatch bridge integrator (D2.17)
- Work: Built the bridge between the existing PluginRuntime scaffolding and the kernel dispatch/hook paths.
  - Added `Hub.plugins *plugin.Registry` initialization and merged registered plugin handles into `allHookSubscribers()` via a parent-package `pluginHookSubscriber` adapter (`internal/kernel/handle_hooksub.go`) so plugins can receive hook `event` messages without importing parent `kernel` from the `plugin` subpackage.
  - Implemented plugin tool dispatch for existing `toolDispatch{Source: toolSourcePlugin}` entries: `ToolService` now sends protocol `tool_call` messages, enforces `deadline_ms` defaults/clamps, handles plugin crash/timeout cleanup, relays `tool_result` back to sessions, and emits `after_tool_call` on completion.
  - Added `plugin_tools.go` helpers for pre-runtime registration seams: `registerPluginHandle`, atomic plugin tool replacement, inbound `tool_result`/`event_reply`/`update_tools`/`log` dispatch, and action mapping `ok/rewrite/deny/claim` → kernel hook actions.
  - Tightened `plugin.Handle` pending semantics: `SendEvent` registers pending interactive hook callIds; delivered/cancelled/closed pending channels are closed to avoid goroutine leaks.
  - Propagated hook `session` into generated hook messages so the plugin event adapter can preserve session context.
  - Added focused tests covering plugin registry→hook index merge, `update_tools` dispatch replacement, plugin tool_call round-trip, plugin hook event_reply denial, and tool timeout pending cleanup.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 4 risks (i) Handle not hook-dispatchable — addressed by `pluginHookSubscriber` adapter and `Hub.plugins` merge point; (ii) caller-owned deadlines — addressed in `ToolService.handlePluginTool`; partially addressed D1.7/D2.10 by making plugin tool dispatch and `update_tools` map replacement live against in-memory handles.
- Files: `internal/kernel/kernel.go`, `internal/kernel/helpers.go`, `internal/kernel/hook_engine.go`, `internal/kernel/handle_hooksub.go` (new), `internal/kernel/tool_service.go`, `internal/kernel/tool_dispatch.go`, `internal/kernel/plugin_tools.go` (new), `internal/kernel/plugin/handle.go`, `internal/kernel/plugin_tools_test.go` (new), `internal/kernel/tool_dispatch_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks:
  - (i) The new bridge is still pre-runtime and relies on in-memory `*plugin.Handle`; `runtime.go`/`supervisor.go` must wire process stdout into `handlePluginProtocolMessage` and stdin into `Handle.SetWriter` before real plugins can run.
  - (ii) `pluginHookSubscriber.SendMsg` intentionally ignores write errors because HookSubscriber has no error return; runtime follow-up should log protocol write failures and close/restart the plugin through supervisor.
  - (iii) `plugin send` messages are logged as not-yet-routed; bus emission from plugins still needs implementation.
  - (iv) `registerPluginHandle` is unexported scaffolding; public `Hub.RegisterPlugin`/`LoadPlugins` builder API still needed to enforce manifest parsing/version/register handshake policy.
- Open issues:
  - D2.1 remaining: `internal/kernel/plugin/runtime.go` + `supervisor.go` for live process spawn/stdin/stdout loops, malformed-message threshold, crash restart/backoff, and PG shutdown.
  - D2.1 manifest parser: implement `plugin.toml` parsing/validation and add TOML dependency or local parser decision.
  - D2.14/D2.7: public `Hub.RegisterPlugin`/`LoadPlugins`, `plugin.BootEntry` consumption, and boot-time `plugins` config wiring between `NewHub` and `StartReaper`.
  - D1.10: `bootConfig.Tools` rename → `Skills` with one-cycle legacy fallback remains open.
  - D1.12 replacement skill-exec hook integration tests still not written; 29 skipped legacy builtin tests remain.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

### BUILD Progress Entry — 2026-04-26 Agent 5
- Requirement: Continue Phase 2 PluginRuntime integration after static handle/registry scaffolding by connecting plugin handles to HookSubscriber and tool dispatch paths without waiting for live process supervision.
- Approach: Reused creative §2/§4/§11 seams: parent-package adapter avoids import cycles, `Hub.plugins` stays separate from ClientRegistry, single `Hub.toolExec` table remains the authoritative dispatch registry, and protocol inbound handling is centralized for the future runtime stdout loop.
- Files modified: `internal/kernel/kernel.go`, `helpers.go`, `hook_engine.go`, `handle_hooksub.go`, `tool_service.go`, `tool_dispatch.go`, `plugin_tools.go`, `plugin/handle.go`, `plugin_tools_test.go`, `tool_dispatch_test.go`, plus Memory Bank updates.
- Testing results: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` passed; `go vet ./...` passed; `go build ./...` passed.

#### Agent 6 — [CONTRIBUTE]
- Role: Phase 2 plugin manifest parser implementer (D2.1 manifest validation)
- Work: Closed the manifest-parser slice of D2.1 so future `Hub.RegisterPlugin`/`LoadPlugins` can load plugin descriptors from disk before spawning processes.
  - Implemented `plugin.LoadManifest(path)` and `plugin.ValidateManifest(*Manifest)` in `internal/kernel/plugin/manifest.go` using BurntSushi TOML. The parser accepts either a direct `plugin.toml` path or a plugin directory, derives `RootDir`, preserves `[config.defaults]`, parses advisory `[[tools]]`/`[[hooks]]`, and returns typed `*ManifestError` for configuration failures.
  - Enforced frozen schema rules from `creative-manifest-schemas.md` §1: required `id/name/version/runtime/entry`, `id` regex `^[a-z0-9_-]+$`, SemVer-shaped `X.Y.Z`, runtime allowlist `python|node`, relative `entry` without `..`, non-empty tool names and hook events, and non-negative tool deadlines.
  - Added `internal/kernel/plugin/manifest_test.go` with happy path, directory path, invalid value table, and direct `ValidateManifest` coverage. Added `github.com/BurntSushi/toml v1.5.0` to `go.mod`/`go.sum`.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 5 open issue “D2.1 manifest parser: implement `plugin.toml` parsing/validation and add TOML dependency or local parser decision.” This removes the manifest-loading blocker for the public builder; runtime/supervisor remain the main D2.1 gap.
- Files: `internal/kernel/plugin/manifest.go`, `internal/kernel/plugin/manifest_test.go` (new), `go.mod`, `go.sum`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: New TOML dependency is now part of the kernel module surface; runtime follow-up should decide where WARN logging for unknown manifest keys belongs because `LoadManifest` intentionally tolerates them silently for forward compatibility.
- Open issues:
  - D2.1 remaining: `internal/kernel/plugin/runtime.go` + `supervisor.go` for live process spawn/stdin/stdout loops, register_request/register handshake, malformed-message threshold, crash restart/backoff, and PG shutdown.
  - D2.14/D2.7: public `Hub.RegisterPlugin`/`LoadPlugins`, `plugin.BootEntry` consumption, and boot-time `plugins` config wiring between `NewHub` and `StartReaper`.
  - D1.10: `bootConfig.Tools` rename → `Skills` with one-cycle legacy fallback remains open.
  - D1.12 replacement skill-exec hook integration tests still not written; 29 skipped legacy builtin tests remain.
  - `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` in `kernel.go` still unused post-D1.2 and can be removed in a small cleanup pass.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Phase 2 default PluginRuntime implementer (spawn/register handshake slice)
- Work: Closed the first live-runtime slice of D2.1 without touching the public Hub builder yet.
  - Added `internal/kernel/plugin/runtime.go` with `Runtime`, `SpawnOptions`, `DefaultRuntime`, and `NewRuntime()`. The default runtime validates manifests, starts plugin entry scripts through `os/exec`, sets `TABULA_PLUGIN_ID`/`TABULA_PLUGIN_PROTOCOL_VERSION`, wires NDJSON stdin/stdout/stderr, sends `register_request`, validates `register` protocol version and plugin id, marks the `Handle` registered, forwards post-register messages through `OnMessage`, and closes/terminates the process group on EOF or malformed-message threshold.
  - Added `process_group_unix.go`/`process_group_windows.go` helpers so runtime spawn uses a separate process group on Unix and has a Windows fallback. Added `Handle.SendRegisterRequest` to keep register preamble writing inside the Handle abstraction.
  - Added `runtime_test.go` covering successful fake-plugin spawn/register plus post-register log dispatch, protocol-version mismatch rejection, and three-malformed-lines threshold rejection. Tests use `/bin/sh` as a runtime override to avoid depending on a Python fixture.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 6 open issue for `runtime.go` live process spawn/stdin/stdout loops, `register_request`/`register` handshake, malformed-message threshold, and basic PG shutdown mechanics. Manifest parser and in-memory dispatch bridge from Agents 5–6 now have a live process surface to attach to.
- Files: `internal/kernel/plugin/runtime.go` (new), `internal/kernel/plugin/runtime_test.go` (new), `internal/kernel/plugin/process_group_unix.go` (new), `internal/kernel/plugin/process_group_windows.go` (new), `internal/kernel/plugin/handle.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks:
  - Runtime is still not wired into `Hub.RegisterPlugin`/`LoadPlugins`; `OnMessage` is only a callback seam until the parent package calls `handlePluginProtocolMessage` and `registerPluginHandle`.
  - Restart/backoff policy from creative §2.8 is not implemented; `supervisor.go` remains the next D2.1 slice.
  - Plugin process PIDs are not yet registered in `ProcessSupervisor`/`afterSpawn`, so unified diagnostics/shutdown bookkeeping is incomplete even though runtime configures and terminates process groups locally.
- Open issues:
  - D2.1: implement `internal/kernel/plugin/supervisor.go` restart/backoff (1s→30s, max 5 restarts/60s, reset after 120s clean run) and tests.
  - D2.14/D2.7: add public `Hub.RegisterPlugin`/`LoadPlugins` bridge using `plugin.LoadManifest` + `plugin.DefaultRuntime`, register handles with `registerPluginHandle`, and route runtime `OnMessage` into `handlePluginProtocolMessage`.
  - D2.9/D2.4: integrate plugin process bookkeeping with `ProcessSupervisor`/shutdown diagnostics or document why runtime-local PG supervision is sufficient.
  - D1.10: `bootConfig.Tools` rename → `Skills` with one-cycle legacy fallback remains open.
  - D1.12 replacement skill-exec hook integration tests still not written; 29 skipped legacy builtin tests remain.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Phase 2 public plugin builder / boot config integrator (D2.14/D2.7 bridge)
- Work: Bridged the live `plugin.DefaultRuntime` slice into the parent Hub and boot loader.
  - Added `internal/kernel/plugin_runtime.go` with `Hub.RegisterPlugin(*plugin.Manifest, config)` and `Hub.LoadPlugins([]plugin.BootEntry)`. `RegisterPlugin` invokes the injectable runtime with `PluginProtocolVersion`, routes runtime `OnMessage` to `handlePluginProtocolMessage`, routes `OnExit` to `handlePluginExit`, and installs the registered handle through `registerPluginHandle`. `LoadPlugins` loads `plugin.toml` manifests, logs per-plugin failures, continues degraded, and returns a joined diagnostic error.
  - Added `Hub.pluginRuntime plugin.Runtime` default initialization in `NewHub` so tests can inject fakes without changing the stable `NewHub` signature.
  - Added `handlePluginExit` cleanup that removes tools/hooks only when the exiting handle is still the current registered handle, preventing stale exit callbacks from deleting a replacement plugin's dispatch entries.
  - Wired boot `plugins` config in `cmd/tabula/main.go`: import `internal/kernel/plugin`, add `BootConfig.Plugins []plugin.BootEntry`, and call `hub.LoadPlugins(bootConfig.Plugins)` before `hub.StartReaper()` in both startup paths.
  - Added `internal/kernel/plugin_runtime_test.go` covering RegisterPlugin runtime option propagation + dispatch install, failure-no-mutation, stale-exit replacement safety, and LoadPlugins continue-after-manifest-failure.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 3 open issue D2.14/D2.7 public `Hub.RegisterPlugin`/`LoadPlugins`, runtime `OnMessage` bridge into `handlePluginProtocolMessage`, handle registration through `registerPluginHandle`, and boot-time `plugins` config wiring.
- Files: `internal/kernel/kernel.go`, `internal/kernel/plugin_runtime.go` (new), `internal/kernel/plugin_runtime_test.go` (new), `cmd/tabula/main.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: `LoadPlugins` currently returns joined errors while boot logs them as warnings and continues; this matches creative degraded-boot policy but should be documented in authoring docs. Restart/backoff still absent, so a crashed plugin is removed rather than supervised/restarted. Plugin processes are still not registered in `ProcessSupervisor`/diagnostic snapshots.
- Open issues: D2.1 `internal/kernel/plugin/supervisor.go` restart/backoff remains the next main slice; D2.9/D2.4 ProcessSupervisor/afterSpawn diagnostics still open; `Hub.SnapshotPlugins` not implemented; plugin `send` remains logged as not-yet-routed; D1.12 replacement skill-exec hook integration tests and unused `enabledBuiltins` cleanup remain open.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 1 — [CONTRIBUTE]
- Role: Phase 2 plugin supervision implementer (D2.1 restart/backoff policy)
- Work: Landed the standalone `internal/kernel/plugin.Supervisor` slice around the existing `plugin.Runtime` so crash recovery policy is implemented and testable before Hub integration.
  - Added `internal/kernel/plugin/supervisor.go` with `Supervisor`, `SupervisorPolicy`, `SupervisorOptions`, default policy constants, `NewSupervisor`, context-cancellable backoff sleeps, `OnStart`/`OnFinalExit` callbacks, rolling restart-window accounting, exponential backoff, max-backoff cap, restart-budget exhaustion, and clean-run reset after the configured threshold.
  - Added `NonRestartableError`/`NonRestartable(err)` and taught `isRestartableSpawnError` to stop retry loops for `ManifestError` and non-restartable protocol/config failures.
  - Updated `runtime.go` so invalid `register` params, protocol-version mismatch, and `Handle.MarkRegistered` rejection are wrapped as non-restartable errors, matching creative protocol §2.6/§2.8 (schema/version failures are not auto-restarted).
  - Updated `plugin.Handle` with a `startedAt` timestamp recorded by `MarkAlive()` plus `StartedAt()` accessor; the supervisor uses this to decide whether a run was clean enough to reset crash counters.
  - Added `internal/kernel/plugin/supervisor_test.go` covering default-shaped restart budget (1s,2s,4s,8s,16s before 5/60 exhaustion), max-backoff cap, clean-run reset, manifest-error non-retry, and non-restartable protocol-error non-retry via a deterministic fake runtime/clock.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 4 open issue D2.1 `internal/kernel/plugin/supervisor.go` restart/backoff policy and tests. Also hardened Agent 3 runtime negative-path semantics by preventing protocol/register schema errors from being treated as transient restartable crashes.
- Files: `internal/kernel/plugin/supervisor.go` (new), `internal/kernel/plugin/supervisor_test.go` (new), `internal/kernel/plugin/handle.go`, `internal/kernel/plugin/runtime.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks:
  - (i) The supervisor is not yet wired into `Hub.RegisterPlugin`; live boot still calls `plugin.Runtime.Spawn` directly, so process exits still remove plugin handles rather than restart them until the next BUILD pass integrates `plugin.NewSupervisor` into the Hub lifecycle.
  - (ii) `Supervisor` currently observes `Runtime.OnExit` callbacks but does not yet expose plugin PID/PG metadata to `ProcessSupervisor`; D2.9/D2.4 unified reaper/diagnostic bookkeeping remains open.
  - (iii) `Handle.SendShutdown()` on context cancellation can fail when a handle is already closed; current code best-effort ignores that error. Hub-level shutdown integration should choose/log graceful shutdown vs forced PG termination explicitly.
- Open issues:
  - Wire `plugin.Supervisor` into `Hub.RegisterPlugin`/`LoadPlugins` (likely asynchronous monitor) so restarts reinstall fresh handles and stale-exit cleanup remains safe.
  - Add ProcessSupervisor/afterSpawn PID bookkeeping or an explicit diagnostic alternative, then implement `Hub.SnapshotPlugins()`.
  - `plugin send` remains logged as not-yet-routed; D1.12 replacement skill-exec hook integration tests and unused `enabledBuiltins` cleanup remain open.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

### BUILD Progress Entry — 2026-04-27 Agent 1
- Requirement: Continue Phase 2 PluginRuntime by implementing the frozen crash-recovery restart/backoff policy from creative-plugin-protocol.md §2.8 without destabilizing existing Hub plugin builder code.
- Approach: Added supervision as a standalone wrapper around the existing `plugin.Runtime` interface with deterministic unit tests, preserving the current `Hub.RegisterPlugin` path for the next integration pass.
- Files modified: `internal/kernel/plugin/supervisor.go`, `internal/kernel/plugin/supervisor_test.go`, `internal/kernel/plugin/handle.go`, `internal/kernel/plugin/runtime.go`, plus Memory Bank updates.
- Testing results: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` passed; `go vet ./...` passed; `go build ./...` passed.

#### Agent 2 — [CONTRIBUTE]
- Role: Hub supervisor lifecycle integrator / plugin process diagnostics bridge
- Work: Wired the standalone `plugin.Supervisor` into the public Hub plugin lifecycle.
  - `Hub.RegisterPlugin` now starts `plugin.NewSupervisor(...)` instead of directly calling `plugin.Runtime.Spawn`; it waits for the first successful `OnStart` so boot-time `LoadPlugins` still has deterministic success/failure semantics, while later crashes restart asynchronously.
  - Added `pluginRuns` lifecycle tracking with per-plugin cancellation; re-registering the same plugin id cancels the prior supervisor without relying on stale exit callbacks. Restarted handles reinstall through `registerPluginHandle`, preserving stale-exit replacement safety and updating tool dispatch to the fresh handle.
  - Added `plugin.SpawnOptions.OnStartProcess`, `Handle.SetPID/PID`, and runtime PID recording. `DefaultRuntime` invokes the callback after `cmd.Start`; Hub registers the plugin PID with `ProcessSupervisor` and marks it exited from the runtime `OnExit` path, avoiding a second `cmd.Wait` while still exposing plugin process state for diagnostics.
  - Extended `plugin_runtime_test.go` with supervisor restart→fresh handle/tool-dispatch reinstall coverage, runtime callback wiring coverage, ProcessSupervisor registration coverage, and exit-marking coverage.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 1 BUILD open issue to wire `plugin.Supervisor` into `Hub.RegisterPlugin` without breaking stale-exit replacement safety; partially addressed D2.9/D2.4 by adding runtime→Hub `OnStartProcess` PID bookkeeping through `ProcessSupervisor` plus `OnExit`-based exit marking.
- Files: `internal/kernel/kernel.go`, `internal/kernel/plugin_runtime.go`, `internal/kernel/plugin_runtime_test.go`, `internal/kernel/plugin/runtime.go`, `internal/kernel/plugin/handle.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks:
  - D2.15 diagnostics still need a durable lifecycle-state model (`running/restarting/failed`, restart count, last error). Current `pluginRuns` intentionally only tracks cancellation/done and is not sufficient as a final `SnapshotPlugins()` data source.
  - `ProcessSupervisor` integration is diagnostic/bookkeeping only for plugin processes; runtime remains the only owner that waits on the plugin process, so existing `afterSpawn` watcher semantics are intentionally not reused for plugins.
  - Hub cancellation currently relies on supervisor context cancellation plus runtime-local PG termination; explicit graceful-vs-forced shutdown logging can be improved when `SnapshotPlugins()` lands.
- Open issues: `Hub.SnapshotPlugins()`; plugin `send` routing; D1.10 boot `Tools`→`Skills` legacy fallback; D1.12 replacement skill-exec hook integration tests; unused `enabledBuiltins` cleanup.
- Quality: Accuracy 4/5, Completeness 4/5, Coherence 5/5, Applicability 4/5, Mission 5/5

#### Agent 3 — [CONTRIBUTE]
- Role: Plugin diagnostics snapshot implementer (D2.15)
- Work: Closed the `Hub.SnapshotPlugins()` diagnostics gap left by Agent 2.
  - Added durable `pluginLifecycleState` tracking in `Hub` (`running`/`restarting`/`failed`/`stopped`, `restart_count`, `last_error`) separate from cancellation-only `pluginRuns` and live `plugin.Registry` handles.
  - Extended `plugin.SupervisorOptions` with `OnRestart`; Hub records restart attempts and removes the dead handle/tools while the plugin is in backoff so active dispatch does not point at a crashed plugin.
  - Implemented `Hub.SnapshotPlugins()` in `snapshot.go` with the frozen diagnostic shape: id/status/pid/restart_count/last_error/registered_tools/subscriptions/registered_at. Failed terminal state is retained after the live handle is removed.
  - Exposed the new diagnostics via `GET /internal/snapshot/plugins` in `cmd/tabula/main.go` alongside the existing `/sessions` snapshot.
  - Added tests for running plugin snapshot contents, failed-state retention after final exit, and supervisor restart notifications.
  - Validation: `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s` PASS; `go vet ./...` PASS; `go build ./...` PASS.
- Addressed: Agent 2 risk/open issue D2.15 (`SnapshotPlugins` and lifecycle state beyond `pluginRuns`); partially improved Agent 2 dispatch-safety risk by removing crashed handles/tools during restart backoff.
- Files: `internal/kernel/kernel.go`, `internal/kernel/plugin_runtime.go`, `internal/kernel/plugin/supervisor.go`, `internal/kernel/plugin/supervisor_test.go`, `internal/kernel/snapshot.go`, `internal/kernel/snapshot_test.go`, `cmd/tabula/main.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: `SnapshotPlugins` now exposes lifecycle diagnostics but does not yet provide a documented public contract in authoring/admin docs; plugin `send` routing remains unimplemented and still logs as not-yet-routed.
- Open issues: plugin `send` routing; D1.10 boot `Tools`→`Skills` legacy fallback; D1.12 replacement skill-exec hook integration tests; unused `enabledBuiltins` cleanup; full live E2E/reference-plugin coverage.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 4 — [CONTRIBUTE]
- Role: Plugin protocol `send` bus-routing implementer (D2.24)
- Work: Routed inbound plugin protocol `send` messages through the existing kernel bus/session broadcast path. `handlePluginProtocolMessage` now delegates `MethodSend` to `handlePluginSend`, which decodes `plugin.SendParams`, accepts only `channel="bus"`, validates a non-empty `type`, and broadcasts `Message{Type, Session, Payload}` to session receivers plus `receives_global` clients. Added focused tests for session delivery, global delivery, cross-session non-delivery, and unknown-channel WARN+drop behavior.
- Addressed: Agent 3 open issue “plugin `send` routing”; removed the `plugin send not yet routed` behavior from `plugin_tools.go` while preserving creative §2.6 unknown-channel drop semantics.
- Files: `internal/kernel/plugin_tools.go`, `internal/kernel/plugin_tools_test.go`, `memory-bank/tasks.md`, `memory-bank/activeContext.md`, `memory-bank/progress.md`
- Risks: `send` currently forwards only `Type`, `Session`, and `Payload`; no plugin identity metadata is attached to the bus message yet. If downstream consumers require provenance, a later pass should add a stable meta convention and docs. Full live E2E/reference-plugin coverage remains open.
- Open issues: D1.10 boot `Tools`→`Skills` legacy fallback; D1.12 replacement skill-exec hook integration tests; unused `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` cleanup; full live E2E/reference-plugin coverage.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 5 — [CONTRIBUTE]
- Role: Phase 1 test-coverage repair / handoff verification
- Work: Closed the actionable D1.12 coverage gap for before-tool-call hook integration by retargeting live tests away from removed kernel builtins and onto synthetic dynamic skill exec dispatch. `hooks_test.go::newTestEnvWithSkillTool` now registers only `echo_tool` backed by `cat`, so hook firing/blocking/security-timeout coverage exercises `SkillExec.Run` through the unified `Hub.toolExec` path. Retargeted four formerly skipped `tool_hook_test.go` tests (input rewrite, infinite approval wait, disconnect fail-closed, sender-exclusion no-deadlock) plus the remaining active `hooks_test.go` shell_exec-based cases to `echo_tool`; remaining skips are now limited to `process_spawn`/spawn-token/MaxChildren dead-code tests and legacy kernel fixture debt. Verified current code already has D1.10 `BootConfig.Skills` with deprecated `Tools` fallback and that `enabledBuiltins`/`IsBuiltinEnabled`/`parseEnabledBuiltins` no longer exist in-tree.
- Addressed: Agent 4 open issue D1.12 replacement skill-exec hook integration tests (substantially resolved for active hook coverage); corrected stale handoff items for D1.10 and unused builtin cleanup based on actual code state.
- Files: `internal/kernel/hooks_test.go`, `internal/kernel/tool_hook_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: `newTestEnv` in `kernel_test.go` still carries a legacy all-four builtin `toolsJSON` fixture used by many general tests; it does not enable execution, but a future cleanup should replace it with a neutral/dynamic skill fixture to reduce stale test vocabulary. Remaining skipped spawn tests are intentional D1.11(b) dead-code-keep until subagent plugin GA.
- Open issues: Phase 3 live E2E/reference-plugin coverage; optional `kernel_test.go` fixture cleanup and skip-count audit for remaining D1.11-only tests.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 6 — [CONTRIBUTE]
- Role: Phase 1 kernel test fixture cleanup / dynamic skill coverage repair
- Work: Replaced `kernel_test.go::newTestEnv`'s legacy all-four builtin `toolsJSON` fixture with neutral dynamic skill fixtures (`echo_tool` and a test-only `test_shell` skill backed by `SkillExec`). Retargeted shell-exec-equivalent tests that only exercise dynamic command dispatch/result routing (`TestExecBasic`, error/timeout/truncation/stderr/special-character cases, parallel batch and cross-session routing, invalid payload) from removed `ToolShellExec` to `test_shell` and unskipped them. Strengthened the removed-builtin regression test to assert all four legacy kernel builtin names are absent from `init.Tools` and rejected as unknown if called. Verified skip count now maps only to D1.11(b) process-spawn/dead-code coverage: 14 callers in `kernel_test.go`, 1 in `hooks_test.go`, and 1 in `tool_hook_test.go`.
- Addressed: Agent 5 open issue to clean `kernel_test.go` legacy builtin fixture and audit remaining skips; further reduced D1.12 coverage gap by restoring non-spawn shell-style dynamic-dispatch tests through skill exec.
- Files: `internal/kernel/kernel_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The test-only `test_shell` fixture depends on `python3` availability, consistent with existing plugin/runtime tests and project environment; remaining skipped tests intentionally cover removed `process_spawn`/spawn-token/MaxChildren/session lifecycle semantics and should wait for a subagent-plugin replacement path.
- Open issues: Full live E2E/reference-plugin coverage (Phase 3); D1.11(b) spawn-related skipped tests remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 7 — [CONTRIBUTE]
- Role: Phase 3 reference plugin SDK + live E2E implementer
- Work: Landed the first full live reference-plugin path. Added a minimal Python SDK under `examples/plugin-sdk-python/` (`PluginAPI`, protocol helpers, `register_request` bootstrap, tool/event dispatch, `send`, `log`, `update_tools`, shutdown loop). Added `examples/plugin-hello/` with `plugin.toml`, `run.py`, and README; the plugin registers `hello_ping`, subscribes to `before_tool_call`, emits a bus event through `send`, logs metric-convention fields, and supports rewrite/deny hook replies. Added `internal/kernel/plugin_live_test.go::TestPluginHelloLiveE2E`, which starts the real Python subprocess via `Hub.RegisterPlugin`, verifies live register/tool dispatch, `hello_ping` tool_result, plugin bus event delivery, hook rewrite+deny event_reply, and `SnapshotPlugins` diagnostics. Added `scripts/test-plugin-hello.sh` wrapper. Validation passed: `scripts/test-plugin-hello.sh`; `go test ./internal/kernel/ ./internal/kernel/plugin/ ./cmd/tabula/ -count=1 -timeout 180s`; `go vet ./...`; `go build ./...`.
- Addressed: Agent 6 open issue “Full live E2E/reference-plugin coverage (Phase 3)” for the core live protocol path (manifest → runtime spawn/register → tool_call/tool_result → event/event_reply → send/log/snapshot).
- Files: `examples/plugin-sdk-python/pyproject.toml` (new), `examples/plugin-sdk-python/src/tabula_plugin_sdk/__init__.py` (new), `examples/plugin-sdk-python/src/tabula_plugin_sdk/protocol.py` (new), `examples/plugin-sdk-python/src/tabula_plugin_sdk/api.py` (new), `examples/plugin-hello/plugin.toml` (new), `examples/plugin-hello/run.py` (new), `examples/plugin-hello/README.md` (new), `internal/kernel/plugin_live_test.go` (new), `scripts/test-plugin-hello.sh` (new), `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The Python SDK is intentionally Phase 3 minimal and not a final distributable package; Phase 6 still moves SDK authority to `tabula-bundles`. D3.4 child-PG cleanup demonstration remains unimplemented: the reference plugin does not yet spawn a dummy child and assert shutdown/killpg cleanup evidence. The live test depends on `python3`, consistent with existing runtime tests.
- Open issues: Add D3.4 child-process cleanup fixture/evidence if Phase 3 must cover two-tier PG cleanup before Phase 4; otherwise next valuable work is Phase 4 distro tooling (`bundle.toml` components, mixed skill/plugin install, lock v2 plugins). D1.11(b) spawn-related skipped tests remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 8 — [CONTRIBUTE]
- Role: Phase 3 reference plugin child-process cleanup hardener (D3.4)
- Work: Closed the D3.4 child-PG cleanup evidence gap left by Agent 7. Added minimal SDK shutdown callback support (`PluginAPI.on_shutdown`) and made the reference plugin register cleanup handlers. Expanded `examples/plugin-hello` with `hello_spawn_child`, which launches a dummy `sleep` child in its own process group and returns its PID; shutdown now SIGTERMs/SIGKILLs tracked children. Added `TestPluginHelloCleansUpChildProcessOnShutdown`, which starts the real plugin, invokes the child-spawning tool, verifies the child is live, sends graceful plugin `shutdown`, and asserts the child exits. Updated the live E2E tool-catalog assertion for the two reference tools. Stabilized `TestRegisterPluginSupervisorRestartsAndReinstallsHandle` by adding a deterministic first-exit delay to the fake runtime so it cannot race past the initial-handle assertion on faster machines.
- Addressed: Agent 7 open issue “D3.4 child-process cleanup fixture/evidence”; Phase 3 reference plugin scope now covers register, tool_call/tool_result, event/event_reply, send/log, snapshot diagnostics, and graceful shutdown child cleanup. Also addressed a test race surfaced during validation in the supervisor restart unit test.
- Files: `examples/plugin-sdk-python/src/tabula_plugin_sdk/api.py`, `examples/plugin-hello/run.py`, `examples/plugin-hello/plugin.toml`, `internal/kernel/plugin_live_test.go`, `internal/kernel/plugin_runtime_test.go`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The child cleanup smoke is POSIX-only and skips on Windows because it validates process-group signalling with `sleep`; Windows runtime still uses process kill fallback from `process_group_windows.go`. Reference SDK remains Phase 3 minimal and not the final distributable SDK authority.
- Open issues: Phase 4 distro tooling is now the next meaningful BUILD area (`bundle.toml` components, mixed skill/plugin install, lock v2 plugins/config/tests). D1.11(b) spawn-related skipped tests remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 5/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 9 — [CONTRIBUTE]
- Role: Phase 4 distro tooling implementer (mixed components, bundle components, lock v2)
- Work: Implemented the first coherent Phase 4 slice in `tools/tabula-distro`: `bundle.toml` now supports `[bundle].components` as a flat relative-path list; install stages `plugins/` alongside `skills/`, installs standalone `[[plugins]]`, routes bundle components by manifest kind (`SKILL.md` vs `plugin.toml`), removes bundle `_`-prefix support-dir special handling, exposes `home/plugins`, and records installed bundle components in lock state. Bumped lock format to v2 with `plugins` plus v1 migrate-on-load; config parsing now has `BundleEntry.components` (with legacy `skills` alias) and `DistroConfig.plugins`; CLI summary/GC includes plugins. Added/updated tests for mixed bundle install, explicit components, legacy fallback, missing component failure, standalone plugin entry, config parsing, and v1→v2 lock migration.
- Addressed: Agent 8 open issue to move to Phase 4 distro tooling; D4.1, D4.2, D4.3, D4.4/D4.11, D4.5, D4.7, D4.9, and D4.10 for the in-repo installer/test surface. Removed stale `_` support-dir expectations in existing tests in line with D4.7.
- Files: `tools/tabula-distro/src/tabula_distro/manifest.py`, `tools/tabula-distro/src/tabula_distro/install.py`, `tools/tabula-distro/src/tabula_distro/lock.py`, `tools/tabula-distro/src/tabula_distro/config.py`, `tools/tabula-distro/src/tabula_distro/cli.py`, `tools/tabula-distro/tests/test_install.py`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: `_refresh_runtime_surface` still preserves `_pylib`/`_tslib` until Phase 6 lib relocation; `_materialize_inline_symlinks` still copies sibling `_*` support dirs for legacy dev symlinks; lock v2 records bundle-sourced component entries with copied bundle `LockEntry` metadata rather than per-component metadata/overrides (acceptable for current flat-list schema but should be revisited if per-component overrides are added).
- Open issues: Phase 4 follow-up should review CLI/update semantics for `update_only` with plugin/component names and any docs/fixtures expecting `_` support dirs; Phase 6 still owns removing runtime-surface `_pylib`/`_tslib` preserves and legacy symlink support-dir behavior. D1.11(b) spawn-related skipped tests remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 10 — [CONTRIBUTE]
- Role: Phase 4 update targeting hardener
- Work: Closed the Agent 9 Phase 4 follow-up around `update_only` semantics. `_resolve` now accepts entry identities instead of checking only source URI, so standalone skill/plugin updates can be targeted by entry name. Added `_bundle_update_names` so bundle updates can also be targeted by bundle-sourced component names, including legacy walk-discovered plugin/skill names recovered from prior lock entries. Updated CLI help to advertise bundle/skill/plugin/component targeting and added fake-git-cache tests for standalone plugin refresh and bundle plugin component refresh.
- Addressed: Agent 9 open issue “review CLI/update semantics for `update_only` with plugin/component names.” Also reduced the lock-v2 component-tracking risk by using copied bundle lock metadata to map component names back to their owning bundle for update targeting.
- Files: `tools/tabula-distro/src/tabula_distro/install.py`, `tools/tabula-distro/src/tabula_distro/cli.py`, `tools/tabula-distro/tests/test_install.py`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Bundle-sourced component targeting relies on matching copied lock-entry origin metadata; this is sufficient for the current flat-list/no-per-component-source schema but should be revisited if future per-component overrides are introduced.
- Open issues: Phase 6 still owns `_pylib`/`_tslib` runtime-surface preserves and legacy inline-symlink sibling `_*` support dirs; Phase 7 docs rewrites remain; D1.11(b) spawn-related skips remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 11 — [CONTRIBUTE]
- Role: Phase 7 authoring documentation implementer
- Work: Added the canonical `docs/PLUGIN_AUTHORING.md` for the implemented PluginRuntime surface: `plugin.toml` schema/validation, NDJSON protocol methods, lifecycle/supervisor behavior, Python SDK example, hook reply actions, child-process cleanup, metrics-via-log convention, and local validation commands. Refreshed `docs/SKILL_AUTHORING.md` to center skills, link plugin authors to the new guide, remove the stale `requires-kernel-tools` status note, annotate `shell_exec` examples as bundle skill tools rather than kernel builtins, and replace direct `skills._pylib` import examples with planned `tabula_plugin_sdk` references. Updated `docs/PHILOSOPHY.md` stable-contract wording and `examples/plugin-hello/README.md` to match the current two-tool/child-cleanup reference plugin.
- Addressed: ActiveContext Phase 7 docs follow-up for plugin authoring rewrites; D7.2 new `PLUGIN_AUTHORING.md`; part of D7.1/D7.5 (`SKILL_AUTHORING.md` status-note deletion, import-path replacements, `shell_exec` clarification); D7.5a `PHILOSOPHY.md`; reference plugin README drift after Agent 8 child-cleanup addition.
- Files: `docs/PLUGIN_AUTHORING.md` (new), `docs/SKILL_AUTHORING.md`, `docs/PHILOSOPHY.md`, `examples/plugin-hello/README.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: The docs now reference the future packaged `tabula_plugin_sdk` import path before Phase 6 physically removes `_pylib`; this is consistent with D7.5/Path C but should be framed carefully in remaining docs until lib relocation lands. `docs/ARCHITECTURE.md` and `docs/DISTROS.md` still contain `_pylib` runtime-contract text; `docs/distro-config.md` still describes legacy `skills` allowlists and skill-only lock examples.
- Open issues: Continue Phase 7 by updating `docs/ARCHITECTURE.md`, `docs/DISTROS.md`, and `docs/distro-config.md`; rerun docs grep for `_pylib`/`_tslib`/`requires-kernel-tools` (excluding `docs/plans/*`) after that cleanup. Phase 6 code still owns `_refresh_runtime_surface` `_pylib`/`_tslib` preservation and inline-symlink sibling `_*` support-dir removal. D1.11(b) spawn-related skips remain by design until subagent plugin GA.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 12 — [CONTRIBUTE]
- Role: Phase 7 architecture/distro documentation cleanup implementer
- Work: Updated the remaining Phase 7 docs called out by Agent 11. `docs/ARCHITECTURE.md` now describes the implemented skill/plugin architecture: no LLM-visible kernel builtins, separate kernel/client/plugin/SDK versioning, PluginRuntime NDJSON methods, two-tier supervision, SDK packages instead of `_pylib` runtime directories, an active `plugins/` surface, and mixed bundle install boundaries. `docs/DISTROS.md` now includes distro-level `plugins/`, `coder` replacing the removed `ouroboros` narrative, mixed skill/plugin bundle semantics, and plugin-aware install steps. `docs/distro-config.md` now documents `components`, standalone `[[plugins]]`, `[bundle].components`, lock v2 `plugins`, mixed generations layout, and v1 lock migration while framing legacy `skills = [...]` only as a temporary alias.
- Addressed: Agent 11 open issues for `docs/ARCHITECTURE.md`, `docs/DISTROS.md`, and `docs/distro-config.md`; D7.3/D7.4/D7.5 docs cleanup; stale `_pylib` runtime-contract text and skill-only distro/lock examples.
- Files: `docs/ARCHITECTURE.md`, `docs/DISTROS.md`, `docs/distro-config.md`, `memory-bank/tasks.md`, `memory-bank/progress.md`, `memory-bank/activeContext.md`
- Risks: Docs intentionally describe the target packaged-SDK/runtime surface while Phase 6 code still preserves `_pylib`/`_tslib` for legacy runtime compatibility; keep remaining Phase 6 code cleanup gated on actual lib relocation scope.
- Open issues: Phase 6 still owns `_refresh_runtime_surface` `_pylib`/`_tslib` preservation and inline-symlink sibling `_*` support-dir behavior; D1.11(b) spawn-related skipped tests remain by design until subagent plugin GA. Optional next BUILD pass can do a final docs consistency sweep, but the direct Phase 7 docs blockers from Agent 11 are closed.
- Quality: Accuracy 5/5, Completeness 4/5, Coherence 5/5, Applicability 5/5, Mission 5/5

#### Agent 13 — [DECLINE]
- Role: N/A
- Work: Reviewed the current BUILD handoff, latest Phase 7 log entries, and the actual files named in the working set. Re-ran scoped docs searches: non-plan docs no longer contain `skills/_pylib`, `skills._pylib`, `skills/_tslib`, `skills._tslib`, or `requires-kernel-tools`; `docs/README.md` and `docs/tests/README.md` do not exist in this repo, so the optional Agent 10 sweep target is already exhausted. Verified the remaining `_pylib`/`_tslib` hits are in `tools/tabula-distro/src/tabula_distro/install.py` runtime-surface/inline-symlink legacy compatibility code explicitly called out as Phase 6 follow-up, not a safe Agent 13 cleanup.
- Addressed: Confirmed Agent 11/12 docs blockers are closed; confirmed activeContext's two remaining implementation issues are intentionally deferred (`_refresh_runtime_surface`/inline `_*` support dirs until lib relocation; D1.11(b) spawn skips until subagent plugin GA).
- Files: none
- Risks: none new
- Open issues: Phase 6 lib-relocation cleanup remains gated on external SDK/bundles relocation; D1.11(b) spawn-related skipped tests remain by design until a plugin-side replacement exists.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 14 — [DECLINE]
- Role: N/A
- Work: Re-validated the latest BUILD handoff from a different angle against the scoped working set instead of broad project exploration. Confirmed task metadata is complete (`Intent=implement`, `Category=deep`); scoped docs grep still finds only source/lesson docs under `docs/plans/` for `skills/_pylib`, `skills._pylib`, `skills/_tslib`, `skills._tslib`, or `requires-kernel-tools`; `docs/**/README.md` sweep targets do not exist. Read the current architecture/distro docs and confirmed they describe mixed skill/plugin runtime surfaces, packaged SDK contracts, plugin protocol/versioning, and lock v2 semantics. Verified remaining `_pylib`/`_tslib`/`_*` code references are exactly the Phase 6-gated runtime-surface preserve and legacy inline-symlink support-dir logic in `tools/tabula-distro/src/tabula_distro/install.py`, and remaining `skipKernelBuiltinRemoved` callers are the D1.11(b) spawn-token/MaxChildren dead-code coverage class.
- Addressed: Confirmed Agent 13's decline rationale with additional direct reads of `docs/ARCHITECTURE.md`, `docs/DISTROS.md`, `docs/distro-config.md`, `install.py`, and `skip_helpers_test.go`; no new safe BUILD change found.
- Files: none
- Risks: none new
- Open issues: Phase 6 lib-relocation cleanup remains intentionally gated on external SDK/bundles relocation; D1.11(b) spawn-related skips remain intentionally gated on a subagent-plugin replacement path.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 15 — [DECLINE]
- Role: N/A
- Work: Re-read the required Memory Bank context and latest BUILD log, then re-checked the scoped handoff from the remaining-risk angle. Task metadata is complete (`Intent=implement`, `Category=deep`). A scoped docs grep for `skills/_pylib`, `skills._pylib`, `skills/_tslib`, `skills._tslib`, and `requires-kernel-tools` still returns only `docs/plans/*` source/lesson files; `docs/**/README.md` optional sweep targets are absent. Direct reads of `docs/PLUGIN_AUTHORING.md`, `docs/distro-config.md`, `tools/tabula-distro/src/tabula_distro/install.py`, and `internal/kernel/skip_helpers_test.go` confirm the remaining issues are intentional gates rather than safe in-repo BUILD gaps: `_refresh_runtime_surface` preserves `_pylib`/`_tslib` and inline symlink materialization copies sibling `_*` support dirs until Phase 6 lib relocation, while `skipKernelBuiltinRemoved` callers are documented D1.11(b) dead-code coverage waiting for subagent-plugin GA.
- Addressed: Confirmed Agent 13/14 decline rationale with fresh scoped verification; no additional code/doc change would advance the Level 4 implementation without crossing the explicitly deferred Phase 6 or D1.11(b) boundaries.
- Files: none
- Risks: none new
- Open issues: Phase 6 lib-relocation cleanup remains gated on external SDK/bundles relocation; D1.11(b) spawn-token/MaxChildren skipped tests remain gated on plugin-side replacement coverage.
- Quality: Accuracy N/A, Completeness N/A, Coherence N/A, Applicability N/A, Mission N/A

#### Agent 16 — [DECLINE]
- Role: N/A
- Work: Final scoped verification confirmed BUILD is complete. The only remaining items are intentionally deferred Phase 6 lib-relocation work and D1.11(b) spawn-related dead-code coverage; both are outside safe BUILD scope for this repo at this time.
- Addressed: Confirmed the final pipeline state; no further BUILD action required.
- Files: none
- Risks: none new
- Open issues: proceed to SECURITY
- Quality: N/A
