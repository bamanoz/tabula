# Диагностировать и удалять устаревшую регистрацию проекта

**Type:** AFK
**Blocked by:** `001-project-selection-web.md`

## What to build

Добавить диагностику project registry и безопасное удаление устаревшей регистрации. Если tenant или его workspace root исчез/изменился, проект остаётся видимым с вычисляемым статусом и не может быть выбран. `unregister` удаляет только registry entry и никогда не удаляет tenant, sessions, workspace или plugin state.

## Acceptance criteria

- [ ] SDK/inspect вычисляет различимые статусы `tenant_missing`, `root_missing`, `root_mismatch` и valid/active без изменения kernel или tenant metadata.
- [ ] Web отображает диагностический status и понятную причину для stale проекта.
- [ ] Stale project нельзя выбрать или использовать для bootstrap/join.
- [ ] Web предлагает подтверждаемую операцию `unregister` для stale и valid entries.
- [ ] `unregister` атомарно удаляет только registry entry под file lock.
- [ ] После `unregister` tenant directory, sessions, project files и plugin config остаются полностью нетронутыми.
- [ ] Concurrent list/inspect/register/unregister операции не повреждают registry и не теряют записи.
- [ ] Есть unit tests для каждого stale status, unregister safety и concurrent access.
- [ ] Есть frontend/API tests для error state, disabled selection и confirmation flow.
- [ ] Есть installed-layout test, подтверждающий, что unregister не удаляет tenant или session state.

## Blocked by

- `001-project-selection-web.md`

## Explicit non-goals

- Tenant repair, recreation, migration или deletion.
- Project rename.
- Profile reconciliation.
- Telegram integration.
- Изменения kernel, `tabula-distro` или workspace plugin.
