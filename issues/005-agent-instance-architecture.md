# Зафиксировать архитектуру agent instance installation

**Type:** HITL
**Status:** completed
**Blocked by:** None

## What to build

Добавить ADR, superseding ADR 0002 и ADR 0004. Зафиксировать целевую модель: distro является продуктом; tenant является project-scoped runtime instance agent profile и runtime isolation boundary; project является user-facing workspace context с одним backing tenant; binding выбирает tenant; optional `tabula.agent.toml` содержит только distro source и distro-owned values; host topology не входит в project config; `tabula-agent` является отдельным generic launcher; `tabula` остаётся kernel/operator binary.

## Acceptance criteria

- [x] Новый ADR явно supersedes ADR 0002 и ADR 0004.
- [x] Зафиксированы ownership kernel, runtime, installer, distro, materializer и launcher.
- [x] Зафиксированы project ↔ tenant 1:1 и directory binding conflict semantics.
- [x] Зафиксировано атомарное удаление старой app/application модели без compatibility aliases.
- [x] ADR требует обновлять mutable docs вместе с shipped behavior, не раньше.
