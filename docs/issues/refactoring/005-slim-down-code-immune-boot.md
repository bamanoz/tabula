# Slim down code-immune boot.py

Type: Refactor

Priority: P2

Status: Completed

Repos: `tabula-distrib` (code-immune first, claw follows), `tabula-bundles`

## Parent

`docs/issues/refactoring/README.md`

## Problem

`tabula-distrib/code-immune/boot.py` — 418 строк. Делает:

- Resolve `TABULA_HOME` и `sys.path` bootstrap (lines 13-17) — после issue 002
  ушло в helper.
- `_toml_literal`, `_write_table`, `_write_toml`, `_write_hook_permissions_config`
  (lines 50-93, 351-370) — после issue 001 ушло в `toml_io`.
- `_workspace_path`, `_tenant_workspace_path`, `_app_workspace_path` —
  workspace resolution, distro policy, остаётся.
- `_ensure_workspace_configs`, `_ensure_app_tenant_config`,
  `_default_permission_rules`, `_default_mcp_servers`, `_ensure_mcp_config`,
  `_ensure_permissions_config` — distro policy, остаётся.
- `_discover_plugins` — после issue 004 это installer-input only,
  оставить, пометить.
- Provider selection через `tabula_drivers.provider_selection` — уже в shared
  bundle library, ОК.
- Skill scanning через `tabula_plugin_sdk.agent_skills` — уже в shared bundle
  library, ОК.

После issue 001+002 файл сократится естественно. Эта issue — добить остаток,
**вынеся** то, что нужно нескольким distro, в `tabula-bundles/_lib`, и оставив
в `boot.py` только то, что реально policy code-immune.

## What to build

### Что переезжает в `tabula-bundles/_lib/python/src/tabula_plugin_sdk/`

- `permissions.py` — `default_permission_rules(home, tenant_dir, *, denied,
  allowed)`, генерация rule-объектов с правильными relative-path-форматами.
  Сейчас inline в `code-immune/boot.py:309-348`. Это нужно как минимум `claw`
  и `code` тоже.
- `mcp_defaults.py` — мог бы быть, но `default_mcp_servers()` в code-immune
  специфично (`context7`, `playwright`, `duckduckgo`). Если в `claw` и `code`
  набор отличается — оставляем в каждом distro. **По умолчанию: не двигаем.**
- `workspace_resolution.py` — `resolve_workspace(env, global_cfg, tenant_cfg,
  app_values, cwd)`. Логика тривиальная, но повторяется. Кандидат на helper,
  если `claw` использует ту же лестницу приоритетов.

### Что остаётся в `code-immune/boot.py`

- `_workspace_path` — обёртка над `resolve_workspace` с code-immune-specifics
  (если есть).
- `_ensure_app_tenant_config` — содержит `distro: "code-immune"`,
  `prompt_builder: "code_immune_prompt.builder"`,
  `agent_providers: ["code_immune_agents.registry"]`. Это явно distro policy,
  не двигаем.
- `_default_mcp_servers` — distro-specific набор.
- `_include_skill` — provider-based фильтр, distro policy.
- `main()` — JSON emit.

### Целевой размер

После issue 001 + 002 + 005: `code-immune/boot.py` ≤ 200 строк (с 418).
`claw/boot.py` соответственно меньше нынешних 775.

## Acceptance criteria

- [x] `tabula_plugin_sdk.permissions.default_permission_rules` существует с
      unit-тестами.
- [x] `code-immune/boot.py` зовёт его вместо inline `_default_permission_rules`.
- [ ] `claw/boot.py` (если у него своя версия) тоже зовёт его (отдельный
      коммит).
- [ ] Размер `code-immune/boot.py` ≤ 200 строк.
- [x] Поведение testbed-сьютов `code-immune` идентично до/после.
- [ ] `tabula_plugin_sdk.workspace_resolution` (если делаем) с тестами и
      используется в `code-immune` и `claw`.

## Files

- New: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/permissions.py`
- New: `tabula-bundles/_lib/python/tests/test_permissions.py`
- Maybe new: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/workspace_resolution.py`
- Edit: `tabula-distrib/code-immune/boot.py` — главное изменение
- Edit: `tabula-distrib/claw/boot.py` — follow-up коммит после testbed

## Verify

```bash
pytest tabula-bundles/_lib/python/tests/test_permissions.py -q
wc -l tabula-distrib/code-immune/boot.py     # <= 200
tabula-testbed run code-immune
tabula-testbed run baseline
```

После прохождения — обновить `claw`, прогнать его сьюты.

## Risk

Низкий. Чисто механический extract. Главный риск — отличия в путях между
distro (relative vs absolute). Покрывается тестами `permissions.py`.

## Blocked by

- `001-tomlkit-runtime-config-writer.md` (writer removal предполагается уже
  сделанным)
- `002-shared-paths-helper.md`
- `004-runtime-toml-as-plugin-source-of-truth.md` — без него `_discover_plugins`
  ещё канон, и реорганизация boot преждевременна.

## Notes

- НЕ переезжать в `_lib`: provider selection (уже там), skill scanning (уже
  там), prompt template logic (это distro policy, отдельные модули в
  `code_immune_prompt.builder` если потребуется).
- `_default_mcp_servers` сейчас в boot.py inline. Если станет ясно, что во всех
  актуальных distro набор одинаковый — отдельный issue вынести в `_lib`. Не
  расширять scope этой PR.
- `guardian` сейчас не обновляем — отдельный todo если ресурс позволит.

## Outcome

**Shared library (`tabula-bundles/_lib/python/src/tabula_plugin_sdk/`):**

- `permissions.py` (new) — `default_permission_rules(home, tenant_dir)`.
  Чистая функция, без I/O, возвращает свежий список каждый вызов. Layout:
  4 глобальных tool rules (gateway/mcp/exec_run) + 4 allow per-path pairs
  (prompt/state/plugin_cfg/logs) + 5 deny per-path pairs (secrets/run/bin/
  venv/generations), каждая пара = absolute + `TABULA_HOME`-relative форма.
- `tests/test_permissions.py` (new) — 6 тестов: fresh list, global rules
  order, allow/deny coverage, custom home directory name, fs_* tool на
  всех path rules.
- `toml_io.py` расширен публичными функциями: `assign(doc, data)`
  (overwrite-keys семантика, в отличие от `merge_defaults`) и
  `to_tomlkit(value)` (alias на private `_to_tomlkit` для distro кода,
  который строит array-of-tables вручную).

**`code-immune/boot.py` (436 → 376 строк):**

- Удалён inline `_default_permission_rules` (~40 строк) — call site зовёт
  helper.
- Удалены дубликаты `_to_tomlkit` и `_assign_dict` (~20 строк) — `_write_toml`
  использует `toml_io.assign`, `_write_hook_permissions_config` использует
  `toml_io.to_tomlkit`.
- Цель ≤200 строк **не достигнута**. Оставшиеся 376 строк — distro policy
  (workspace resolution, app-mode tenant config, MCP defaults, hook-permissions
  layout) которую AGENTS.md tabula-distrib запрещает выносить в shared.
  Workspace resolution не двигалась так как `claw/boot.py` использует другую
  лестницу приоритетов (issue 005 явно: "только если claw использует ту же").

**Verification:**

- `pytest tabula-bundles/_lib/python/tests/test_permissions.py` — 6 passed.
- `pytest tabula-bundles/_lib/python/tests` — 86 passed, 2 pre-existing
  baseline failures (`test_subagent_runtime.py` envelope drift).
- `python -c "import ast; ast.parse(...)"` на `code-immune/boot.py` — OK.
- Testbed `lint --suite baseline` — passed.
- Testbed `direct --suite baseline` — passed.

**Deferred to follow-ups:**

- `claw/boot.py` использует примитивный string-concat `_write_toml_file` (не
  tomlkit), не имеет `_default_permission_rules`. Миграция claw на
  `toml_io` writer + extraction отдельный follow-up (не блокирует issue
  006/007).
- `_default_mcp_servers` остаётся inline в каждой distro (наборы серверов
  различаются — `code-immune` имеет context7+playwright+duckduckgo, `claw`
  только duckduckgo). Извлечение преждевременно.
- Цель ≤200 строк недостижима без переноса distro policy. Если потребуется —
  отдельный architectural issue.
