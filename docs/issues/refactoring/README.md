# Config & Boot Refactoring Backlog

Тематический бэклог: распутать логику конфигурации и старта ядра Tabula. Источник
анализа: сравнение с Neovim (stdpath / runtimepath / health / trust) и аудит
текущей реализации (`internal/tabula/app.go`, `tabula-distro/install.py`,
`tabula-distrib/*/boot.py`, `tabula_plugin_sdk.config`).

Цель серии — один центр конфигурации (`runtime.toml`), один контракт boot
(behavior, не layout), общие helpers для путей и `.env`, и явный trust для
исполняемого distro boot. По итогу — Neovim-уровень introspection (`tabula
config inspect`, `tabula health`).

## Ground rules

- **Primary distro** для разработки и проверки — `code-immune`. Сначала меняем
  его, гоняем testbed, потом переносим на `claw`. `guardian` сейчас неактуален
  и, вероятно, сломан — он не блокирует серию, но если правка тривиально на него
  ложится — обновляем.
- **AGENTS.md границы:**
  - Generic kernel mechanics → `tabula/internal/`, `tabula/cmd/`.
  - Reusable Python runtime helpers → `tabula-bundles/_lib/python/src/`.
  - Distro product policy (boot, prompt, provider, workspace) →
    `tabula-distrib/<distro>/`.
- **Без legacy shims.** Переименовали — удалили старый surface в той же PR.
- **Не коммитить без явной просьбы.**

## Priority Order

1. `001-tomlkit-runtime-config-writer.md` — **Completed**
2. `002-shared-paths-helper.md` — **Completed**
3. `003-single-dotenv-loader.md` — **Completed**
4. `004-runtime-toml-as-plugin-source-of-truth.md` — **Completed**
5. `005-slim-down-code-immune-boot.md` — **Completed**
6. `006-tabula-config-inspect-and-health.md` — **Skipped** (P2 feature, deferred)
7. `007-distro-boot-trust-db.md` — **Completed**

## Dependency Map

| Issue | Blocked by | Notes |
|---|---|---|
| 001 | None | Чисто writer, риск низкий. |
| 002 | None | Может идти параллельно с 001. |
| 003 | 002 | Нужен общий paths helper, чтобы env loader был в одном месте. |
| 004 | 001, 002 | Меняет публичный контракт `BootConfig`; самый рискованный шаг. |
| 005 | 004 | Без 004 нет смысла резать boot — он всё ещё канон для plugins. |
| 006 | 004 | `inspect` имеет смысл, когда `runtime.toml` — канон. |
| 007 | 002 | Использует общий `state_dir()` для trust DB. |

## Rollout strategy

- Каждый issue = одна PR. После каждой PR — `tabula-testbed run baseline` плюс
  фокус-сьюты, упомянутые в issue.
- Все изменения сначала проверяются на `code-immune`. После прохождения
  testbed — отдельный коммит/PR обновляет `claw`.
- `guardian` обновляем только если правка тривиальна; иначе помечаем как
  out-of-scope в PR description.

## Out of scope

- Раздел `$TABULA_HOME` по XDG (`config/state/data/cache`). Это сознательное
  отличие Tabula от Neovim, см. `AGENTS.md`.
- Замена polling `run/reload.touch` на inotify/fsevents. Текущий механизм
  cross-platform и работает.
- Слияние `TABULA_BOOT` и `TABULA_BOOT_PATH` — это публичный env-контракт, ждёт
  major version.
