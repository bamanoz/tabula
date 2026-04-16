# Tabula: дистиллят сравнений и backlog реализации

Дата: 2026-04-16

Источники:

- `ANALYSIS.md`
- `CLAUDECODE_VS_TABULA.md`
- `CODEX_VS_TABULA.md`
- `OPENCODE_VS_TABULA.md`
- `OPENCLAW_VS_TABULA.md`
- `DECEPTICON_VS_TABULA.md`
- `DEEPAGENTS_VS_TABULA.md`

## Главный вывод

Почти все сравнения сходятся в одном и том же:

- `Tabula` уже сильна как platform core;
- конкуренты выигрывают не "идеей агента", а зрелостью системных слоев;
- самый большой разрыв у `Tabula` сейчас в `control plane`, `durability`, `permission/tool orchestration`, `operator tooling` и `task/session lifecycle`;
- при этом главная ценность `Tabula` не в том, чтобы стать еще одним большим host runtime, а в том, чтобы остаться компактным `Go`-kernel с внешними изолированными skill-процессами.

Иными словами:

- копировать нужно не breadth чужих продуктов;
- копировать нужно качество их системных контрактов.

## Главное преимущество, которое нельзя потерять

`Tabula` стоит развивать так, чтобы сохранить ее strongest edge:

- маленький `Go`-kernel как центр маршрутизации и lifecycle;
- внешние skills как реальные процессы, а не декоративные plugins;
- protocol-first orchestration;
- process isolation для skills и subagents;
- language-agnostic extensibility;
- hooks/policy как kernel primitive, а не как побочный слой UI.

Практическое правило:

Любая новая подсистема должна усиливать границу `kernel <-> external skills`, а не размывать ее.

## Что нужно заимствовать у рынка

Это повторяется почти во всех сравнительных документах и выглядит как реальный must-have backlog:

1. Canonical control plane вокруг `session/turn/task/agent/artifact`.
2. Durable session core с `SQLite` или аналогичным локальным store.
3. Event log + materialized state/projectors.
4. Центральный слой tool orchestration:
   approvals, sandbox selection, retries, escalation, telemetry.
5. First-class permission subsystem:
   `allow/deny/ask`, persisted approvals, session-aware requests.
6. Typed tool contracts:
   schema input/output, validation, activity/progress semantics.
7. Manifest-first metadata для skills/hooks/gateways/drivers.
8. Operator shell:
   `doctor`, `health`, `setup`, `session inspection`, readiness checks.
9. First-class task layer поверх sessions и subagents.
10. Guardrails для multi-agent:
    depth limits, thread limits, ownership, final-state handling.
11. Snapshot/diff/revert как часть session architecture.
12. Project/workspace abstraction.
13. Explicit ownership model:
    `node_id`, `session_owner`, `process_owner`, `task_owner`.
14. Подготовка к future multi-node через durable control plane, а не через ранний distributed rewrite.

## Что нельзя копировать вслепую

Эти anti-goals тоже повторяются почти во всех сравнениях:

- не превращать `Tabula` в монолитный in-process host runtime;
- не плодить несколько конкурирующих extension systems;
- не наращивать desktop/web/IDE/remote surface до стабилизации ядра;
- не держать две параллельные policy systems без явного владельца;
- не подменять kernel чужим orchestration stack;
- не жертвовать process isolation ради быстрого product breadth.

## Приоритетная формула

Правильный вектор для `Tabula`:

1. Сначала hardening текущих контрактов.
2. Затем durable control plane.
3. Затем typed tool/permission layer.
4. Затем manifest/operator/task surfaces.
5. И только после этого новые крупные продуктовые поверхности.

## Backlog к реализации

### P0. Stabilization и hardening текущего ядра

Это не "техдолг на потом", а обязательный входной билет ко всем следующим фичам.

- `P0.1` Починить install/dev/runtime contract.
  Включает:
  единый источник конфигурации, исправление install scripts, `Makefile`, launcher paths, единый Python dependency contract, smoke-путь `install -> boot -> connect -> gateway -> stop`.
- `P0.2` Довести `gateway-api` до гарантированно исполнимого состояния.
  Включает:
  исправление import/runtime ошибок, threaded server, надежный SSE path, корректную инициализацию session state.
- `P0.3` Нормализовать bookkeeping процессов и сессий.
  Включает:
  явный `PID/handle` в `SpawnedProcess`, безопасный `/sessions`, корректный snapshot path, одинаковую модель для boot-spawned и tool-spawned процессов.
- `P0.4` Ввести строгий session lifecycle.
  Включает:
  session-level turn serialization, idle cleanup, TTL, eviction, cancellation semantics, driver cleanup.
- `P0.5` Привести hooks contract в соответствие runtime behavior.
  Включает:
  либо полную поддержку modified payload в `before_tool_call`, либо упрощение контракта; вынос observer из security path; разделение policy и telemetry hooks.
- `P0.6` Пересобрать тестовый контур вокруг реальных контрактов.
  Включает:
  разделение unit и e2e, smoke tests для install/runtime path, lifecycle tests для sessions/processes/gateways, concurrency tests для gateway-слоя.

### P1. Durable control plane и session backbone

Это главный инфраструктурный слой, который чаще всего выигрывает у `Tabula` во всех сравнениях.

- `P1.1` Ввести canonical сущности:
  `Session`, `Turn`, `Task`, `Agent`, `Artifact`, `Approval`, `Project`.
- `P1.2` Добавить локальный durable store.
  Предпочтительно:
  `SQLite` с event log и индексированным state layer.
- `P1.3` Добавить projector/materialized-state модель.
  Нужна для:
  session summary, task state, live agents, approvals, session health, artifacts.
- `P1.4` Сделать child sessions и subagent state persisted-сущностями ядра.
- `P1.5` Реализовать `resume`, `fork`, `replay`, `export` как control-plane операции, а не ad hoc функции отдельных gateways.
- `P1.6` Добавить bounded queues, backpressure и graceful restart для gateway/API paths.
- `P1.7` Ввести session busy/idle/running/cancelled/failed/completed state model.
- `P1.8` Ввести explicit ownership fields в persisted records.
  Минимум:
  `node_id`, `session_owner`, `process_owner`, `task_owner`.

### P2. Typed tools и permission orchestration

Этот слой нужен, чтобы governance перестала быть "хорошей идеей на бумаге" и стала реальным runtime-контрактом.

- `P2.1` Ввести typed tool descriptor.
  Включает:
  input schema, optional output schema, validation model, activity description, progress semantics.
- `P2.2` Собрать центральный `Tool Orchestrator`.
  Он должен владеть:
  approval flow, sandbox selection, retry policy, escalation path, network rules, telemetry decisions.
- `P2.3` Ввести first-class permission service.
  Базовая модель:
  `allow / deny / ask` как явные outcomes.
- `P2.4` Сделать approvals persisted и session-aware.
- `P2.5` Ввести более узкие permission object types.
  Например:
  `tool`, `exec`, `network`, `external_directory`, `workspace_write`.
- `P2.6` Сделать approval UX gateway-agnostic.
  То есть одна permission model должна работать одинаково для CLI, API, Telegram и будущих клиентов.
- `P2.7` Ввести layered config/policy contract с явными ограничениями по sandbox, network и tool use.

### P3. Manifest-first extensibility и operator surfaces

Это нужно делать не вместо skills, а для усиления уже выбранной skill-модели.

- `P3.1` Ввести декларативный manifest для skills.
  Он должен быть отделен от narrative-документации в `SKILL.md`.
- `P3.2` Формализовать capability declarations.
  Skill должен заранее объявлять:
  tools, hooks, gateway roles, driver roles, daemon roles, dependencies, compatibility.
- `P3.3` Добавить trust/health/version/install metadata.
- `P3.4` Собрать registry + validation на boot.
  Discovery должно валидироваться до запуска runtime.
- `P3.5` Рано зафиксировать public SDK boundary.
  Нужно явно отделить:
  public seams для skills/extensions и private internals ядра.
- `P3.6` Добавить contract tests для manifest loading, capability registration и install/discovery drift.
- `P3.7` Реализовать operator-команды:
  `tabula doctor`, `tabula setup`, `tabula health`, `tabula sessions`, `tabula inspect`.
- `P3.8` Добавить health/readiness/audit surfaces.

### P4. Task platform, multi-agent guardrails и session tooling

Это следующий шаг после control plane, а не до него.

- `P4.1` Ввести task как first-class сущность.
  У задачи должны быть:
  `id`, `lifecycle`, `progress`, `artifacts`, `notification`, `timeout`, `cleanup policy`.
- `P4.2` Связать tasks с session/subagent/process state.
- `P4.3` Добавить registry живых агентов и child sessions.
- `P4.4` Ввести guardrails для multi-agent.
  Минимум:
  `max_depth`, `max_threads`, ownership, live registry, final-state reconciliation.
- `P4.5` Сделать snapshot/diff/revert частью session architecture.
- `P4.6` Ввести formal project/workspace abstraction.
  Минимум:
  `project root`, `session owner`, `child session`, `artifact root`, `replay/export target`.
- `P4.7` Строить background jobs, cron и detached work только поверх task layer, а не отдельными локальными обходами.
- `P4.8` Подготовить distributed-readiness without building multi-node now.
  Включает:
  явные owner semantics, routable correlation IDs для hooks/approvals/cancel, process records отдельно от локального PID, abstraction для local/remote executor.

### P5. Опциональные усилители после стабилизации

Это не must-have для следующего цикла, но хорошие направления после закрытия слоев выше.

- `P5.1` Экспериментальный `deepagents-runner` как bounded specialist skill для code-heavy delegation.
- `P5.2` Persistent interactive sandbox sessions для тяжелых execution workflows.
- `P5.3` Более богатый session UX:
  inspection, grouped sessions, richer streaming event surfaces.
- `P5.4` Headless automation и non-interactive runs поверх уже существующего task/control-plane слоя.

## Как переупорядочить текущий ROADMAP

Текущий `ROADMAP.md` полезен как список feature ideas, но его стоит читать через новую последовательность.

### Что нужно подтянуть вверх

- permission system;
- session resume/fork;
- file undo/revert;
- headless/non-interactive mode;
- compaction hooks и auto-compaction;
- более строгий install/config/discovery contract.

### Что нужно опустить ниже

- новые gateways ради самих gateways;
- desktop app;
- browser automation;
- skill marketplace;
- voice surfaces;
- широкий remote/product surface.

Причина простая:

пока у `Tabula` не собран durable control plane и не стабилизирован lifecycle, новые поверхности будут только размножать хрупкость.

## Короткий итог

Если свести все к одной практической формуле, то следующий этап для `Tabula` такой:

- не становиться копией `Codex`, `OpenCode`, `OpenClaw` или `Claude Code`;
- сохранить microkernel + external skills + process isolation;
- быстро добрать control plane, durability, permissions, manifests и operator tooling;
- только потом расширять product surface.

То есть лучшая следующая версия `Tabula` - это не "более широкий продукт", а "гораздо более надежная и контрактно строгая версия самой себя".
