# tomlkit-based runtime.toml writer

Type: Refactor

Priority: P1

Status: Completed

Repos: `tabula` (testbed template), `tabula-distrib` (`code-immune` first, then
`claw`)

## Parent

`docs/issues/refactoring/README.md`

## Problem

`$TABULA_HOME/config/runtime.toml` сейчас пишется ручной конкатенацией строк
минимум в трёх местах:

- `tools/tabula-distro/src/tabula_distro/runtime_config.py:42-61`
- `tools/tabula-distro/src/tabula_distro/app_run.py:268-294`
- `tabula-distrib/code-immune/boot.py:50-93` (`_toml_literal`, `_write_table`,
  `_write_toml` — самописный мини-encoder)
- аналог в `tabula-distrib/claw/boot.py`

Последствия:

- Пользовательские правки в `runtime.toml` (комментарии, порядок ключей)
  перетираются при `tabula-install`/reinstall.
- Каждый writer переизобретает escape: булевы, списки, словари, multiline-strings
  обрабатываются по-разному. Уже была пара тонких багов с inline-таблицами.
- Нельзя сделать merge "default + user" — только overwrite.

## What to build

Один writer на базе `tomlkit` (сохраняет комментарии, порядок и whitespace),
живущий в shared bundle library, плюс удаление всех самописных encoder-ов.

### Где живёт writer

`tabula-bundles/_lib/python/src/tabula_plugin_sdk/toml_io.py`:

- `load(path: Path) -> tomlkit.TOMLDocument` — пустой документ если файла нет.
- `dump(path: Path, doc: tomlkit.TOMLDocument) -> None` — atomic write через
  temp + rename.
- `merge_defaults(doc, defaults: Mapping) -> tomlkit.TOMLDocument` — добавляет
  ключи, которых нет; не трогает существующие; добавляет комментарии-заголовки
  для новых секций.

Это **reusable Python runtime helper, нужный нескольким distro и инсталлеру** —
по `AGENTS.md` (`tabula/AGENTS.md` § Config And Runtime, `tabula-bundles` rules)
это явно место для `_lib`.

### Где writer используется

- `tools/tabula-distro/src/tabula_distro/runtime_config.py` — заменить
  string-сборку на `merge_defaults` + `dump`.
- `tools/tabula-distro/src/tabula_distro/app_run.py:268-294` — звать общую
  функцию.
- `tabula-distrib/code-immune/boot.py` — удалить `_toml_literal`,
  `_write_table`, `_write_toml`, `_write_hook_permissions_config`. Все вызовы
  идут через `tabula_plugin_sdk.toml_io`.
- `tabula-distrib/claw/boot.py` — аналогично (отдельным коммитом в той же PR,
  после того как `code-immune` прошёл testbed).
- `tabula-distrib/guardian/boot.py`, `code/boot.py`, `testbed/boot.py` — если
  тривиально, иначе скипнуть с пометкой в PR description.

## Acceptance criteria

- [x] `tomlkit` добавлен в зависимости `tabula-bundles/_lib/python` и
      `tools/tabula-distro` (`pyproject.toml`).
- [x] Module `tabula_plugin_sdk.toml_io` с функциями `load`, `dump`,
      `merge_defaults` и unit-тестами (10/10 проходят).
- [x] String-writer-ы удалены: `runtime_config.py`, `app_run.py`,
      `code-immune/boot.py:_toml_literal`/`_write_table`/`_write_toml`
      (`_write_toml`/`_write_hook_permissions_config` остаются как тонкие
      обёртки над `toml_io`, чтобы остальной distro-policy код в boot.py не
      менялся в одной PR).
- [x] Reinstall на dirty `runtime.toml` (с комментарием в начале файла)
      сохраняет комментарий. Покрыто `test_runtime_config.py
      ::test_rewrite_preserves_user_comments_and_unknown_sections`.
- [x] Atomic write: прерывание после `dump` не оставляет полу-записанный TOML.
      Покрыто `test_toml_io.py::test_dump_is_atomic_on_crash`.

## Outcome

- New: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/toml_io.py` —
  `load`/`dump`/`merge_defaults` поверх `tomlkit`, atomic write через
  temp + `os.replace`.
- New: `tabula-bundles/_lib/python/tests/test_toml_io.py` — 10 тестов.
- New: `tabula/tools/tabula-distro/tests/test_runtime_config.py` — 4
  regression-теста: initial write, comment preservation, owned-key update,
  default plugin dir.
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/runtime_config.py` —
  `write()` теперь load → assign owned keys → `merge_defaults` для
  `[pool]` → `dump`. Пользовательские комментарии и неизвестные секции
  сохраняются.
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/app_run.py` —
  `write_runtime_config()` использует тот же writer, AoT для tenants и
  kernel через `tomlkit.aot()`.
- Edit: `tabula-distrib/code-immune/boot.py` — `_toml_literal`,
  `_write_table` удалены. `_write_toml` и `_write_hook_permissions_config`
  стали тонкими обёртками над `toml_io.load`/`dump`. Comment preservation
  работает для всех 5+ config-файлов, которые boot пишет.
- Edit: `pyproject.toml` в `tabula-bundles/_lib/python` и
  `tabula/tools/tabula-distro` — добавлена зависимость `tomlkit>=0.12`.

Pre-existing failures (2 в `_lib`, 2 в `tabula-distro`) подтверждены как
unrelated baseline через `git stash` бэйслайн-сравнение.

Verification:

```bash
cd tabula-bundles/_lib/python
PYTHONPATH=src .../python3 -m unittest tests.test_toml_io -v
# 10/10 OK

cd tabula
PYTHONPATH=tools/tabula-distro/src:../tabula-bundles/_lib/python/src \
  .../python3 -m unittest tools.tabula-distro.tests.test_runtime_config -v
# 4/4 OK

PYTHONPATH=... .../python3 -m unittest tools.tabula-distro.tests.test_app_run -v
# 15/15 OK

go build ./...
go test ./internal/runtime/host/config/... -count=1
# ok
```

## Follow-ups (not part of this PR)

- `code-immune/boot.py` всё ещё содержит обёртки `_write_toml` и
  `_write_hook_permissions_config` поверх `toml_io`. Их можно убрать в
  issue 005 (slim down boot), когда `_ensure_*_config` функции будут
  переезжать или упрощаться. Не делаем сейчас, чтобы не растягивать PR.
- `claw/boot.py` — отдельным коммитом после testbed на code-immune. Помечен
  в README.md серии. Можно сделать сразу же мини-PR, копия change для
  `claw`.

## Files / surfaces

- New: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/toml_io.py`
- New: `tabula-bundles/_lib/python/tests/test_toml_io.py`
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/runtime_config.py`
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/app_run.py` (~268-294)
- Edit: `tabula/tools/tabula-distro/pyproject.toml`
- Edit: `tabula-bundles/_lib/python/pyproject.toml`
- Edit: `tabula-distrib/code-immune/boot.py` (drop self-rolled encoder)
- Edit: `tabula-distrib/claw/boot.py` (после прохождения testbed на code-immune)
- Edit (testbed): `tabula/tools/tabula-testbed/.../testbed_template/...` — если
  template содержит копию writer-а, синхронизировать.

## Verify

```bash
# unit
pytest tabula-bundles/_lib/python/tests/test_toml_io.py -q

# distro install preserves comments
tabula-testbed run baseline
tabula-testbed run config-preservation   # новый сьют, см. ниже

# go side untouched
go test ./... -race -count=1
```

Новый testbed-сьют `config-preservation`:

1. `tabula-install code-immune`.
2. Дописать в `$TABULA_HOME/config/runtime.toml` строку `# user comment` сверху.
3. `tabula-install code-immune --update`.
4. Проверить, что `# user comment` всё ещё первая строка файла.

## Risk

Низкий. Изменение чисто writer-side, формат файла не меняется. Главный риск —
`tomlkit` иначе сериализует inline-таблицы. Закрывается round-trip тестом в
unit-сьюте.

## Out of scope

- Изменения схемы `runtime.toml` (это issue 004).
- Удаление двойного дискавери плагинов (это issue 004).

## Notes for implementer

- `tomlkit` >= 0.12 для нормальной поддержки `aot` (array-of-tables).
- Atomic write: писать в `runtime.toml.tmp`, fsync, `os.replace`.
- `merge_defaults` должен быть deterministic — порядок ключей в defaults
  сохраняется в результате.
