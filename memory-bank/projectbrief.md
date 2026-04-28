# Project Brief

## Current Project Focus
Tabula — агентская платформа на Go с экосистемой skills и долгоживущих компонентов (gateways, drivers, hooks, MCP bridge). Текущая архитектурная инициатива — разделить перегруженную абстракцию «skill» на две: декларативный per-call **skill** и программный long-lived **plugin**, и привести kernel к минимальному инвариантному ядру (bus + hook engine + two-tier supervisor).

## Current Task Context
- Task: Grouped post-reflection skill/plugin architecture follow-ups
- Task ID: skill-plugin-architecture-followups
- Intent: implement
- Category: deep
- Level: 4

## Success Criteria
- Legacy builtin metadata is either removed from runtime-relevant paths or explicitly guarded/documented as non-runtime-only.
- `/internal/snapshot/plugins` has authenticated/local-only behavior or strict locality assumptions documented and verified for non-local deployments.
- External `tabula-bundles` migrations complete for hook, MCP, driver/subagent, gateway plugins, and remaining per-call skills with `tools[].exec`.
- Subagent plugin owns spawn-token/MaxChildren/depth coverage, enabling removal of D1.11(b) dead code and skipped kernel spawn tests.
- Phase 6 SDK/lib relocation removes in-repo and distro `_pylib`/`_tslib` preserve behavior after bundled wheel/tarball packages exist.

## Previous Architecture Success Criteria
- Kernel: пустой LLM-tool catalog (shell_exec, process_* убраны); `RunSkillTool` → internal `SkillExec`.
- Реализован `PluginRuntime` (stdio JSON-RPC) с reference example для Python и далее для TS.
- Все компоненты из §8.1 design doc мигрированы в plugins; компоненты из §8.2 остаются skills с обновлённым `SKILL.md` (включая `exec` в tools).
- `_` префикс libs и `skills = []` allowlist hack удалены; libs переехали в обычные packages.
- Two-tier supervision: kernel → skill/plugin → plugin's children (PG leader pattern).
- MaxChildren/depth/spawn-token инвариант сохранён, переехав внутрь subagent plugin'а.
- Новый `bundle.toml` с unified списком components; distro понимает оба манифеста.
- Документация обновлена: `SKILL_AUTHORING.md`, `PLUGIN_AUTHORING.md` (новый), `ARCHITECTURE.md`, `DISTROS.md`.

## Constraints / Notes
- `SKILL.md` обратно совместим с Anthropic-style skill manifest — ломать формат нельзя.
- Hook-skill миграция инвазивна, без compat shim'ов (§8.4).
- Sandbox, opencode integration, harness distro — out of scope (отдельные документы).
- Capabilities-based dispatch в kernel намеренно НЕ делается.
- `tabula-distrib/ouroboros` удаляется на старте миграции.
