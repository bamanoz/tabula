# Регистрировать существующий workspace tenant как проект

**Type:** AFK
**Blocked by:** `001-project-selection-web.md`

## What to build

Расширить projects capability безопасной явной регистрацией уже materialized tenant через Web. Регистрация не создаёт tenant, не копирует tenant layout и не вызывает `tabula-distro`.

Web показывает validated tenant candidates, пользователь выбирает tenant, указывает project ID и display name, подтверждает операцию. SDK извлекает `project_root` из canonical tenant workspace config, проверяет tenant и atomically добавляет запись в глобальный registry.

## Acceptance criteria

- [ ] SDK предоставляет операцию `register(project_id, display_name, tenant_id)`; `project_root` не принимается от пользователя и извлекается из tenant config.
- [ ] Candidate API читает только canonical `$TABULA_HOME/tenants/*/tenant.toml`, проверяет существование tenant и извлекает валидный workspace root; автоматической регистрации при discovery нет.
- [ ] Web registration UI показывает только validated candidates и явно подтверждает регистрацию.
- [ ] Регистрация отклоняется, если tenant отсутствует, tenant config некорректен, workspace root отсутствует или не совпадает с canonical tenant config.
- [ ] Registry запрещает duplicate `project_id`, duplicate `tenant_id` и невалидные project IDs.
- [ ] One-to-one invariant `project_id <-> tenant_id` покрыт unit tests и enforced до записи.
- [ ] Registry запись создаётся атомарно под cross-process file lock; конкурентные регистрации не теряются и не создают дубликаты.
- [ ] После успешной регистрации новый проект появляется в Web и может быть выбран без перезапуска gateway.
- [ ] При отмене или ошибке регистрации не изменяются tenant files, sessions, project workspace и plugin config.
- [ ] Web API возвращает безопасную ошибку без раскрытия лишних runtime/config details.
- [ ] Есть unit, frontend и installed-layout tests для candidate listing, register, duplicate rejection и immediate selection.

## Blocked by

- `001-project-selection-web.md`

## Explicit non-goals

- Создание или materialization нового tenant.
- Удаление tenant или workspace.
- Telegram registration.
- Изменения kernel и `tabula-distro`.
- Автоматическое discovery и автоматическая регистрация всех tenants.
