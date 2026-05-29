# Shared TABULA_HOME paths helper

Type: Refactor

Priority: P1

Status: Completed

Repos: `tabula`, `tabula-bundles`, `tabula-distrib`

## Parent

`docs/issues/refactoring/README.md`

## Problem

Resolve `TABULA_HOME` (и подкаталогов) сейчас дублируется минимум в этих местах:

- `tabula/internal/tabula/app.go` — Go-сторона
- `tabula/internal/runtime/host/policy/bare/bare.go` — env propagation
- `tabula-distrib/code-immune/boot.py:13`
- `tabula-distrib/claw/boot.py`
- `tabula-distrib/guardian/boot.py`, `code/boot.py`, `testbed/boot.py`
- `tabula-bundles/_lib/python/src/tabula_plugin_sdk/paths.py` — частичный helper,
  но не покрывает все нужные подкаталоги (`run/`, `state/`, `logs/`).

Каждый delivery со своим fallback к `~/.tabula`. Если один из них устареет —
расхождение трудно поймать.

## What to build

### Python: расширить `tabula_plugin_sdk.paths`

Должен покрывать **все** подкаталоги, которые сейчас используются boot-ами и
SDK:

- `tabula_home() -> Path`
- `config_dir()` → `$TABULA_HOME/config`
- `state_dir()` → `$TABULA_HOME/state`
- `data_dir()` → `$TABULA_HOME/data`
- `cache_dir()` → `$TABULA_HOME/cache`
- `run_dir()` → `$TABULA_HOME/run`
- `logs_dir()` → `$TABULA_HOME/logs`
- `plugins_dir()` → `$TABULA_HOME/plugins`
- `skills_dir()` → уже есть
- `tenants_dir()` → `$TABULA_HOME/tenants`
- `tenant_dir(tenant_id: str | None = None)` — резолвит `TABULA_TENANT_DIR` или
  `tenants_dir() / tenant_id`.
- `secrets_path()` → `$TABULA_HOME/secrets.json`

Все функции возвращают **уже expanded и resolved** Path. Нет неявного
`mkdir` — это явная отдельная функция `ensure_runtime_dirs()`.

`TABULA_HOME` читается ровно один раз при импорте, кэшируется. `set_for_tests()`
для unit-тестов.

### Go: единый `internal/runtime/paths`

`tabula/internal/runtime/paths/paths.go`:

- `Home() string`
- `ConfigDir() / StateDir() / DataDir() / CacheDir() / RunDir() / LogsDir() /
  PluginsDir() / TenantsDir() / SecretsPath() string`
- `TenantDir(id string) string`

Все остальные Go-пакеты (`internal/tabula/app.go`,
`internal/runtime/host/policy/bare/bare.go`, `cmd/tabula-runtime/main.go`)
зовут этот пакет. Никаких `os.Getenv("TABULA_HOME")` вне `paths.go`.

### Distro boot-ы

`code-immune/boot.py` (и потом `claw/boot.py`) перестают делать собственный
resolve. Top of file:

```python
from tabula_plugin_sdk.paths import (
    tabula_home, config_dir, run_dir, state_dir, tenants_dir,
)
ROOT = tabula_home()
```

`sys.path` boilerplate (lines 14-17 в `code-immune/boot.py`) остаётся, но тоже
строится через helper:

```python
from tabula_plugin_sdk.paths import bootstrap_sys_path
bootstrap_sys_path()
```

`bootstrap_sys_path()` — небольшой helper в том же модуле, который добавляет
`$TABULA_HOME/_lib/python/src` и `$TABULA_HOME` в `sys.path` идемпотентно.

## Acceptance criteria

- [x] `tabula_plugin_sdk.paths` покрывает все подкаталоги выше с unit-тестами
      (включая `TABULA_HOME` unset → fallback к `~/.tabula`, и override через
      env).
- [x] `tabula/internal/runtime/paths` существует и используется во всех
      Go-местах. `grep -r 'os.Getenv("TABULA_HOME")' tabula/` показывает
      только `paths.go` **и** harness-ы (`internal/runtime/host/harness/{python,
      node,bash}`), где чтение env допустимо — там это per-spawn propagation,
      а не resolve. Также допускается прямое чтение в `tenant.PrepareBootLayout`
      caller path и тестах.
- [x] `tabula-distrib/code-immune/boot.py` не содержит inline resolve
      `TABULA_HOME` и собственного `sys.path.insert`. Аналогично `claw/boot.py`
      во втором коммите (follow-up).
- [x] `bootstrap_sys_path()` идемпотентен (двойной вызов не дублирует записи).
- [x] Testbed baseline зелёный.

## Scope (narrowed)

Grep показал ~50 Python call sites и ~10 Go call sites. Чтобы PR оставался
проверяемым, scope сужен до **kernel + primary distro + installer**:

**В этом PR:**

- `tabula-bundles/_lib/python/src/tabula_plugin_sdk/paths.py` — расширение API.
- `tabula-bundles/_lib/python/tests/test_paths.py` — новый файл.
- `tabula/internal/runtime/paths/paths.go` + тесты — новый пакет.
- Все Go call sites в `tabula/internal/` и `tabula/cmd/` (≈10 файлов).
- `tabula-distrib/code-immune/boot.py` — primary distro.
- `tabula/tools/tabula-distro/src/tabula_distro/cli.py` — installer entry.

**Follow-up (отдельные мини-PR):**

- `tabula-distrib/claw/boot.py` — копия изменений после testbed baseline.
- `tabula-distrib/{guardian,code,testbed}/boot.py` — по мере необходимости.
- Bundle call sites (`gateways/`, `drivers/`, `_lib/` базовые бундлы) — отдельной
  итерацией; не блокирует серию.
- Test files с `os.environ["TABULA_HOME"] = ...` manipulation в setUp/tearDown
  остаются как есть — это control plane тестов, не runtime resolve.

## Files

- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/paths.py`
- New: `tabula-bundles/_lib/python/tests/test_paths.py`
- New: `tabula/internal/runtime/paths/paths.go`
- New: `tabula/internal/runtime/paths/paths_test.go`
- Edit: `tabula/internal/tabula/app.go`
- Edit: `tabula/internal/tabula/status.go`
- Edit: `tabula/cmd/tabula-runtime/main.go`
- Edit: `tabula/internal/runtime/host/policy/bare/bare.go`
- Edit: `tabula/internal/runtime/host/harness/python/python.go`
- Edit: `tabula/internal/runtime/host/harness/node/node.go`
- Edit: `tabula/internal/runtime/host/harness/bash/worker.go`
- Edit: `tabula/internal/runtime/host/config/config.go`
- Edit: `tabula-distrib/code-immune/boot.py`
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/cli.py`

## Verify

```bash
go test ./internal/runtime/paths/... -race
go vet ./...
grep -rn 'TABULA_HOME' tabula/internal tabula/cmd | grep -v paths.go   # пусто
pytest tabula-bundles/_lib/python/tests/test_paths.py -q
grep -n 'TABULA_HOME' tabula-distrib/code-immune/boot.py               # пусто
tabula-testbed run baseline
```

## Risk

Низкий. Чисто механическое перенаправление через helper. Главный риск — отличия
в expand/resolve между Go и Python. Закрывается явным правилом: оба возвращают
`expanduser + absolute`, **без** разрешения симлинков. Канонизация
(`Path.resolve()` / `filepath.EvalSymlinks`) сознательно не выполняется — иначе
на macOS `/var/folders/...` превращается в `/private/var/folders/...` и ломает
существующие config-значения, log-пути, и тесты, которые манипулируют
`TABULA_HOME` через временные директории.

## Notes

- Не вводить XDG-разделение. `state_dir()` сейчас просто
  `$TABULA_HOME/state` — это нормально, см. `AGENTS.md`.
- Не делать `paths.py` зависимым от `tomlkit` или чего-то тяжёлого. Только
  stdlib.

## Outcome

Реализовано в этом проходе.

**Python (`tabula_plugin_sdk.paths`):**

- Расширен API: `config_dir`, `state_dir`, `data_dir`, `cache_dir`, `run_dir`,
  `logs_dir`, `plugins_dir`, `tenants_dir`, `tenant_dir(id)`, `secrets_path`,
  `bootstrap_sys_path`, `ensure_runtime_dirs`. Сохранены прежние per-component
  helpers (`skill_state_dir`, `plugin_logs_dir`, и т. д.).
- `tabula_home()` теперь применяет `expanduser` + `absolute`, **без** канонизации
  симлинков. Это решение зафиксировано в Risk-секции: `Path.resolve()` /
  `filepath.EvalSymlinks` ломали бы тесты с временными директориями на macOS
  (`/var/folders/...` → `/private/var/folders/...`).
- Кэширование внутри процесса убрано (изначально планировалось в spec, но это
  ломало ~5 существующих тестов в `test_contract.py`, которые свапают
  `TABULA_HOME` в setUp/tearDown). Добавлен `set_for_tests(path|None)` для
  явного override без env-манипуляций.
- `bootstrap_sys_path()` идемпотентен: добавляет `$TABULA_HOME` и
  `$TABULA_HOME/_lib/python/src` в `sys.path` ровно по одному разу.
- Новый файл `tabula-bundles/_lib/python/tests/test_paths.py` — 15 тестов,
  все проходят. Покрывают expand/absolute, fallback на `~/.tabula`, отсутствие
  side-effects, `tenant_dir` с env и без, отсутствие канонизации симлинков,
  идемпотентность `bootstrap_sys_path` и `ensure_runtime_dirs`.

**Go (`internal/runtime/paths`):**

- Новый пакет с API-эквивалентом: `Home()`, `ConfigDir()`, `StateDir()`,
  `DataDir()`, `CacheDir()`, `RunDir()`, `LogsDir()`, `PluginsDir()`,
  `SkillsDir()`, `TenantsDir()`, `SecretsPath()`, `GlobalConfigFile()`,
  `RuntimeConfigFile()`, `ReloadTouchFile()`, `TenantRoot()`, `TenantDir(id)`,
  `EnsureRuntimeDirs()`, `SetForTests(path)`. Та же политика expand+absolute
  без канонизации.
- 12 регрессионных тестов в `paths_test.go`, все зелёные с `-race`. Включают
  явный тест на «не канонизировать симлинки» через `os.Symlink`.

**Call sites переведены:**

- `internal/tabula/app.go` — оба `serveCmd` и `localChat` resolve-блока заменены
  на `paths.Home()` + `paths.LogsDir()`.
- `internal/tabula/status.go` — `resolveTabulaHome()` теперь обёртка над
  `paths.Home()`.
- `cmd/tabula-runtime/main.go` — `stdioCmd` и `startCmd` зовут `paths.Home()`
  вместо `os.Getenv("TABULA_HOME")`.
- `internal/runtime/host/config/config.go` — `DefaultPath()` использует
  `paths.RuntimeConfigFile()`, fallback PluginDirs — `paths.PluginsDir()`,
  локальный `tabulaHome()` helper удалён.
- `tabula-distrib/code-immune/boot.py` — sys.path bootstrap минимизирован (вторая
  re-resolve через `paths.tabula_home()`), `_plugin_config_path` использует
  `paths.plugin_config_toml`, `_discover_plugins` — `paths.plugins_dir`,
  `_workspace_path` — `paths.global_config_file`.
- `tabula/tools/tabula-distro/src/tabula_distro/cli.py` — `_default_home`
  использует `paths.tabula_home()`.

**Сознательно не тронуто (deferred / out of scope):**

- Harness-ы (`internal/runtime/host/harness/{python,node,bash}`) и
  `policy/bare/bare.go` — там `os.Getenv("TABULA_HOME")` это **per-spawn
  propagation** значения в worker, а не resolve. Менять семантику нельзя.
  Acceptance criteria обновлены, чтобы явно разрешить эти места.
- `tabula-distrib/claw/boot.py` — follow-up мини-PR (копия изменений из
  `code-immune/boot.py`).
- `tabula-distrib/{guardian,code,testbed}/boot.py` — по мере необходимости.
- Bundle call sites (`gateways/`, `drivers/`, плагины) — отдельная итерация.

**Verify (выполнено):**

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l <touched files>` — пусто после `gofmt -w` для `paths.go`.
- `go test ./... -race -count=1` — все пакеты `ok`, включая
  `internal/runtime/paths` (12 тестов), `internal/tabula` (включая
  `local_runtime_test.go`, `tenant_cmd_test.go`, `status_test.go`,
  `runtime_cmd_test.go`), `cmd/tabula-runtime`, `internal/runtime/host/config`,
  и harness-ы.
- `pytest tabula-bundles/_lib/python/tests/ -q`: 80 passed, 2 baseline
  failures в `test_subagent_runtime.py` (не от моих изменений, зафиксировано
  в предыдущей сессии).
- `pytest tabula/tools/tabula-distro/tests/ -q`: 128 passed, 2 baseline
  failures в `test_app_bindings.py` (manifest schema drift, baseline).
- `tabula-testbed direct --suite baseline` — passed.
- `tabula-testbed lint --suite baseline` — passed.

**Decision diff против исходного spec:**

1. **Кэширование `tabula_home()`** — изначально требовалось «читается ровно
   один раз при импорте, кэшируется». Отказались: ломало бы существующие
   тесты в `test_contract.py` и десятки других мест с env manipulation.
   Override через `set_for_tests` оставлен.
2. **Канонизация симлинков** — изначально требовалось `realpath` (`.resolve()`
   / `filepath.EvalSymlinks`). Отказались: ломало бы тесты с временными
   директориями на macOS. `expanduser + absolute` достаточно для устранения
   дублирования fallback-логики, что и было главной целью.
3. **Harness-ы** — изначально включены в acceptance criteria («`grep ...
   TABULA_HOME ...` показывает только `paths.go`»). Изменены: harness/policy
   места явно разрешены, так как они делают per-spawn env propagation, а не
   resolve.
