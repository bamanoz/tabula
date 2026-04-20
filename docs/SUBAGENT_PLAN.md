# Subagent improvements plan

Цель: подтянуть subagent-слой tabula до operational-уровня, выборочно
заимствуя зрелые паттерны из openclaw (`src/agents/subagent-*`,
`src/auto-reply/reply/commands-subagents/action-*`), но **не** копируя его
объём (75 файлов, ~1700 строк только в core + announce/delivery pipeline).

Сохраняем сильную сторону tabula: subagent = отдельный процесс с собственным
WS-подключением к kernel, реальная process isolation, kernel-first модель.

## Состояние сейчас

- `distrib/assistant/skills/subagent-{anthropic,openai}/run.py` — тонкие
  адаптеры (~96 строк), выбирают провайдера.
- `skills/lib/subagent_runtime.py` (156 строк) — общий runtime: connect →
  join → init → loop `generate / tool_use / tool_result` до `max_turns=20`
  → `message` в parent session → опциональный idle-loop на followup.
- `skills/lib/driver_runtime.py` со стороны parent: ловит `process_spawn`,
  парсит `--id`, копит `subagent_id → result`, вклеивает в следующий turn
  как `<subagent_result id="...">...</subagent_result>`.
- Kernel: `internal/kernel/policy.go:CanSpawn` — spawn token, MaxSpawnDepth,
  MaxChildren, hook `before_spawn`. `process_manager.go` управляет
  процессами. Tool `process_spawn` определён в `protocol.go`.

## Чего не хватает

1. Нет persistent registry запусков. Рестарт kernel = всё забыто.
2. Нет control API: list / info / kill / steer / wait.
3. Нет lifecycle events для observer / UI.
4. Нет orphan recovery: если subagent-процесс умер, parent-driver висит
   до `TOOL_RESULT_TIMEOUT=120s`.
5. Нет tool allowlist на spawn — subagent наследует **полный** toolset
   parent-а через `init_msg["tools"]`. Это и sharp, и opasnoe.

## План работ (в порядке выполнения)

### 1. Orphan recovery (приоритет — это **баг**, а не фича)

Бюджет: ~1 день, ~80 строк.

Когда `SpawnedProcess` для `agent_id=X` завершился (по любой причине), а
parent-driver всё ещё ждёт `subagent_result` для X, kernel должен
синтезировать в parent session `message` с агент-id X и текстом вида
`<error>subagent process died: exit=N</error>`. Сейчас driver просто висит
TOOL_RESULT_TIMEOUT секунд, и это выглядит как зависание системы.

Файлы:
- `internal/kernel/process_supervisor.go` — точка, где детектится exit.
- `internal/kernel/process_manager.go` или новый `subagent_recovery.go` —
  отправка fake-message в parent.
- Тест в `internal/kernel/kernel_test.go`: убить process до прихода
  результата, проверить что в parent пришёл `message` с `<error>`.

Acceptance:
- Существующий integration-test scenario с упавшим subagent перестаёт
  тайм-аутиться, parent получает ошибку за <1s.

### 2. Run registry + tools `subagent_list` / `subagent_info`

Бюджет: ~1–2 дня, ~250 строк.

Расширить `SpawnedProcess` (или sibling-структура `SubagentRun`) полями:
`agent_id`, `parent_session`, `task`, `model`, `provider`, `started_at`,
`finished_at`, `final_text`, `exit_reason`, `turn_count`.

Persistence: один JSON-файл `~/.tabula/state/subagent-runs.json`. Без
SQLite — у нас компактная архитектура. Запись append-only при старте,
update при finish. На рестарте kernel читаем, помечаем неподтверждённые
как `crashed`.

Новые kernel tools (в `protocol.go`):
- `subagent_list` — параметры: `recent_minutes` (default 30), `parent_session`
  (optional). Возвращает массив run-ов.
- `subagent_info` — параметры: `agent_id`. Возвращает полную запись.

Subagent runtime отправляет `turn` lifecycle event (см. п. 3) — registry
инкрементирует `turn_count`.

Файлы:
- `internal/kernel/subagent_registry.go` (новый).
- `internal/kernel/subagent_registry_test.go` (новый).
- `internal/kernel/protocol.go` — добавить tool constants.
- `internal/kernel/tool_service.go` — wire-up.

Acceptance:
- После 3 spawn-ов `subagent_list` возвращает 3 записи с корректными
  полями. Рестарт kernel — записи на месте, активные помечены `crashed`.

### 3. Lifecycle hooks

Бюджет: ~полдня, ~50 строк.

В существующий `dispatchHook` добавить точки:
- `subagent_started` — payload: `{agent_id, parent_session, task, model}`.
- `subagent_turn` — payload: `{agent_id, turn_index, has_tool_calls}`.
- `subagent_finished` — payload: `{agent_id, final_text, turn_count}`.
- `subagent_failed` — payload: `{agent_id, error, exit_reason}`.

Observer (`distrib/assistant/skills/observer`) автоматом получает feed —
без изменений API observer-а.

Файлы:
- `internal/kernel/hooks.go` — добавить event constants.
- `skills/lib/protocol.py` — синхронизировать константы.
- `internal/kernel/process_supervisor.go` или `subagent_registry.go` —
  точки вызова.
- `tests/test_observer.py` — расширить (помечен как e2e, ок).

### 4. Tool allowlist на spawn

Бюджет: ~полдня, ~40 строк.

Добавить в `process_spawn` опциональный `--tools "shell,read,grep"`
(comma-separated). Kernel при отправке `init`-сообщения subagent-у
фильтрует `tools` по allowlist. Если параметра нет — поведение прежнее
(полный toolset, обратная совместимость).

Файлы:
- `skills/lib/driver_runtime.py:extract_spawn_id` — расширить парсинг,
  чтобы пробросить allowlist в kernel через `process_spawn` args.
- `internal/kernel/process_manager.go` — хранить allowlist на process.
- `internal/kernel/connect.go` или где формируется `init` — фильтрация.
- Новый тест в `tests/test_subagent_e2e.py`.

Acceptance:
- Spawn с `--tools "read,grep"` — subagent не видит `shell_exec` в init.

### 5. Control tool `subagent_control`

Бюджет: ~2 дня, ~200 строк.

Один kernel tool с диспатчем по `action`:
- `kill` — SIGTERM через `process_manager`. Запись в registry помечается
  `killed`.
- `steer` — отправить `message` в subagent session с throttling 2s
  (steal `STEER_RATE_LIMIT_MS=2000` из openclaw).
- `wait` — polling registry до terminal state или timeout.
- `list`, `info` — алиасы для п. 2 (можно не дублировать).

Файлы:
- `internal/kernel/subagent_control.go` (новый).
- `internal/kernel/protocol.go` — `subagent_control` constant.
- `tests/test_subagent_control.py` (новый).

Acceptance:
- `subagent_control(action="kill", agent_id="X")` — процесс мёртв за 1s,
  parent получает `<error>killed</error>` (через п. 1).
- `subagent_control(action="steer", agent_id="X", text="...")` — subagent
  получает текст как followup-message.
- Два steer-а подряд за <2s — второй возвращает rate-limit error.

## Что **не** делаем

- Announce/delivery pipeline openclaw (`subagent-announce-*`, ~20 файлов).
  Это нужно для UX где subagent говорит в разные каналы с форматированием.
  Override kernel-first модели tabula, не нужен.
- SQLite persistence. JSON достаточен.
- Multi-tier capability inheritance (`agent-scope.ts`). Плоский allowlist
  на spawn проще и достаточен.
- Nested subagent registry. Уже есть `MaxSpawnDepth` в policy.
- Docker sandboxing per subagent. Отдельная тема.

## Итого

Бюджет: ~5–6 дней, +600–800 строк (80% Go в kernel, 20% Python).

Порядок выполнения = порядок в плане. Каждый пункт — отдельный коммит,
каждый имеет acceptance criteria и юнит/e2e тесты.

После п. 1 уже сразу починится самая видимая операционная проблема
(подвисание driver-а на 120s при упавшем subagent).
