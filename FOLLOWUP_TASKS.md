# Follow-Up Tasks

## Цель

Завершить внешнюю миграцию `tabula-bundles`, разблокировать перенос SDK/lib и после этого удалить устаревшие compatibility bridges из `tabula`.

## Текущий статус

Repo-local hardening и D1.11(b) kernel spawn bridge cleanup в `tabula` завершены.

External `tabula-bundles` consumers больше не импортируют `skills._pylib` или `skills._tslib`; они переведены на packaged `_lib` SDK surface.

## Выполнено

- Задача 1 выполнена: per-call skills в `tabula-bundles` переведены на YAML `tools[]` с явным `exec`.
- Задача 10 выполнена: kernel-side D1.11(b) spawn-token/depth/MaxChildren bridge удален после green subagent plugin evidence.
- Задача 8 выполнена по external consumer imports: `tabula-bundles` больше не требует `skills/_pylib` или `skills/_tslib` imports.

## Задача 1: Мигрировать per-call skills в `tabula-bundles`

### Объем

Добавить явный `tools[].exec` для оставшихся per-call skills в `tabula-bundles`.

### Готово, когда

- У каждого advertised tool есть непустой `name`.
- У каждого advertised tool есть непустой `exec`.
- Duplicate tool names удалены или отклоняются валидатором.
- Distro/kernel validator проходит на migrated bundles.
- Functional smoke tests вынесены в отдельный follow-up harness, потому что часть tools interactive, destructive или зависит от внешних сервисов.

### Проверка

- Запустить bundle manifest validation.
- Запустить distro install validation.
- Зафиксировать отдельную follow-up задачу для safe functional smoke harness.

### Статус

Выполнено.

Мигрированные paths:

- `base/mcp/SKILL.md`
- `caveman/caveman-compress/SKILL.md`
- `code/git/SKILL.md`
- `code/review/SKILL.md`
- `subagents/subagents/SKILL.md`
- `code/todo/SKILL.md`
- `code/ask-user/SKILL.md`
- `code/workspace/SKILL.md`
- `files/files/SKILL.md`
- `memory/memory-admin/SKILL.md`
- `memory/memory-save/SKILL.md`
- `memory/memory-search/SKILL.md`

Validation command:

```bash
PYTHONPATH="tools/tabula-distro/src" python3 - <<'PY'
from pathlib import Path
import tempfile
from tabula_distro import install as installmod
root = Path(tempfile.mkdtemp(prefix='tabula-bundles-validate-'))
distro = root / 'demo'
(distro / 'templates').mkdir(parents=True)
(distro / 'skills').mkdir()
(distro / 'boot.py').write_text('# boot\n', encoding='utf-8')
(distro / 'templates' / 'SYSTEM.md').write_text('hello\n', encoding='utf-8')
bundles = Path('/Users/mak/src/tabula-bundles').resolve()
entries = []
for bundle in sorted(p for p in bundles.iterdir() if p.is_dir() and (p / 'bundle.toml').is_file()):
    entries.append(f'[[bundles]]\nname="{bundle.name}"\nsource="local:{bundle}"\n')
(distro / 'distro.toml').write_text('[distro]\nname="demo"\n\n' + '\n'.join(entries), encoding='utf-8')
home = root / 'home'
home.mkdir()
kernel_version = Path('VERSION').read_text(encoding='utf-8').strip()
(home / 'VERSION').write_text(kernel_version + '\n', encoding='utf-8')
result = installmod.install(distro, home)
print(f'installed generation={result.generation.number} bundles={len(result.lock.bundles)} skills={len(result.lock.skills)} plugins={len(result.lock.plugins)} kernel_version={kernel_version}')
print(root)
PY
```

Validation result:

```text
installed generation=1 bundles=10 skills=31 plugins=0 kernel_version=0.8.0
```

Functional smoke tests не блокируют эту миграцию. Они вынесены в задачу 1a ниже.

## Задача 1a: Добавить safe functional smoke harness для per-call skills

### Объем

Создать отдельный набор smoke tests для migrated per-call skills, не смешивая его с manifest migration.

### Готово, когда

- Есть allowlist безопасных read-only/non-interactive tools для автоматического запуска.
- Interactive tools явно помечены как manual или skipped с причиной.
- Destructive tools запускаются только в temporary sandbox.
- Tools с external dependencies имеют guarded tests или mock fixtures.
- Результат smoke harness документируется отдельно от `tools[].exec` manifest validation.

### Проверка

- Запустить safe subset smoke tests.
- Проверить, что skipped/manual список явно перечисляет причину для каждого skipped tool.

## Задача 2: Мигрировать hook plugins

### Объем

Перевести старые hook-style bundle components на явные plugin manifests/runtime.

### Компоненты

- permission hooks
- approval hooks
- workspace boundary hooks
- logger/observer hooks
- caveman hooks, если они еще активны

### Готово, когда

- Hook plugin manifests существуют.
- Hook subscriptions объявлены явно.
- Invalid subscriptions отклоняются.
- Malformed `event_reply` fail closed.
- Security hooks нельзя обойти через malformed replies.

### Проверка

- Plugin manifest validation.
- Hook lifecycle smoke test.
- Negative protocol tests для malformed replies.

### Статус

Выполнено.

Мигрированные plugin paths:

- `base/hook-permissions/plugin.toml`
- `base/hook-logger/plugin.toml`
- `base/observer/plugin.toml`
- `code/hook-workspace-boundary/plugin.toml`
- `code/hook-approvals/plugin.toml`
- `caveman/hook-caveman/plugin.toml`

Старые `SKILL.md` для этих components заменены на `README.md`, потому что distro installer считает `SKILL.md` и `plugin.toml` взаимоисключающими component kinds.

Validation commands:

```bash
PYTHONPATH="/Users/mak/src/tabula-bundles/_lib/python/src:/Users/mak/src/tabula" python3 -m py_compile base/hook-permissions/run.py base/hook-logger/run.py base/observer/run.py code/hook-workspace-boundary/run.py code/hook-approvals/run.py caveman/hook-caveman/run.py
go test ./internal/kernel -run TestTabulaBundlesHookPluginsLiveE2E -count=1 -timeout 30s
go test ./internal/kernel/plugin ./internal/kernel -count=1 -timeout 90s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
hook plugin py_compile: OK
TestTabulaBundlesHookPluginsLiveE2E: OK
go test ./internal/kernel/plugin ./internal/kernel: OK
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
external bundle install: installed generation=1 bundles=10 skills=25 plugins=6 kernel_version=0.8.0, lib_python=True
```

Задача 2 потребовала выполнить dependency из задачи 6 и часть задачи 8: Python SDK wheel создан, а distro installer теперь устанавливает root-level/sibling `_lib/python` из `tabula-bundles` в `$TABULA_HOME/_lib/python`.

## Задача 3: Мигрировать MCP plugin

### Объем

Перевести MCP bundle functionality на plugin runtime model.

### Готово, когда

- MCP plugin manifest существует.
- Plugin корректно регистрирует initial tools.
- Dynamic `update_tools` работает атомарно.
- Invalid dynamic catalogs отклоняются.
- Malformed `tool_result` / `event_reply` не мутирует состояние.

### Проверка

- MCP plugin lifecycle smoke test.
- Tool registration/update test.
- Malformed protocol input test.

### Статус

Выполнено.

Мигрированный plugin path:

- `base/mcp/plugin.toml`

Что изменено:

- `base/mcp/SKILL.md` заменен на `base/mcp/README.md`.
- Generic MCP tools регистрируются при plugin startup.
- First-class `mcp__<server>__<tool>` tools публикуются через runtime `update_tools` после discovery.
- Boot-time helper `mcp.register.mcp_tool_entries` оставлен как compatibility helper, но active registration больше не зависит от boot-time discovery.
- HTTP dependency `requests` импортируется lazily, поэтому stdio MCP работает без extra package dependency.

Validation commands:

```bash
PYTHONPATH="/Users/mak/src/tabula-bundles/_lib/python/src:/Users/mak/src/tabula" python3 -m py_compile base/mcp/run.py base/mcp/register.py base/mcp/daemon.py base/mcp/pool.py base/mcp/client.py
go test ./internal/kernel -run TestTabulaBundlesMCPPluginLiveE2E -count=1 -timeout 30s
go test ./internal/kernel/plugin ./internal/kernel -count=1 -timeout 90s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
MCP py_compile: OK
TestTabulaBundlesMCPPluginLiveE2E: OK
go test ./internal/kernel/plugin ./internal/kernel: OK
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
external bundle install: installed generation=1 bundles=10 skills=24 plugins=7 kernel_version=0.8.0, plugins=[hook-approvals hook-caveman hook-logger hook-permissions hook-workspace-boundary mcp observer], lib_python=True
```

MCP E2E использует fake stdio MCP server и проверяет:

- plugin lifecycle/register;
- generic tools `mcp_list_servers` и `mcp_call`;
- runtime `update_tools` для `mcp__fake__echo`;
- first-class tool dispatch;
- legacy `run.py tool mcp_list_servers` compatibility command.

## Задача 4: Мигрировать driver/subagent plugins

### Объем

Перенести driver и subagent execution под plugin-side ownership.

### Готово, когда

- Driver/subagent plugin manifests существуют.
- Plugin-side spawn authorization реализован.
- Spawn depth enforced.
- `MaxChildren` enforced.
- `list`, `wait`, `kill`, `cancel` и cleanup покрыты тестами.
- Malformed spawn/security replies fail closed.

### Проверка

- Driver plugin lifecycle test.
- Subagent spawn/list/wait/kill smoke test.
- Depth limit test.
- Max children test.
- Malformed auth/spawn reply test.

### Разблокирует

- Удаление D1.11(b) kernel spawn bridge.
- Удаление skipped kernel spawn tests, которые покрывают только старое поведение.

### Статус

Выполнено для primary spawn owner: `subagents/subagents`.

Решение по scope:

- Primary migration target: `subagents/subagents`, потому что именно этот component владеет spawn/list/wait/kill lifecycle.
- `drivers/driver` и `drivers/subagent` пока остаются process runners, а не tool-owning plugins. Их миграция не закрывает spawn/depth/MaxChildren blocker сама по себе.

Мигрированный plugin path:

- `subagents/subagents/plugin.toml`

Что изменено:

- `subagents/subagents/SKILL.md` заменен на `README.md`.
- `subagent_spawn`, `subagent_send`, `subagent_steer`, `subagent_wait`, `subagent_list`, `subagent_kill` теперь plugin tools.
- Default limits: `max_spawn_depth=3`, `max_children=5`.
- Limits configurable through plugin config.
- Spawn registry records `depth` and `parent_depth`.
- Plugin enforces depth and active child count before spawning.
- Type presets are installed under `plugins/subagents/types/*.toml`.

Validation commands:

```bash
PYTHONPATH="/Users/mak/src/tabula-bundles/_lib/python/src:/Users/mak/src/tabula" python3 -m py_compile subagents/subagents/run.py drivers/subagent/run.py drivers/driver/run.py
go test ./internal/kernel -run TestTabulaBundlesSubagentsPluginLimitsLiveE2E -count=1 -timeout 30s
go test ./internal/kernel/plugin ./internal/kernel -count=1 -timeout 90s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
subagents py_compile: OK
TestTabulaBundlesSubagentsPluginLimitsLiveE2E: OK
go test ./internal/kernel/plugin ./internal/kernel: OK
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
external bundle install: installed generation=1 bundles=10 skills=23 plugins=8 kernel_version=0.8.0, plugins=[hook-approvals hook-caveman hook-logger hook-permissions hook-workspace-boundary mcp observer subagents], subagent_types=True
```

Subagents E2E проверяет:

- plugin lifecycle/register;
- tool dispatch for spawn/list/wait/kill surface;
- successful async spawn with registry entry;
- `MaxChildren` denial;
- depth denial;
- list visibility by parent session;
- kill/cleanup of spawned child process.

## Задача 5: Мигрировать gateway plugins

### Объем

Перевести gateway components на явное plugin ownership.

### Готово, когда

- Gateway plugin manifests существуют.
- Lifecycle start/stop behavior работает.
- Plugin-side process ownership явно определен.
- Malformed plugin replies не вызывают partial side effects.

### Проверка

- Gateway lifecycle smoke test.
- Plugin manifest validation.
- Malformed reply negative test.

### Статус

Выполнено для daemon gateways.

Решение по scope:

- В `tabula-bundles` gateway components отсутствуют.
- Реальные gateway sources находятся в `/Users/mak/src/tabula-distrib`.
- `gateway-telegram` мигрирован в plugin-owned wrapper (`claw/plugins/gateway-telegram-plugin`); daemon живёт внутри plugin dir.
- `gateway-api` удалён целиком.
- Interactive gateways (`gateway-cli`, `gateway-tui`) намеренно остаются client-owned launchers, потому что им нужно владеть terminal/UI lifecycle.

Мигрированные plugin paths:

- `/Users/mak/src/tabula-distrib/claw/plugins/gateway-telegram-plugin/plugin.toml`

Что изменено:

- Wrapper plugin для `gateway-telegram` запускает и останавливает sibling `daemon.py`.
- Status tool: `gateway_telegram_status`.
- Shutdown path отправляет SIGTERM process group и при необходимости SIGKILL.

Validation commands:

```bash
go test ./internal/kernel -run TestTabulaDistribGatewayPluginWrappersLiveE2E -count=1 -timeout 30s
go test ./internal/kernel/plugin ./internal/kernel -count=1 -timeout 90s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
gateway wrapper py_compile: OK
TestTabulaDistribGatewayPluginWrappersLiveE2E: OK
go test ./internal/kernel/plugin ./internal/kernel: OK
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
```

Gateway E2E использует fake daemon runner scripts и проверяет:

- plugin manifest load/register;
- wrapper starts child daemon process;
- status tool reports running process and pid;
- graceful plugin shutdown cleans up child process.

## Задача 6: Собрать Python SDK package

### Объем

Собрать installable Python SDK artifact вне `skills/_pylib`.

### Требуемый artifact

```text
_lib/python/dist/tabula_plugin_sdk-*.whl
```

### Готово, когда

- Wheel успешно собирается.
- Wheel устанавливается без local source hacks.
- Contract tests проходят против installed wheel.
- Bundle consumers используют packaged SDK path.

### Проверка

```bash
pip install --no-index --find-links _lib/python/dist tabula_plugin_sdk
```

### Разблокирует

- Удаление `skills/_pylib`.
- Удаление imports вида `skills._pylib`.
- Удаление distro `_pylib` preserve behavior.

### Статус

Выполнено для package artifact и migrated bundle consumers.

Consumer migration:

- `tabula-bundles` Python consumers переведены с `skills._pylib.*` на `tabula_plugin_sdk.*` helpers.
- В packaged SDK добавлены runtime helper modules: `paths`, `config`, `kernel_client`, `filelock`, plus skill/kernel protocol constants in `protocol`.
- `skills._tslib` imports in `tabula-bundles` отсутствуют.

Artifact:

```text
/Users/mak/src/tabula-bundles/_lib/python/dist/tabula_plugin_sdk-0.1.0-py3-none-any.whl
```

Validation commands:

```bash
PYTHONPATH="src" python3 -m unittest tests/test_contract.py
python3 -m pip wheel . --no-deps --wheel-dir dist
tmpdir=$(mktemp -d) && python3 -m venv "$tmpdir/venv" && "$tmpdir/venv/bin/python" -m pip install --no-index --find-links dist tabula-plugin-sdk && "$tmpdir/venv/bin/python" -m unittest discover -s tests
```

Validation result:

```text
PYTHONPATH="src" python3 -m unittest tests/test_contract.py: Ran 6 tests, OK
python3 -m compileall -q _lib/python/src base subagents drivers code: OK
python3 -m pip wheel . --no-deps --wheel-dir dist: Successfully built tabula-plugin-sdk
installed wheel contract tests: Ran 6 tests, OK
grep skills._pylib / skills._tslib in tabula-bundles: no files found
```

## Задача 7: Собрать TypeScript SDK package

### Объем

Собрать installable TypeScript SDK artifact вне `skills/_tslib`.

### Требуемый artifact

```text
_lib/typescript/dist/tabula-skill-sdk-*.tgz
```

### Готово, когда

- Tarball успешно собирается.
- Consumer install работает.
- Contract tests проходят против installed package.
- Bundle consumers используют packaged SDK path.

### Проверка

```bash
bun install _lib/typescript/dist/tabula-skill-sdk-*.tgz
```

### Разблокирует

- Удаление `skills/_tslib`.
- Удаление references вида `skills._tslib`.
- Удаление distro `_tslib` preserve behavior.

### Статус

Выполнено для package artifact.

Artifact:

```text
/Users/mak/src/tabula-bundles/_lib/typescript/dist/tabula-skill-sdk-0.1.0.tgz
```

Validation commands:

```bash
bun install
bun test
bun run typecheck
rm -rf dist && mkdir -p dist && bun pm pack --destination dist
tmpdir=$(mktemp -d) && cd "$tmpdir" && bun init -y >/dev/null && bun install /Users/mak/src/tabula-bundles/_lib/typescript/dist/tabula-skill-sdk-0.1.0.tgz && bun test
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
bun test: 4 pass, 0 fail
tsc --noEmit: OK
bun pm pack: /Users/mak/src/tabula-bundles/_lib/typescript/dist/tabula-skill-sdk-0.1.0.tgz
consumer install: installed @tabula/skill-sdk@.../tabula-skill-sdk-0.1.0.tgz
consumer tests: 2 pass, 0 fail
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
external bundle install: lib_typescript=True
```

Consumer smoke imports match the current `gateway-tui` usage:

- `@tabula/skill-sdk`
- `@tabula/skill-sdk/protocol`

## Задача 8: Подтвердить финальный bundle `_lib` layout

### Объем

Подтвердить, что external bundles устанавливают SDK support files через `_lib`.

### Требуемый layout

```text
_lib/python
_lib/typescript
```

### Готово, когда

- Bundle install smoke test проходит.
- Ни один consumer не требует `skills/_pylib`.
- Ни один consumer не требует `skills/_tslib`.
- Ни один consumer не требует inline sibling `_*` compatibility dirs.

### Проверка

- Distro install test.
- External bundle install smoke test.

### Разблокирует

- Удаление `_materialize_inline_symlinks` compatibility.
- Удаление magic `skills/_*` support-dir handling.

### Статус

Выполнено для `tabula-bundles` active imports и `_lib` package layout.

Мигрированные consumer areas:

- Hook plugins: `base/hook-permissions`, `base/hook-logger`, `code/hook-workspace-boundary`, `code/hook-approvals`.
- Subagents plugin: `subagents/subagents`.
- Driver runners/helpers: `drivers/driver`, `drivers/subagent`, `drivers/_drivers/*`.
- Per-call/helper skills: `code/ask-user`, `base/sessions`, `code/todo`, `code/workspace`, `base/pair`, `base/mcp/daemon.py`, `base/cron`.

Validation commands:

```bash
grep -R "skills\._pylib\|skills\._tslib" /Users/mak/src/tabula-bundles
PYTHONPATH="src" python3 -m unittest tests/test_contract.py
python3 -m compileall -q _lib/python/src base subagents drivers code
go test ./internal/kernel -run 'TestTabulaBundles(HookPluginsLiveE2E|MCPPluginLiveE2E|SubagentsPluginLimitsLiveE2E)' -count=1 -timeout 60s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
legacy import grep: no files found
Python SDK contract tests: Ran 6 tests, OK
compileall affected bundles: OK
bundle live E2E: ok github.com/bamanoz/tabula/internal/kernel 0.828s
distro install tests: Ran 34 tests, OK
```

## Задача 9: Обновить evidence matrix

### Объем

Обновить `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`.

### Готово, когда

Каждая завершенная строка содержит:

- source path
- target path
- commit или PR link
- validation command
- validation result
- cleanup gate, который теперь разблокирован

### Правило

Не отмечать строку green только на основании намерения, документации или частичных файлов.

## Задача 10: Удалить D1.11(b) kernel compatibility

### Объем

После того как driver/subagent plugin evidence станет green, удалить obsolete kernel-side spawn bridge code.

### Кандидаты на удаление

- kernel-side spawn token generation
- generic `TABULA_SPAWN_TOKEN` propagation
- `PolicyEngine.CanSpawn` bridge behavior
- kernel `MaxChildren` / depth bridge paths, если они полностью plugin-owned
- skipped kernel spawn tests, которые покрывают только удаленное поведение

### Готово, когда

- Replacement plugin-side tests покрывают поведение.
- Kernel tests проходят.
- Active docs больше не обещают удаленный generic spawn API.

### Статус

Выполнено.

Что удалено из kernel-side compatibility surface:

- `SpawnTokenStore` и файл `internal/kernel/spawn_token_store.go`.
- `generateSpawnToken`.
- `PolicyEngine.CanSpawn`.
- `ProcessManager.Spawn` и `SpawnResult`.
- `ProcessLauncher.Start` interface method.
- `Hub` fields `tokens`, `MaxSpawnDepth`, `MaxChildren`.
- Kernel-emitted hook events `before_spawn` и `after_spawn` из active `HookEvents` list.
- Spawn-token/depth accessors from `state_access.go`.
- Skipped legacy kernel spawn/process tests that only covered removed builtins/bridge behavior, including spawn/list/kill, token one-time/expiry/env propagation, depth/max-children bridge behavior, cancel-scoped spawned process behavior, and kernel `process_spawn` self-hook coverage.

Compatibility note:

- `NewHub(..., maxSpawnDepth, maxChildren, ...)` keeps the old constructor signature for current callsites, but these values are ignored because subagent spawn policy is now plugin-owned.
- Non-empty connect spawn tokens are rejected with `spawn tokens are no longer accepted by the kernel`.
- Deprecated `ToolProcess*` constants remain as stale-name compatibility strings for validator/tests; kernel still does not dispatch those builtins.

Validation command:

```bash
gofmt -w internal/kernel && go test ./internal/kernel ./internal/kernel/plugin -count=1 -timeout 90s
```

Validation result:

```text
ok  github.com/bamanoz/tabula/internal/kernel        38.869s
ok  github.com/bamanoz/tabula/internal/kernel/plugin 0.224s
```

## Задача 11: Удалить SDK/lib compatibility из `tabula`

### Объем

После того как Python/TS SDK package evidence станет green, удалить temporary in-repo SDK support dirs.

### Кандидаты на удаление

- `skills/_pylib`
- `skills/_tslib`
- distro `_pylib` / `_tslib` preserve behavior
- inline sibling `_*` support-dir compatibility
- old script references
- old active docs references

### Готово, когда

- Packaged SDK contract tests заменяют old support-dir tests.
- Distro tests проходят.
- Active runtime code больше не импортирует `skills._pylib` или `skills._tslib`.

### Статус

Выполнено.

Что удалено:

- `skills/_pylib/`.
- `skills/_tslib/`.
- `conftest.py` reference to `skills/_pylib/test_protocol.py`.
- Distro runtime preserve behavior for `skills/_pylib` and `skills/_tslib`.
- Inline symlink materialization compatibility that copied sibling `_*` support dirs into staged `skills/`.

Replacement evidence:

- Python SDK package in `tabula-bundles/_lib/python` contains the runtime helpers formerly imported from `skills._pylib`.
- TypeScript SDK package in `tabula-bundles/_lib/typescript` replaces the old `_tslib` support dir.
- `tabula-bundles` active imports no longer reference `skills._pylib` or `skills._tslib`.

Validation commands:

```bash
gofmt -w internal/kernel cmd/tabula
python3 -m compileall -q tools/tabula-distro/src tools/tabula-distro/tests conftest.py
go test ./cmd/tabula ./internal/kernel ./internal/kernel/plugin -count=1 -timeout 120s
PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py
```

Validation result:

```text
compileall: OK
ok  github.com/bamanoz/tabula/cmd/tabula              0.282s
ok  github.com/bamanoz/tabula/internal/kernel         39.153s
ok  github.com/bamanoz/tabula/internal/kernel/plugin  0.627s
tools/tabula-distro/tests/test_install.py: Ran 34 tests, OK
grep runtime code for skills/_pylib, skills/_tslib, skills._pylib, skills._tslib: no files found
skills/_*lib glob: no files found
```

## Финальная проверка

Запустить после всех cleanup tasks:

```bash
go test ./cmd/tabula ./internal/kernel ./internal/kernel/plugin
python -m pytest tools/tabula-distro/tests
```

Также запустить external `tabula-bundles` smoke tests и SDK package contract tests.

## Остаточный риск

External evidence стала green, и repo-side compatibility bridges удалены. Остаточный риск теперь связан с историческими Memory Bank/archive references и неактивными план-документами, которые описывают прошлое состояние; runtime/code grep по удаленным SDK dirs чистый.
