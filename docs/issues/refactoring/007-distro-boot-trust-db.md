# Distro boot trust DB

Type: Security / Feature

Priority: P1

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/issues/refactoring/README.md`

## Problem

`boot.py` distro — это **код**, который kernel запускает unconditionally при
каждом старте `tabula serve`. Если кто-то (или сторонний package) положил
distro в `$TABULA_HOME/distros/<x>/boot.py`, он выполнится в контексте
пользователя без какого-либо явного подтверждения.

В Neovim для аналогичного случая (`exrc`, project-local `init.lua`) есть trust
DB в `$XDG_STATE_HOME/nvim/trust` с SHA содержимого. При несовпадении SHA Neovim
просит явное `:trust`.

Tabula этого не имеет. С учётом того, что boot.py может вызывать произвольные
subprocess-ы (provider selection, npm, uvx) — это реальный attack surface.

## What to build

### Trust DB

`$TABULA_HOME/state/trust.json`:

```json
{
  "distros": {
    "code-immune": {
      "boot_sha256": "abc123...",
      "trusted_at": "2026-05-29T12:34:56Z",
      "trusted_by": "user"
    }
  }
}
```

### CLI

- `tabula distro trust <distro-id>` — печатает SHA текущего `boot.py`,
  спрашивает явное `[y/N]`, при согласии записывает в trust DB.
- `tabula distro trust <distro-id> --yes` — для скриптов / CI.
- `tabula distro untrust <distro-id>` — удалить запись.
- `tabula distro trust --list` — показать все trusted distros и их SHA.

### Kernel behavior

При старте `tabula serve`:

1. Резолвится active distro из `runtime.toml`.
2. Читается `boot.py` (или весь distro directory? см. Open questions ниже).
3. SHA сравнивается с `trust.json`.
4. Если совпадает — boot запускается.
5. Если не совпадает или записи нет — kernel **отказывается стартовать**,
   печатает diff hint:
   ```
   Distro 'code-immune' boot script changed since last trust.
   Expected SHA: abc123...
   Actual SHA:   def456...
   Run `tabula distro trust code-immune` to approve.
   ```

### Installer behavior

`tabula-install code-immune` (свежая установка или update) — после успешной
установки спрашивает: "Trust this distro's boot script? [y/N]". При `--yes`
автоматически записывает в trust DB.

В CI / non-interactive окружении: `tabula-install --trust code-immune` —
явный флаг, без интерактива.

## Acceptance criteria

- [ ] `state/trust.json` создаётся при первой trust-операции.
- [ ] Kernel отказывается стартовать с untrusted distro, exit code != 0,
      понятное сообщение.
- [ ] `tabula distro trust code-immune` записывает корректный SHA, после
      этого `tabula serve` стартует.
- [ ] Изменение `boot.py` (хотя бы whitespace) → kernel снова падает.
- [ ] Установка через `tabula-install --trust` записывает в trust DB
      автоматически.
- [ ] Trust DB не trackит секреты, не содержит абсолютных путей пользователя
      (только distro id и SHA).
- [ ] `tabula distro untrust` удаляет запись.
- [ ] Тесты: trusted → starts, untrusted → fails, modified → fails, --yes
      flow.

## Files

- New: `tabula/internal/runtime/trust/trust.go` — load/save/check SHA.
- New: `tabula/internal/runtime/trust/trust_test.go`
- Edit: `tabula/internal/tabula/app.go` — trust check перед запуском boot.
- New: `tabula/cmd/tabula/distro.go` — `trust` / `untrust` подкоманды.
- Edit: `tabula/tools/tabula-distro/src/tabula_distro/cli.py` — `--trust`
  флаг для install.
- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/paths.py` —
  `trust_path() -> Path` (через `state_dir()`).
- Edit: `tabula/docs/ARCHITECTURE.md` — раздел Security / Trust.
- Edit: `tabula/docs/DISTRO_CONFIG.md`.

## Verify

```bash
go test ./internal/runtime/trust/... -race
tabula-testbed run trust-flow              # новый сьют
tabula-testbed run baseline
```

Новый сьют `trust-flow`:

1. Install code-immune без `--trust`. `tabula serve` падает с trust error.
2. `tabula distro trust code-immune --yes`. `tabula serve` стартует.
3. `echo "" >> $TABULA_HOME/distros/code-immune/boot.py`. `tabula serve` падает.
4. `tabula distro trust code-immune --yes`. `tabula serve` стартует.
5. `tabula distro untrust code-immune`. `tabula serve` падает.

## Risk

Средний.

- UX-риск: пользователь, не понимая, упирается в trust ошибку при первом
  старте. Митигируется явным сообщением и флагом `tabula-install --trust`.
- Migration риск: существующие установки сразу после обновления не имеют
  trust записей и kernel перестаёт стартовать. **Migration window:**
  установщик после обновления автоматически записывает trust для уже
  установленных distro, если `state/trust.json` отсутствует целиком (cold
  start). После этого migration shim удаляется через релиз. Зафиксировать в
  `state/trust.meta.json`: `{"migrated_at": "..."}`.

## Blocked by

- `002-shared-paths-helper.md` — нужен `state_dir()` и общий resolve.

## Open questions

- **Скоуп SHA:** только `boot.py` или весь distro directory? Если distro
  содержит вспомогательные Python-модули рядом (`_lib`, `application/`), их
  тоже стоит trustить. **Предложение:** SHA по всем `*.py` и `distro.toml`,
  рекурсивно, deterministic order. Документировать. Если изменился любой
  файл — re-trust.
- **Distro signing (future):** distro author подписывает release, kernel
  проверяет подпись через известный публичный ключ. Out of scope этой issue,
  но trust DB должна быть способна хранить и pub-key fingerprint в дополнение
  к SHA.
- **Workspace `.tabula/boot.py`** (project-local override, как Neovim
  `exrc`): сейчас Tabula этого не имеет. Если когда-то появится — та же
  trust механика, отдельная запись `workspaces.<path>`.

## Notes

- `state/trust.json` — это **state**, не **config**. Не входит в backup
  пользовательских настроек, кэш-локальный для машины. Документировать.
- Migration shim из секции Risk — единственный допустимый shim в этой серии,
  по `tabula/AGENTS.md` § No Legacy: "Migration shims are allowed only when
  there is a concrete migration requirement". Документировать почему он есть
  и когда удалится.

## Outcome

Реализовано как описано выше.

**Runtime config schema:** `runtime.toml` теперь имеет
`[distro] { active, dir }` table; installer пишет её на каждой
install/reinstall/use операции. Kernel читает оттуда active distro id и
source directory для trust check.

**Hash алгоритм:** SHA256 по recursive `*.py` + top-level `distro.toml`,
sorted relative paths, stream `"<rel>\x00<len>\x00<bytes>\n"`. Excluded:
`__pycache__/`, hidden dirs (`.git`, `.venv`, …), symlinks, non-regular
files, nested `distro.toml`. Markdown/prompts/templates тоже исключены —
они inert от точки зрения code execution.

**Cross-language pin:** Go (`internal/runtime/trust/HashDir`) и Python
(`tabula_distro.trust.hash_dir`) оба pin'ят SHA
`05c8091020bb6dd91bcadd486ab5abe23f2fadce3bcb8d3bb7ec54ebbdfd3498` для
одинакового fixture. Любой drift между сторонами ломает один из двух
тестов до того, как может корраптить пользовательскую trust DB.

**Kernel:** `enforceDistroTrust` в `internal/tabula/local_runtime.go`
зовётся перед `runBoot` в `serveCmd` и `runCmd`. Legacy installs без
`[distro]` table в `runtime.toml` проходят без check (grace period в
migration window). `TABULA_TRUST_SKIP=1` — emergency override.

**CLI:** `tabula distro trust [<id>] [--yes] [--list]` и
`tabula distro untrust <id>`. Без id — берёт активный из `runtime.toml`.
С id, не совпадающим с active — refuse. `--list` печатает sorted JSON.
10 unit-тестов покрывают happy path, refuse-mismatch, prompt abort,
missing TABULA_HOME, no active distro, list empty/populated, untrust
revoke, untrust missing no-op, untrust требует id.

**Installer:** `tabula-install distro install --trust` approve'ит в той
же операции через `trust.approve(..., trusted_by="installer")`. Без
`--trust` — `auto_trust_cold_start` shim: fires only когда
`state/trust.json` отсутствует, drops `state/trust.meta.json` с
`migrated_at`. Hook'нут в `_sync_runtime_config_for_active_distro` —
центральной точке, через которую идут все 4 install/update/reinstall/use
call sites.

**Migration shim (единственный допустимый):** документирован в
ARCHITECTURE.md § Distro trust и DISTRO_CONFIG.md § Trust DB. Удаляется в
будущем релизе после migration window.

**Files changed:**

- New: `internal/runtime/trust/trust.go` + `_test.go` (13 тестов).
- New: `internal/tabula/distro_cmd.go` + `_test.go` (10 тестов).
- New: `tools/tabula-distro/src/tabula_distro/trust.py` + `tests/test_trust.py` (11 тестов, включая pinned cross-language).
- Edit: `internal/runtime/host/config/config.go` — `Distro` struct + validate.
- Edit: `internal/runtime/paths/paths.go` — `TrustFile`, `TrustMetaFile`.
- Edit: `internal/tabula/local_runtime.go` — `enforceDistroTrust`.
- Edit: `internal/tabula/app.go` — call sites + Run switch + usage banner.
- Edit: `tools/tabula-distro/src/tabula_distro/cli.py` — `--trust` flag + auto-trust hook.
- Edit: `tools/tabula-distro/src/tabula_distro/runtime_config.py` — `distro_metadata()` public, `write(..., distro=)`.
- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/paths.py` — `runtime_config_file`, `trust_file`, `trust_meta_file`.
- Edit: `docs/ARCHITECTURE.md` — § Distro trust.
- Edit: `docs/DISTRO_CONFIG.md` — § Trust DB + CLI summary.
- Edit: `README.md` — CLI command table + trust paragraph.

**Open follow-ups (out of scope этой issue):**

- Testbed `trust-flow` suite — заменит integration check'и которые сейчас
  покрыты unit-тестами.
- Distro signing — структура trust DB готова принять pub-key fingerprint
  дополнительно к SHA.
- Workspace `.tabula/boot.py` (project-local override) — если когда-то
  появится, та же механика, отдельная запись в DB.
- Удаление `auto_trust_cold_start` migration shim — после migration
  window (по `state/trust.meta.json` marker).
