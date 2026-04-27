# Project Brief

## Current Project Focus
Tabula — агентская платформа на Go с экосистемой skills и долгоживущих компонентов (gateways, drivers, hooks, MCP bridge). Текущая архитектурная инициатива — разделить перегруженную абстракцию «skill» на две: декларативный per-call **skill** и программный long-lived **plugin**, и привести kernel к минимальному инвариантному ядру (bus + hook engine + two-tier supervisor).

## Current Task Context
- Task: Реализация skill/plugin архитектуры из `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`
- Task ID: skill-plugin-architecture
- Intent: implement
- Category: deep
- Level: 4

## Success Criteria
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
