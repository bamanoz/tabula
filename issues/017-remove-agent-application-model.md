# Удалить agent application model

**Type:** AFK
**Blocked by:** 008, 009, 011, 013, 014, 016

## What to build

Атомарно удалить старую app/application surface из source, tests и docs без compatibility aliases или on-disk migration.

## Acceptance criteria

- [x] Удалены `tabula.app.toml`, `[application]`, app topology и app lock contracts.
- [x] Удалены `tabula-install app *`, `TABULA_APP_*` и `app-bindings.toml`.
- [x] Удалён public `tabula-cli` wrapper; `tabula-agent` запускает tenant stack без обязательного client component.
- [x] Broad reference search не находит активных legacy references.
