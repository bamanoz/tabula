# Выбирать зарегистрированный проект в Web

**Type:** AFK
**Blocked by:** None - can start immediately

## What to build

Добавить optional capability `projects` как library bundle для gateway-интеграции. Реализовать read-only project registry contract и первый вертикальный Web-срез: gateway получает список заранее зарегистрированных проектов, показывает project selector и при выборе переключается на tenant/session выбранного проекта.

Проект является пользовательской записью над существующим tenant:

- `project_id` и display name принадлежат registry;
- `tenant_id` остаётся существующей runtime isolation boundary;
- `project_root` берётся из materialized tenant workspace config;
- project selection не меняет kernel contract и не добавляет сущности в kernel;
- дистрибутивы без `projects` продолжают работать как single-context приложения.

Bundle не создаёт tenant и не управляет `tabula-distro`. Registry хранится в глобальном `$TABULA_HOME/state/plugins/projects/registry.json` и читается/пишется через общую библиотеку с atomic reads/writes и file locking.

## Acceptance criteria

- [ ] В `tabula-bundles` существует optional `projects` library bundle с моделью project registry, reader и tenant resolution API.
- [ ] Registry contract поддерживает `project_id`, display name, `tenant_id`, сохранённый `project_root` snapshot и status/diagnostic fields без хранения `extra_roots`.
- [ ] SDK валидирует project ID, запрещает дубликаты project ID и one-to-many регистрацию одного tenant.
- [ ] SDK читает `project_root` из canonical tenant workspace config и проверяет, что registry snapshot согласован с tenant.
- [ ] `gateway-web` обнаруживает доступность projects capability без изменения kernel и показывает selector только для установленного bundle.
- [ ] Web получает список проектов через gateway API/adapter и отображает project name, root и диагностический status.
- [ ] При выборе valid project Web сохраняет текущий view, bootstrap/join-ит `tenant_id` проекта и восстанавливает последнюю session этого tenant; session не переносится между tenants.
- [ ] Нет изменений в `internal/kernel` и `tools/tabula-distro`, связанных с project semantics.
- [ ] Distro без projects bundle сохраняет существующий single-workspace/single-context UX.
- [ ] Есть unit tests для registry/locking/validation и frontend/API tests для selector и tenant switch.
- [ ] Есть установленная проверка, демонстрирующая выбор проекта и выполнение в tenant-specific workspace.
- [ ] Обновлены ADR/документация и установленный `tabula-guide` с описанием optional projects capability, registry layout и project/tenant/session границ.

## Blocked by

None - can start immediately

## Explicit non-goals

- Telegram integration.
- Tenant creation, deletion, or materialization.
- `tabula-distro project` commands.
- Kernel project entities or protocol changes.
- `agent_profile` and profile inheritance.
- Changes to `exec.allowed_cwds`.
