# Tabula Execution Plan

Дата: 2026-04-16

Этот документ раскладывает [`IMPLEMENTATION_BACKLOG.md`](/Users/mak/src/tabula/IMPLEMENTATION_BACKLOG.md) на отслеживаемые задачи, по которым удобно:

- заводить issues;
- назначать владельцев;
- вести прогресс;
- реализовывать работу параллельно без лишних конфликтов.

## Как использовать

- Один task ниже = один issue или одна рабочая карточка.
- Сохраняйте ID задачи при переносе в tracker.
- Обновляйте checkbox прямо в этом файле или отражайте статус в issue tracker.
- Не запускайте одновременно несколько задач с сильно пересекающимся write scope.

## Статусы

- `[ ]` не начато
- `[-]` в работе
- `[x]` завершено

## Current progress

Ниже зафиксирован текущий статус после первого параллельного delivery slice.

- `T0-01` завершен
- `T0-02` завершен
- `T0-03` завершен
- `T0-04` завершен
- `T0-05` завершен
- `T0-06` завершен
- `T0-07` завершен
- `T0-08` завершен
- `T0-09` завершен
- `T0-10` завершен
- `T0-11` завершен
- `T0-12` завершен

Первый реальный delivery slice уже дал:

- выровненный install/bootstrap contract;
- детерминированный smoke bootstrap path, согласованный с `tabula serve`;
- рабочий `gateway-api` baseline;
- concurrency-safe `gateway-api` server path с per-session turn serialization;
- Telegram gateway session lifecycle baseline с TTL/cleanup/turn serialization;
- безопасный process snapshot path;
- реальное применение modified payload в `before_tool_call`;
- observer выведен из modifying/security hook path в telemetry-only модель;
- разделение verification matrix на `unit/smoke/e2e/contract`;
- устранение красной unit-regression в `boot.py` вокруг duplicate tool override.

## Рекомендуемые parallel lanes

- `Lane A` install/bootstrap/release contract
- `Lane B` gateway runtime и transport
- `Lane C` process/session core
- `Lane D` hooks/policy/observability
- `Lane E` tests/verification
- `Lane F` durable state/control plane
- `Lane G` tools/permissions
- `Lane H` manifests/operator surfaces
- `Lane I` tasks/multi-agent/workspace

## Рекомендуемое разбиение на агентские потоки

Ниже не просто batches, а рекомендованные долгоживущие ownership streams для нескольких параллельных агентов.

### Stream 1. Bootstrap и operator entrypoints

- Цель:
  собрать надежный путь `install -> boot -> connect -> run`.
- Основной write scope:
  `scripts/`, `Makefile`, `bin/`, `README.md`, install docs, operator entrypoints.
- Основные задачи:
  `T0-01`, `T0-02`, позже `T3-04`, `T3-05`.
- Почему это хороший отдельный поток:
  он почти не конфликтует с kernel core и может идти параллельно с runtime hardening.
- Ключевой артефакт handoff:
  canonical install/runtime contract для всей команды.

### Stream 2. Gateway runtime

- Цель:
  сделать transport layer исполнимым, устойчивым и concurrency-safe.
- Основной write scope:
  `skills/gateway-api/run.py`, `skills/gateway-telegram/run.py`, gateway launchers.
- Основные задачи:
  `T0-03`, `T0-04`, часть `T0-07`, часть `T0-08`, позже `T1-06`.
- Почему это хороший отдельный поток:
  его можно развивать отдельно от storage и tools, если session semantics публикует Stream 3.
- Ключевой артефакт handoff:
  gateway session-state contract и transport-level concurrency model.

### Stream 3. Kernel runtime core

- Цель:
  стабилизировать process/session bookkeeping и сделать ядро источником правды по lifecycle.
- Основной write scope:
  `internal/kernel/session.go`, `internal/kernel/process_manager.go`, `internal/kernel/process_supervisor.go`, `internal/kernel/snapshot.go`, `internal/kernel/kernel.go`, `internal/kernel/message_router.go`.
- Основные задачи:
  `T0-05`, `T0-06`, `T0-07`, `T0-08`, позже `T1-04`, часть `T4-03`, часть `T4-07`.
- Почему это хороший отдельный поток:
  это самая конфликтная зона кода, поэтому у нее должен быть один явный владелец.
- Ключевой артефакт handoff:
  canonical process/session lifecycle contract.

### Stream 4. Hooks, tools и permissions

- Цель:
  превратить governance из идеи в реальный runtime contract.
- Основной write scope:
  `internal/kernel/hook_engine.go`, `internal/kernel/hooks.go`, `internal/kernel/policy.go`, `internal/kernel/tool_service.go`, `internal/kernel/tools*.go`, `skills/observer/run.py`, tool metadata surfaces.
- Основные задачи:
  `T0-09`, `T0-10`, `T2-01`, `T2-02`, `T2-03`, `T2-04`, `T2-05`, `T2-06`.
- Почему это хороший отдельный поток:
  write scope здесь естественно отделен от gateway и storage; можно параллелить почти весь слой после фиксации базовых entity IDs.
- Ключевой артефакт handoff:
  typed tool contract и approval/permission model, единая для всех gateways.

### Stream 5. State и control plane

- Цель:
  ввести durable backbone, на которую потом встанут tasks, resume/fork, introspection и future multi-node readiness.
- Основной write scope:
  новый state/control-plane слой, storage packages, migrations, persisted records, control-plane API.
- Основные задачи:
  `T1-01`, `T1-02`, `T1-03`, `T1-05`, `T1-07`, `T1-08`, позже `T4-01`, `T4-02`, `T4-05`, `T4-06`.
- Почему это хороший отдельный поток:
  это архитектурный backbone с собственной write зоной; после фиксации schema он разблокирует почти все поздние волны.
- Ключевой артефакт handoff:
  canonical entity/state schema, durable store, projector model, introspection API.

### Stream 6. Verification и contracts

- Цель:
  держать быстрый feedback loop и не дать runtime contract drift.
- Основной write scope:
  `tests/`, `*_test.go`, smoke harness, test docs/scripts.
- Основные задачи:
  `T0-11`, `T0-12`, позже `T3-03` и contract-test часть `T3-02`.
- Почему это хороший отдельный поток:
  этот поток почти не конфликтует с production code и может сопровождать остальные как независимый stabilizer.
- Ключевой артефакт handoff:
  reproducible test matrix: `unit`, `smoke`, `e2e`, `contract`.

## Как эти потоки запускать параллельно

### Рекомендуемая конфигурация: 6 агентов

- Agent A: Stream 1
- Agent B: Stream 2
- Agent C: Stream 3
- Agent D: Stream 4
- Agent E: Stream 5
- Agent F: Stream 6

Это лучший вариант по независимости write scopes.

### Если агентов 4

- Agent A: Stream 1 + Stream 6
- Agent B: Stream 2
- Agent C: Stream 3
- Agent D: Stream 4 + Stream 5

Это рабочий минимум, но Agent D станет самым загруженным.

### Если агентов 3

- Agent A: Stream 1 + Stream 2
- Agent B: Stream 3 + Stream 6
- Agent C: Stream 4 + Stream 5

Такой режим все еще жизнеспособен, если держать короткие sync cycles.

## Основные sync points между агентами

### Sync 1. После первых P0 задач

Нужны публикации от потоков:

- Stream 1:
  финальный install/runtime contract.
- Stream 2:
  gateway executable baseline.
- Stream 3:
  process/session record contract.
- Stream 4:
  hook/tool contract.
- Stream 6:
  test categories и smoke harness plan.

После этого можно безопасно запускать `T0-04`, `T0-06`, `T0-10`.

### Sync 2. После завершения Wave 0

Нужны публикации:

- canonical session lifecycle;
- stable gateway behavior;
- stable hook/tool behavior;
- regression matrix.

Только после этого стоит пускать full-scale работу по `T1-*`.

### Sync 3. После `T1-01` и `T1-02`

Это главный архитектурный checkpoint.

После него потоки получают:

- entity schema;
- storage abstraction;
- ownership fields;
- minimal persistence contract.

Этот sync unlock-ит:

- `T1-03`, `T1-04`, `T1-06`, `T1-08`;
- почти весь Wave 2;
- часть Wave 4.

### Sync 4. После `T2-01` и `T3-01`

После него можно безопасно двигать:

- manifest registry;
- operator surfaces;
- richer task integrations;
- contract tests на extensibility layer.

## Что нельзя делать двум агентам одновременно

- Не давайте двум агентам одновременно править `internal/kernel/process_*`, `session.go`, `snapshot.go`, `message_router.go`.
  Это всегда ownership Stream 3.
- Не давайте двум агентам одновременно править `hook_engine.go`, `policy.go`, `tool_service.go`.
  Это ownership Stream 4.
- Не смешивайте storage schema changes и gateway integration changes в одном PR без крайней необходимости.
  Сначала Stream 5 публикует schema, потом Stream 2 и 3 адаптируются.
- Не делайте test harness owner одновременно владельцем runtime semantics.
  Иначе regressions перестанут быть независимым сигналом.

## Recommended first 6-agent kickoff

Если запускать прямо сейчас шесть параллельных агентских потоков, то стартовый набор задач должен быть таким:

- Agent A / Stream 1:
  `T0-01`
- Agent B / Stream 2:
  `T0-03`
- Agent C / Stream 3:
  `T0-05`
- Agent D / Stream 4:
  `T0-09`
- Agent E / Stream 5:
  подготовка `T1-01` как design/doc track, без merge до завершения Wave 0
- Agent F / Stream 6:
  `T0-11`

Это дает лучший стартовый параллелизм с минимальным количеством merge-конфликтов.

## Batch plan

### Batch 0A

Эти задачи можно начинать почти сразу и параллельно:

- `T0-01`
- `T0-03`
- `T0-05`
- `T0-09`
- `T0-11`

### Batch 0B

Стартует после частичного завершения `0A`:

- `T0-02` после `T0-01`
- `T0-04` после `T0-03`
- `T0-06` после `T0-05`
- `T0-10` после `T0-09`

### Batch 0C

Стартует после стабилизации ядра и gateway path:

- `T0-07`
- `T0-08`
- `T0-12`

### Batch 1A

Foundation для durable control plane:

- `T1-01`
- `T1-02`
- `T1-07`

### Batch 1B

Поверх foundation:

- `T1-03`
- `T1-04`
- `T1-06`
- `T1-08`

### Batch 1C

User-visible control-plane operations:

- `T1-05`

### Batch 2A

Tools/permissions foundation:

- `T2-01`
- `T2-02`
- `T2-03`

### Batch 2B

Integration layer:

- `T2-04`
- `T2-05`
- `T2-06`

### Batch 3A

Extensibility/operator foundation:

- `T3-01`
- `T3-03`

### Batch 3B

После фиксации contracts:

- `T3-02`
- `T3-04`
- `T3-05`

### Batch 4A

Task platform foundation:

- `T4-01`
- `T4-03`
- `T4-06`

### Batch 4B

Task integration и guardrails:

- `T4-02`
- `T4-04`
- `T4-05`
- `T4-07`

## Tasks

## Wave 0. Stabilization и hardening

- [x] `T0-01` Fix install/dev/runtime contract
  Lane: `A`
  Area: `scripts/`, `Makefile`, `bin/`, `README.md`
  Depends on: none
  Done when:
  один source of truth для config/install path, исправлены launcher paths, install from source проходит по documented flow.
  Status note:
  install scripts, launchers, `README`, requirements source of truth и `tabula-server` baseline уже выровнены.

- [x] `T0-02` Normalize Python dependency contract and smoke bootstrap
  Lane: `A`
  Area: `scripts/`, Python dependency manifests, install docs
  Depends on: `T0-01`
  Done when:
  dev/install paths ставят один и тот же минимально достаточный dependency set, есть smoke-check `install -> boot -> connect -> stop`.
  Status note:
  dependency contract вынесен в `scripts/requirements-runtime.txt` и `scripts/requirements-dev.txt`; runtime smoke harness выровнен с `tabula serve` и детерминированно ждет boot-spawned driver по `/sessions`, так что documented bootstrap path теперь проверяется отдельным smoke-контуром.

- [x] `T0-03` Make `gateway-api` reliably executable
  Lane: `B`
  Area: `skills/gateway-api/run.py`, `bin/tabula-api*`
  Depends on: none
  Done when:
  устранены import/runtime errors, gateway поднимается и отвечает на базовый request path без ручных правок окружения.
  Status note:
  добавлены missing protocol constants, robust `TABULA_HOME` resolution, `.env` loading, absolute driver command и unit tests.

- [x] `T0-04` Move API gateway to concurrency-safe runtime
  Lane: `B`
  Area: `skills/gateway-api/run.py`
  Depends on: `T0-03`
  Done when:
  gateway использует threaded/concurrency-safe server model, SSE path не блокирует весь server, session state защищен от очевидных гонок.
  Status note:
  `gateway-api` переведен на `ThreadingHTTPServer`, создание session больше не держит глобальный map-lock во время kernel/driver handshake, concurrent creation одной session идет single-flight, а turn-операции одной session сериализованы вокруг общего driver/event queue.

- [x] `T0-05` Normalize process record model
  Lane: `C`
  Area: `internal/kernel/process_manager.go`, `internal/kernel/process_supervisor.go`, `internal/kernel/kernel.go`
  Depends on: none
  Done when:
  `SpawnedProcess` имеет явный record-level contract для `pid`, `handle`, `command`, `session`, `alive`, без неявной зависимости от `exec.Cmd.Process`.
  Status note:
  `SpawnedProcess` теперь хранит явный `PID`, а supervisor заполняет его независимо от локального `Cmd.Process`.

- [x] `T0-06` Rebuild safe `/sessions` snapshot path
  Lane: `C`
  Area: `internal/kernel/snapshot.go`, `skills/sessions/run.py`
  Depends on: `T0-05`
  Done when:
  snapshot безопасно работает для boot-spawned и tool-spawned процессов, `/sessions` больше не опирается на хрупкий локальный `Cmd.Process`.
  Status note:
  `SnapshotSessions()` переведен на recorded PID, добавлен focused test.

- [x] `T0-07` Introduce strict session lifecycle
  Lane: `C`
  Area: `internal/kernel/session.go`, `skills/gateway-api/run.py`, `skills/gateway-telegram/run.py`
  Depends on: `T0-05`
  Done when:
  есть TTL/idle cleanup/eviction policy, session-owned drivers корректно завершаются, долгоживущие session states не висят бессрочно.
  Status note:
  kernel session state теперь отслеживает lifecycle/activity, `gateway-api` и Telegram используют TTL/idle cleanup/eviction, single-flight creation, explicit `close(reason)` и корректно закрывают replaced/shutdown session-owned drivers; regression tests покрывают cleanup, replacement и shutdown paths.

- [x] `T0-08` Add session turn serialization and cancel semantics
  Lane: `C`
  Area: `internal/kernel/message_router.go`, gateway session state, cancel handling
  Depends on: `T0-04`, `T0-07`
  Done when:
  одна session не исполняет несколько конфликтующих turn-операций одновременно, `cancel` работает предсказуемо и не оставляет зависшие inflight paths.
  Status note:
  kernel session model теперь хранит `last_active`, inflight turn и `cancel_requested`; root-level turns сериализуются в `message_router`, повторный root message во время inflight turn отклоняется как `session busy`, turn state сбрасывается на `done`, `error`, process crash и disconnect turn-capable client, а gateway session states используют согласованный inflight/cancel contract с focused regressions на busy/cancel/reset paths.

- [x] `T0-09` Align hook contract with runtime behavior
  Lane: `D`
  Area: `internal/kernel/hook_engine.go`, `internal/kernel/policy.go`, `internal/kernel/tool_service.go`
  Depends on: none
  Done when:
  `before_tool_call` либо реально поддерживает modified payload end-to-end, либо контракт упрощен и это отражено в коде и тестах.
  Status note:
  modified payload в `before_tool_call` теперь реально применяется к effective tool input; добавлен focused regression test.

- [x] `T0-10` Separate policy path from telemetry path
  Lane: `D`
  Area: `skills/observer/run.py`, `internal/kernel/hooks.go`, observability hooks
  Depends on: `T0-09`
  Done when:
  observer не участвует в security/modifying critical path, telemetry живет отдельно от policy enforcement.
  Status note:
  observer теперь подписывается только на observability hooks (`after_message`, `after_tool_call`, `session_end`, `after_spawn`), не отправляет `hook_result`, а session/process telemetry добирает через безопасный `/sessions` snapshot polling.

- [x] `T0-11` Split test harness into unit/smoke/e2e layers
  Lane: `E`
  Area: `tests/`, `cmd/tabula/*_test.go`, test docs/scripts
  Depends on: none
  Done when:
  тесты разделены на четкие категории, можно отдельно запускать fast unit, runtime smoke и heavier e2e.
  Status note:
  добавлены `pytest` markers, `Makefile` targets, `scripts/test-go.sh`, `scripts/test-python.sh`, `tests/README.md`.

- [x] `T0-12` Add regression suites for runtime, lifecycle and concurrency
  Lane: `E`
  Area: `tests/`, `internal/kernel/*_test.go`
  Depends on: `T0-02`, `T0-04`, `T0-06`, `T0-08`, `T0-10`
  Done when:
  есть регрессии на install/runtime path, session lifecycle, gateway concurrency, hook/tool contract.
  Status note:
  regression matrix теперь закрывает bootstrap/runtime smoke, live kernel busy/reset path, kernel busy/cancel reset, gateway API lifecycle cleanup/replacement/shutdown/cancel, Telegram lifecycle/cancel regression, safe snapshot path, before_tool_call contract и observer telemetry separation через `tests/runtime_harness.py`, `tests/test_runtime_smoke.py`, `tests/test_gateway_api.py`, `tests/test_telegram_gateway.py`, `tests/test_observer.py`, `internal/kernel/snapshot_test.go`, `internal/kernel/tool_hook_test.go` и смежные hook tests.

## Wave 1. Durable control plane

- [ ] `T1-01` Define canonical entity/state schema
  Lane: `F`
  Area: новый state/control-plane слой, docs
  Depends on: `T0-05`, `T0-07`
  Done when:
  зафиксированы canonical сущности `Session`, `Turn`, `Task`, `Agent`, `Artifact`, `Approval`, `Project` и их минимальные поля.

- [ ] `T1-02` Add local durable store and migrations
  Lane: `F`
  Area: новый state package, storage bootstrap
  Depends on: `T1-01`
  Done when:
  есть локальный `SQLite` store, миграции, bootstrap path и read/write abstraction.

- [ ] `T1-03` Add event log and projector skeleton
  Lane: `F`
  Area: state/event packages
  Depends on: `T1-02`
  Done when:
  события пишутся в event log, materialized state строится через projectors, а не через ad hoc in-memory aggregation.

- [ ] `T1-04` Persist session/process/subagent records
  Lane: `F`
  Area: session/process/subagent runtime + state layer
  Depends on: `T1-02`, `T1-03`
  Done when:
  состояние session/process/subagent переживает process restart и может быть прочитано без опоры на живые in-memory maps.

- [ ] `T1-05` Implement control-plane operations `resume/fork/replay/export`
  Lane: `F`
  Area: control-plane API, session service, gateways
  Depends on: `T1-03`, `T1-04`
  Done when:
  эти операции существуют как platform-level API, а не как обходные реализации в отдельных gateways.

- [ ] `T1-06` Add bounded queues, backpressure and graceful restart
  Lane: `F`
  Area: gateway runtime, kernel queues, lifecycle management
  Depends on: `T1-02`
  Done when:
  gateway/API paths имеют лимиты очередей, понятное поведение под нагрузкой и controlled restart semantics.

- [ ] `T1-07` Introduce explicit ownership fields
  Lane: `F`
  Area: persisted record schemas
  Depends on: `T1-01`
  Done when:
  в ключевых record types есть `node_id`, `session_owner`, `process_owner`, `task_owner`, даже если пока они всегда локальны.

- [ ] `T1-08` Rebuild snapshot/introspection from persisted state
  Lane: `F`
  Area: snapshot/introspection layer
  Depends on: `T1-03`, `T1-04`, `T1-07`
  Done when:
  introspection показывает canonical state, а не случайный local-memory snapshot текущего узла.

## Wave 2. Typed tools и permissions

- [ ] `T2-01` Introduce typed tool descriptor and schema registry
  Lane: `G`
  Area: tool metadata, boot/discovery, tool execution contract
  Depends on: `T1-01`
  Done when:
  tool имеет typed input contract, optional output contract и единый validation path.

- [ ] `T2-02` Build central Tool Orchestrator
  Lane: `G`
  Area: `internal/kernel/tools*`, orchestration layer
  Depends on: `T2-01`
  Done when:
  approval, sandbox choice, retries, escalation и telemetry decisions проходят через один orchestrator layer.

- [ ] `T2-03` Introduce first-class permission object model and store
  Lane: `G`
  Area: permission service, persisted approvals, policy layer
  Depends on: `T1-02`, `T1-03`
  Done when:
  permissions представлены как явные objects/events, а не только как hook intervention.

- [ ] `T2-04` Add session-aware approvals and gateway-agnostic approval UX
  Lane: `G`
  Area: CLI/API/Telegram approval flow
  Depends on: `T2-02`, `T2-03`
  Done when:
  одна и та же approval model работает для всех gateways и знает про session/task context.

- [ ] `T2-05` Add layered config/policy contract
  Lane: `G`
  Area: config loading, policy rules, docs
  Depends on: `T2-03`
  Done when:
  есть явный config/policy layer с ограничениями по sandbox, network, tool use и ownership of approval decisions.

- [ ] `T2-06` Add routable correlation IDs for hooks, approvals and cancel
  Lane: `G`
  Area: hook/approval/cancel envelopes
  Depends on: `T1-07`, `T2-03`
  Done when:
  request/response correlation является явной моделью, пригодной и для local runtime, и для future distributed control plane.

## Wave 3. Extensibility и operator surfaces

- [ ] `T3-01` Define skill manifest format separate from `SKILL.md`
  Lane: `H`
  Area: skill metadata format, boot/discovery
  Depends on: `T2-01`
  Done when:
  декларативный manifest отделен от narrative docs и содержит install/discovery/compatibility contract.

- [ ] `T3-02` Build capability registry and boot validation
  Lane: `H`
  Area: `boot.py`, skill registry/validation
  Depends on: `T3-01`
  Done when:
  discovery валидируется до запуска runtime, capability declarations собираются в единый registry.

- [ ] `T3-03` Freeze public SDK boundary and add contract tests
  Lane: `H`
  Area: `skills/lib/`, public extension seams, tests/docs
  Depends on: `T3-01`
  Done when:
  зафиксированы public seams для skills/extensions, есть contract tests на manifest loading и capability registration.

- [ ] `T3-04` Implement operator commands
  Lane: `H`
  Area: CLI/operator surfaces
  Depends on: `T1-08`, `T3-02`
  Done when:
  есть `doctor`, `setup`, `health`, `sessions`, `inspect` с полезной диагностикой по install/runtime/state.

- [ ] `T3-05` Add health/readiness/audit surfaces
  Lane: `H`
  Area: health endpoints/commands, audit output
  Depends on: `T3-02`, `T3-04`
  Done when:
  система умеет явно показывать readiness, install drift, broken entrypoints, session/process health.

## Wave 4. Task platform и future-readiness

- [ ] `T4-01` Introduce first-class Task entity and lifecycle
  Lane: `I`
  Area: state/task packages
  Depends on: `T1-01`, `T1-03`
  Done when:
  task имеет `id`, lifecycle, progress, artifact links, timeout and cleanup policy.

- [ ] `T4-02` Link tasks with sessions, subagents, processes and artifacts
  Lane: `I`
  Area: task/session/subagent integration
  Depends on: `T4-01`, `T1-04`
  Done when:
  long-running and detached work учитывается как task graph, а не как набор разрозненных runtime objects.

- [ ] `T4-03` Add live agent registry and child-session persistence
  Lane: `I`
  Area: subagent runtime, registry/state layer
  Depends on: `T1-04`
  Done when:
  live agents и child sessions адресуемы, наблюдаемы и переживают runtime restarts на уровне control plane.

- [ ] `T4-04` Add multi-agent guardrails
  Lane: `I`
  Area: subagent policy/runtime
  Depends on: `T4-01`, `T4-03`, `T2-05`
  Done when:
  есть `max_depth`, `max_threads`, ownership, live registry и final-state reconciliation semantics.

- [ ] `T4-05` Integrate snapshot/diff/revert into session architecture
  Lane: `I`
  Area: snapshot/session/project state
  Depends on: `T1-08`, `T4-02`
  Done when:
  snapshot/diff/revert живут рядом с session/task history и не являются отдельной случайной функцией.

- [ ] `T4-06` Introduce formal project/workspace abstraction
  Lane: `I`
  Area: project/workspace model, config/session linkage
  Depends on: `T1-01`
  Done when:
  formalized `project root`, `session owner`, `artifact root`, `replay/export target`, workspace boundary.

- [ ] `T4-07` Prepare local/remote executor abstraction without leaving multiprocess model
  Lane: `I`
  Area: process launcher/executor abstraction
  Depends on: `T1-07`, `T4-02`
  Done when:
  execution backend можно мыслить как local or remote, но orchestration/policy ownership остается за `Tabula`.

## Suggested issue order

Если нужен короткий practical start order, то он такой:

1. `T0-01`
2. `T0-03`
3. `T0-05`
4. `T0-09`
5. `T0-11`
6. `T0-04`
7. `T0-06`
8. `T0-07`
9. `T0-08`
10. `T0-10`
11. `T0-12`
12. `T1-01`
13. `T1-02`
14. `T1-03`
15. `T1-04`

Это и есть первый реальный delivery slice, после которого у `Tabula` появится надежный фундамент для следующих волн.
