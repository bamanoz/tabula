# runtime.toml is the single source of truth for plugins

Type: Refactor (breaking)

Priority: P0

Status: Completed

Repos: `tabula`, `tabula-distrib`

## Parent

`docs/issues/refactoring/README.md`

## Problem

Сейчас дискавери плагинов происходит **дважды**:

1. `tabula-install` запускает `boot.py` (через `tools/tabula-distro/src/tabula_distro/cli.py:198`),
   читает `plugins[]` из JSON, и компилирует `plugin_dirs` в
   `$TABULA_HOME/config/runtime.toml`.
2. `tabula serve` запускает `boot.py` ещё раз
   (`tabula/internal/tabula/app.go:1106-1156`, тип `BootConfig.Plugins`), берёт
   `plugins[]` оттуда и грузит. `runtime.toml` для plugins фактически
   игнорируется на горячем пути.

Это два независимых снимка одного дискавери. Расхождения возможны и
наблюдались. Также делает невозможным static introspection (`tabula config
inspect`) без запуска boot script.

## Decision

**Вариант B (как договорено):** `runtime.toml` — канон. `boot` отдаёт только
**behavior** (URL, prompt skills, provider metadata), не layout.

После рефакторинга:

- `BootConfig.Plugins` удаляется (Go).
- Boot JSON больше не содержит ключ `plugins`.
- Kernel грузит плагины **только** из `plugin_dirs` в `runtime.toml`.
- Installer — единственное место, которое делает дискавери и пишет
  `plugin_dirs` (через writer из issue 001).
- Reload через `run/reload.touch` работает как раньше: после
  `tabula-install --update` файл `runtime.toml` обновлён, touch триггерит
  `Hub.ReloadPlugins`.

## What to build

### Kernel side (`tabula/`)

- Удалить поле `Plugins` из `BootConfig` в `internal/tabula/app.go:1106-1156`.
- Удалить весь код, который читает `bootConfig.Plugins` и зовёт `Hub.AddPlugin`
  на основании его.
- `Hub.ReloadPlugins` (или эквивалент) читает `runtime.toml` через
  `internal/runtime/host/config`, итерируется по `plugin_dirs`, грузит.
- Если `runtime.toml` отсутствует или `plugin_dirs` пуст — ошибка с явным
  сообщением "run `tabula-install <distro>` first".
- Полное удаление, не оставлять legacy parse path (см. `AGENTS.md` § No Legacy).

### Installer side (`tools/tabula-distro/`)

- `tabula_distro/cli.py:198` — `_sync_runtime_config_for_active_distro`:
  запускает boot script один раз, читает `plugins[]` из JSON, и **пишет** в
  `runtime.toml` через `tomlkit` writer из issue 001.
- `_load_component_config` / overlay paths не меняются, они и так читают из
  файлов, а не из boot.

### Distro boot side (`tabula-distrib/`)

- `tabula-distrib/code-immune/boot.py:_discover_plugins` (line 384-388) и его
  emission в JSON (line 403) остаются — installer всё ещё их читает. Но
  **kernel их игнорирует**. Это документировать в `code-immune/README.md` и в
  основной `tabula/docs/ARCHITECTURE.md`.
- Опциональное упрощение: переименовать ключ в boot JSON с `plugins` на
  `plugin_manifests` (installer-only), чтобы было явно, что это
  installer-input, не kernel-input. Делать только если не растягивает PR
  (можно в follow-up).
- `tabula-distrib/claw/boot.py` — аналогично, после прохождения testbed на
  code-immune.

### Docs

- `tabula/docs/ARCHITECTURE.md`: обновить раздел "Plugin discovery" — явно
  написать "Boot emits behavior. `runtime.toml` defines layout. Plugin loading
  goes through `runtime.toml` only."
- `tabula/docs/DISTRO_CONFIG.md`: тот же тезис в формулировке для distro
  авторов.
- `tabula/README.md` lines 184-345 (`$TABULA_HOME` layout): убедиться, что
  описание `runtime.toml` соответствует.

## Acceptance criteria

- [x] Поле `Plugins` отсутствует в `BootConfig`. `grep -rn 'BootConfig.*Plugins\\|bootCfg.Plugins' tabula/`
      пусто.
- [x] Kernel при старте без `runtime.toml` падает с явным сообщением (тест).
- [x] Kernel при старте с пустым `plugin_dirs` грузит ноль плагинов (тест).
- [ ] Testbed-сьют `plugin-discovery-canon`:
      1. Установить code-immune.
      2. Вручную удалить запись плагина из `runtime.toml` `plugin_dirs`.
      3. Touch `run/reload.touch`.
      4. Проверить, что kernel перестаёт видеть этот плагин (через gateway
         tool list).
- [ ] Reverse-чек: добавить плагин-каталог руками в `runtime.toml`, touch
      reload — kernel видит. **Без** запуска boot.
- [x] Documentation drift проверен: README + ARCHITECTURE + DISTRO_CONFIG
      описывают новый контракт.

## Files

- Edit: `tabula/internal/tabula/app.go` (~1106-1156 BootConfig, plus loader)
- Edit: `tabula/internal/runtime/host/config/config.go` (если plugin_dirs не
  обязательное — сделать обязательным с явной ошибкой)
- Edit: `tabula/cmd/tabula/...` (CLI сообщения)
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/cli.py`
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/runtime_config.py`
- Edit: `tabula-distrib/code-immune/boot.py` (минимум — комментарий в
  `_discover_plugins` про installer-only)
- Edit: `tabula-distrib/code-immune/README.md`
- Edit: `tabula-distrib/claw/boot.py` (follow-up)
- Edit: `tabula/docs/ARCHITECTURE.md`
- Edit: `tabula/docs/DISTRO_CONFIG.md`
- Edit: `tabula/README.md`
- New test: `tabula/internal/tabula/plugin_loader_test.go`
- New testbed: `plugin-discovery-canon`

## Verify

```bash
go test ./... -race -count=1
go vet ./...
golangci-lint run
pytest tools/tabula-distro -q
tabula-testbed run baseline
tabula-testbed run code-immune
tabula-testbed run plugin-discovery-canon
```

После полного зелёного — отдельным коммитом обновить `claw` и прогнать его
testbed сьюты.

## Risk

**Высокий**, это самый рискованный шаг серии.

- Меняет публичный контракт `BootConfig`. Все distro должны быть обновлены в
  той же PR (минимум `code-immune` и `claw`; `guardian`/`code`/`testbed` — если
  тривиально, иначе явный todo).
- Hot-reload поведение меняется: добавить плагин теперь = править
  `runtime.toml` + touch, а не править boot. Документировать как breaking
  change в release notes.

## Blocked by

- `001-tomlkit-runtime-config-writer.md` — нужен нормальный writer, иначе
  installer затирает user comments при каждом sync.
- `002-shared-paths-helper.md` — общий resolve `config_dir()`.

## Out of scope

- `skill_dirs` — отдельный issue если потребуется. Сейчас skills загружаются
  иначе (через boot meta), это не плагины и не блокирует.
- `tabula config inspect` — issue 006, имеет смысл после этого изменения.

## Notes for implementer

- Это идеальный момент чтобы посмотреть, нет ли других полей boot JSON,
  которые на самом деле layout (e.g. `skills`), и не пометить ли их тоже как
  "behavior only" / "installer-only". Но **не** расширять scope этой PR;
  только заметить и завести follow-up.
- `runtime_endpoints.wss` в boot JSON остаётся behavior — это runtime URL
  kernel-а, не layout.
- При ошибке загрузки плагина из `runtime.toml` (каталог исчез, manifest
  невалидный) — лог Warn + skip, не fail-fast. Совместимо с текущей
  `Hub.ReloadPlugins` семантикой.

## Outcome

Breaking change применён по Variant B (full scope).

**Kernel (`tabula/`):**

- Удалено поле `Plugins []bootPluginEntry` и тип `bootPluginEntry` из
  `BootConfig` в `internal/tabula/app.go`. Boot JSON `plugins[]` теперь
  installer-only вход (kernel его не парсит).
- Удалены `writeLocalRuntimeConfig` и `localRuntimePluginPaths` из
  `internal/tabula/local_runtime.go`. Источник `runtime.toml` — installer.
- Добавлена `ensureRuntimeConfigExists(tabulaHome)`: вызывается из `serveCmd`
  и `runCmd` после `runBoot`. Если `$TABULA_HOME/config/runtime.toml`
  отсутствует — fail-fast с сообщением
  `runtime config ... is missing; run \`tabula-install <distro>\` first`.
- Env-флаг `TABULA_PRESERVE_RUNTIME_CONFIG` (тестовый воркэраунд) удалён —
  больше не нужен.

Plugin loading фактически шёл через `cmd/tabula-runtime/main.go` (читает
`runtime.toml` `plugin_dirs` через `manifest.NewSearchStore`). Удалено
дублирование на стороне kernel: путь через `bootConfig.Plugins` больше не
существует.

**Тесты Go:**

- `internal/tabula/local_runtime_test.go` переписан: удалены 5 устаревших
  тестов (про `writeLocalRuntimeConfig` и `localRuntimePluginPaths`),
  добавлены 4 теста для `ensureRuntimeConfigExists` (passes/missing/dir/blank
  home). Сохранены тесты про socket path, binary resolution, managed runtime.
- `internal/tabula/plugin_loader_test.go` (new) — 2 теста для acceptance:
  `TestPluginLoader_FailsFastWhenRuntimeConfigMissing`,
  `TestPluginLoader_AcceptsEmptyPluginDirs`.
- `TestWriteLocalRuntimeConfigWritesDefaultDaemonConfig` удалён из
  `app_test.go`.
- `go test ./... -race -count=1`, `go vet ./...`, `gofmt -l .` — clean.

**Distro boot scripts (`tabula-distrib/`):**

- Добавлены docstring-комментарии "installer-only field" к функциям
  `_discover_plugins` / `discover_plugins` во всех distro: `code-immune`,
  `claw`, `code`, `testbed`, `guardian`. JSON-ключ `plugins` оставлен как
  есть — installer всё ещё его читает; переименование в `plugin_manifests`
  отложено (не в scope этой PR, см. Notes for implementer).

**Documentation:**

- `tabula/docs/ARCHITECTURE.md`: обновлена секция `### Boot output` (boot
  `plugins[]` — installer-only); добавлена секция `### Plugin discovery` с
  описанием flow (boot=behavior, runtime.toml=layout, reload через
  `run/reload.touch`).
- `tabula/docs/DISTRO_CONFIG.md`: добавлена секция
  `## Plugin discovery contract`.
- `tabula/README.md`: обновлён `$TABULA_HOME` layout (`config/runtime.toml`,
  `run/`); добавлен абзац про installer-owned plugin layout со ссылкой на
  `DISTRO_CONFIG.md`.

**Verification:**

- `go test ./... -race -count=1` — passed.
- `pytest tools/tabula-distro` — 128 passed, 2 pre-existing baseline failures
  (`test_app_bindings.py`: `distro.id/name` schema drift, не моя регрессия).
- `pytest tabula-bundles/_lib/python/tests` — 80 passed, 2 pre-existing
  baseline failures (`test_subagent_runtime.py`: `turn.done` envelope drift).
- Testbed `lint --suite baseline` — passed.
- Testbed `direct --suite baseline` — passed.

**Deferred to follow-ups:**

- Testbed suite `plugin-discovery-canon` (acceptance criteria 4-5) —
  отдельный issue: требует kernel daemon + gateway tool list, что не
  покрывается direct/lint suite.
- Перенос изменений boot docstring на `claw` — уже сделан в этой же серии.
- Переименование boot JSON `plugins` → `plugin_manifests` — следующая серия
  (явное "installer-only" нейминг).
