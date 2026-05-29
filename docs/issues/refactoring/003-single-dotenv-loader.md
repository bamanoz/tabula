# Single .env loader: kernel is canonical

Type: Refactor

Priority: P2

Status: Completed

Repos: `tabula`, `tabula-distrib`

## Parent

`docs/issues/refactoring/README.md`

## Problem

`.env` грузится дважды:

- Go kernel в `tabula/internal/tabula/app.go:291` (через `godotenv`) при старте
  `tabula serve`.
- Python `tabula-distrib/code-immune/boot.py` (и `claw/boot.py`) при запуске
  subprocess.

Поведение нестабильно, если файл изменился между двумя моментами: kernel и boot
видят разные значения. Хуже того, `os.environ` в boot перетирает то, что kernel
уже выставил.

## What to build

Kernel — **единственный** loader `.env`. Boot полагается на env, который
kernel пробросил через `cmd.Env`.

### Kernel side

- В `tabula/internal/tabula/app.go` сделать загрузку `.env` единственным
  источником. Loader идёт перед всеми subprocess spawn-ами.
- Список путей загрузки документировать (что именно kernel читает, в каком
  порядке): `$TABULA_HOME/.env`, опционально workspace `.env` если есть
  явный override.
- `bare.go` policy уже пробрасывает env в subprocess — убедиться, что
  passthrough покрывает всё, что нужно distro boot-ам (как минимум
  `TABULA_*`, `HOME`, `PATH`, `LANG`, всё что есть в `code-immune` exec
  `env_passthrough`).

### Distro boot side

Удалить любую загрузку `.env` из:

- `tabula-distrib/code-immune/boot.py` (если есть прямой или транзитивный
  вызов `dotenv.load_dotenv` через `tabula_drivers.provider_selection`).
- `tabula-distrib/claw/boot.py`
- `tabula-distrib/code/boot.py`, `testbed/boot.py`
- shared `tabula-bundles/_lib/python/src/tabula_drivers/provider_selection.py`
  если он сам грузит `.env`.

Boot читает только `os.environ`.

## Acceptance criteria

- [x] `grep -rn 'load_dotenv\\|dotenv\\.' tabula-distrib tabula-bundles` →
      только в kernel-tested compatibility shim (если вообще нужен) или пусто.
- [x] Тест в `tabula/internal/tabula/`: `.env` с `FOO=from_file`, shell env
      с `FOO=from_shell` → kernel запускает boot, boot stdout содержит
      `FOO=from_shell`.
- [x] Тест в том же месте: при отсутствии shell-override boot видит
      `FOO=from_file`.
- [x] Документировано в `tabula/docs/ARCHITECTURE.md` (или
      `DISTRO_CONFIG.md`): kernel — single loader of `.env`.

## Files

- Edit: `tabula/internal/tabula/app.go` (около строки 291, env loading)
- Edit: `tabula/internal/runtime/host/policy/bare/bare.go` — env passthrough
  audit
- New test: `tabula/internal/tabula/dotenv_test.go`
- Edit: `tabula-distrib/code-immune/boot.py`
- Edit: `tabula-distrib/claw/boot.py` (после code-immune)
- Edit: `tabula-bundles/_lib/python/src/tabula_drivers/provider_selection.py`
  если содержит `.env` loading
- Edit: `tabula/docs/ARCHITECTURE.md` (или DISTRO_CONFIG.md)

## Verify

```bash
go test ./internal/tabula/... -run TestDotenv -race
grep -rn 'load_dotenv\\|dotenv\\.' tabula-distrib tabula-bundles
tabula-testbed run baseline
tabula-testbed run code-immune
```

## Risk

Низкий-средний. Риск регрессии — distro boot ожидал, что `dotenv.load_dotenv`
загрузит file, которого нет в kernel context. Покрывается тестом и явным
audit-ом списка env-ов, который kernel пробрасывает.

## Blocked by

`002-shared-paths-helper.md` — нужен общий `tabula_home()` чтобы knew, где
`$TABULA_HOME/.env`.

## Notes

- Workspace-level `.env` (в директории проекта пользователя) — отдельный вопрос.
  Если он нужен, kernel читает его в дополнение к `$TABULA_HOME/.env`, и явно
  документирует порядок: shell > workspace `.env` > `$TABULA_HOME/.env`.
- НЕ оставлять "shim" в boot-ах, который грузит `.env` если kernel этого не
  сделал. Один loader или его нет.

## Outcome

Реализовано: kernel (`tabula/internal/tabula/app.go:loadEnvFile`) — единственный
loader `$TABULA_HOME/.env`. Boot subprocess наследует env через стандартное
process inheritance: `runBoot` → `mainShellCommand` → `shell.Command` не
выставляет `cmd.Env`, значит `os.Environ()` пробрасывается as-is. Precedence:
shell > file (file-значения применяются с `setdefault`-семантикой через
`os.LookupEnv` check в `loadEnvFile`).

Изменения:

- **Удалено**: `load_env()` из `tabula-distrib/claw/boot.py` (строки 50–65) и
  `tabula-distrib/guardian/boot.py` (строки 16–30). В обоих случаях вызов
  `load_env()` тоже удалён.
- **Удалено**: дубликат `TABULA_HOME`/`ROOT` в `claw/boot.py` — теперь
  `ROOT` строится через bootstrap (как в `code-immune/boot.py`), и
  `TABULA_HOME = str(paths.tabula_home())` после того как
  `tabula_plugin_sdk` стал импортируемым.
- **Проверено**: `code/boot.py`, `code-immune/boot.py`, `testbed/boot.py` —
  никогда не имели `.env`-loader. `tabula_drivers.provider_selection`
  тоже чист.
- **Тесты**: добавлен `tabula/internal/tabula/dotenv_test.go` с двумя
  e2e-тестами (`TestDotenvKernelLoadsAndBootInheritsEnv`,
  `TestDotenvMissingFileIsSilent`). Существующие
  `TestLoadEnvFileLoadsMissingValues` и
  `TestLoadEnvFileDoesNotOverrideExistingValues` остаются. Все 4 зелёные.
- **Документация**: в `tabula/docs/ARCHITECTURE.md` добавлена секция
  `### .env loading` после `## Boot` (precedence + правило «boot читает
  только `os.environ`»). Удалён устаревший пункт «loads `.env`» из секции
  Claw boot.

Verification: `go test ./... -race -count=1` зелёный; `go vet ./...` чист;
`gofmt -l .` пуст; testbed baseline `direct` + `lint` passed; pre-existing
baseline failures без изменений (2 в `_lib/python` envelope-drift, 2 в
`tools/tabula-distro` manifest schema drift, 6 в
`tabula-distrib/claw/tests/test_gateway_telegram.py` provider-config drift —
все не связаны с этим issue).
