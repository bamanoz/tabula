# tabula config inspect & tabula health

Type: Feature (Neovim parity)

Priority: P2

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/issues/refactoring/README.md`

## Problem

В Neovim есть два рабочих лошади для отладки конфигурации:

- `:checkhealth` — пробегает по runtimepath, зовёт `M.check()` модулей,
  собирает structured report.
- `:lua print(vim.inspect(...))` + `nvim --headless +':checkhealth' +qa` —
  static introspection без запуска полноценной сессии.

В Tabula сейчас нет аналога. Чтобы узнать "какие плагины активны, какие
config-merge применился, какой провайдер выбран" — приходится читать boot
JSON руками или гонять gateway.

После issue 004 (`runtime.toml` — канон) static introspection становится
тривиальным.

## What to build

### `tabula config inspect`

Новая команда в `tabula/cmd/tabula/`. Печатает:

- Resolved paths (`TABULA_HOME`, `config_dir`, `state_dir`, etc — через
  `internal/runtime/paths` из issue 002).
- Active kernel (из `runtime.toml`).
- Active tenant(s) и их overlay paths.
- Plugin layout: каждая запись `plugin_dirs` → manifest path → plugin id →
  enabled/disabled.
- Source of truth: `runtime.toml`, no boot execution. Падать с явным
  сообщением, если файла нет.

Флаги:

- `--plugin <id>` — печатает effective merged config для плагина
  (`global.toml` + `plugins/<id>/config.toml` + tenant `defaults.toml` +
  tenant `overrides.toml`, без значений секретов — только маркеры
  `<redacted>`).
- `--format=text|json` — для скриптов.
- `--tenant <id>` — выбор активного tenant overlay (default: первый из
  `runtime.toml`).

### `tabula health`

Аналог `:checkhealth`. Пробегает по `plugin_dirs`, для каждого manifest
ищет опциональный hook (например, `health` тулу через plugin SDK) и
вызывает её.

Структура health report (одна запись на плагин):

```json
{
  "plugin_id": "fs",
  "status": "ok|warn|error",
  "messages": [
    {"level": "info|warn|error", "text": "..."}
  ]
}
```

В SDK добавить опциональный entry point: если плагин экспортирует tool
`health` без аргументов и возвращает structured payload — `tabula health`
его собирает. Если не экспортирует — checker помечает `status: "skipped"`.

### Output

```
tabula config inspect
─── runtime ──────────────────────────
  TABULA_HOME      /Users/mak/.tabula
  config_dir       /Users/mak/.tabula/config
  state_dir        /Users/mak/.tabula/state
  run_dir          /Users/mak/.tabula/run

─── kernel ──────────────────────────
  active           code-immune
  tenant           default

─── plugins (from runtime.toml) ─────
  fs               /Users/mak/.tabula/plugins/fs        enabled
  exec             /Users/mak/.tabula/plugins/exec      enabled
  mcp              /Users/mak/.tabula/plugins/mcp       enabled
  ...

(use `tabula config inspect --plugin fs` for merged config)
```

## Acceptance criteria

- [x] `tabula config inspect` без аргументов работает на чистой code-immune
      установке и печатает все секции.
- [x] `tabula config inspect --plugin fs --format=json` валиден как JSON и
      содержит merged config.
- [x] Значения из `secrets.json` (или плагин-секрет paths) **не** попадают
      в output, заменены на `<redacted>`.
- [x] `tabula health` пробегает по plugin_dirs, для каждого либо зовёт
      health-tool, либо помечает `skipped`. На code-immune минимум один
      плагин экспортирует реальный health (для демо — `fs` или `exec`).
- [x] `tabula config inspect` НЕ запускает distro boot script. Проверка
      через testbed (mock boot, который пишет файл при запуске — после
      `inspect` файла нет).
- [x] Output stable для скриптов: `--format=json` schema задокументирована.

## Files

- New: `tabula/cmd/tabula/config.go` (или подкоманда внутри существующего CLI)
- New: `tabula/cmd/tabula/health.go`
- New: `tabula/internal/inspect/inspect.go` — pure-Go функция, читает
  `runtime.toml` + overlays, собирает merged view.
- New: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/health.py` —
  helper для плагинов: декоратор или конвенция для экспорта health.
- Edit: один-два плагина в `tabula-bundles/` (`fs`, `exec`) — добавить health.
- Edit: `tabula/docs/ARCHITECTURE.md` — раздел Introspection.
- Edit: `tabula/README.md` — добавить CLI команды.
- New tests: `tabula/internal/inspect/inspect_test.go`,
  `tabula/cmd/tabula/config_test.go`

## Verify

```bash
go test ./internal/inspect/... ./cmd/tabula/... -race
tabula config inspect
tabula config inspect --plugin fs --format=json | jq .
tabula health
tabula-testbed run config-inspect
```

## Risk

Низкий. Это чисто read-only фича, не меняет конфигурацию. Главный риск —
случайно засветить секреты. Покрывается явным `<redacted>` правилом и
тестом на secrets passthrough.

## Blocked by

- `004-runtime-toml-as-plugin-source-of-truth.md` — без него inspect должен
  был бы запускать boot, что разрушает идею.

## Notes

- Schema JSON output документируется в `tabula/docs/CLI.md` (или
  ARCHITECTURE), с указанием stability ("beta until v1.0").
- `tabula health` НЕ должна гонять полную сессию. Никаких LLM-вызовов,
  никаких subprocess kernel restart. Только plugin-side checks.
- Это та же ниша, что в Neovim занимает `:checkhealth` — диагностика без
  поднятия полного pipeline.
