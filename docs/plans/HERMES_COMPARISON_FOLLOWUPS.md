# Hermes Comparison — Follow-ups

Дата: 2026-05-02
Status: actionable backlog
Источник: ревью архитектуры Tabula в сравнении с Hermes Agent
(NousResearch/hermes-agent), включая отдельный анализ observability через
границу процессов.

Документ — **bridge между ревью и FOLLOWUP_TASKS.md**. Содержит только те
пункты, которые вытекли из сравнения и не были перечислены в существующих
планах. Каждый пункт самостоятелен — можно брать в любой последовательности
внутри своего приоритета.

Отдельные направления, которые **уже** покрыты другими планами и здесь не
дублируются:

- session durability/resume/fork — `COMPETITIVE_LESSONS.md §3`;
- ToolOrchestrator — `COMPETITIVE_LESSONS.md §6`;
- operator tooling (`tabula doctor`, `inspect`) — `COMPETITIVE_LESSONS.md §7`;
- tasks как first-class — `COMPETITIVE_LESSONS.md §11`;
- subagent migration в plugin — `ARCHITECTURE.md §415-418` + `SKILL_PLUGIN_ARCHITECTURE.md §4.5`.

---

## P0

### P0.1 Симметрия SDK: Node plugin SDK или сужение `runtime`

**Проблема.** `internal/kernel/plugin/manifest.go:19-22` декларирует
`runtime ∈ {python, node}` валидным для plugin manifest. TypeScript SDK
существует только для **skills** (`@tabula/skill-sdk` в `tabula-bundles/_lib/typescript/`,
используется TUI `coder`). Node-аналога `tabula_plugin_sdk` для long-lived
плагинов с `register/tool_call/event/event_reply` поверх stdio NDJSON нет.

Это значит: «multi-language plugins» — частично декларация, ловушка для
авторов, аргумент против Tabula в сравнении с любым другим агентом.

**Как.** Один из двух путей:

- **A. Сделать.** Пакет `@tabula/plugin-sdk` рядом с `@tabula/skill-sdk`.
  Перенести один существующий Python plugin (например `hook-logger` —
  observability-only, минимальная поверхность) на Node как доказательство.
  Запустить против него тот же contract test suite (см. P1.1).
- **B. Сузить.** `validRuntimes = {"python"}` в `manifest.go:19`. В
  `docs/PLUGIN_AUTHORING.md` явно сказать «plugin runtime is currently
  Python; skills can be any language». Снять контракт, который не
  выполняется.

**Стоимость.** A — 4–6 дней. B — 1 час.

**Решение по умолчанию.** B сейчас, A — когда появится конкретная
потребность во втором runtime. Не держать обещание, которое не
обеспечено.

---

## P1

### P1.1 SDK contract test suite поверх tabula-testbed

**Проблема.** `tools/tabula-testbed` уже есть с уровнями `lint/direct/full` и
`TestbedClient`, но он проверяет **конкретные бандлы**, не сам контракт SDK.
Без фиксированной батареи сценариев:

- вторую SDK невозможно написать корректно (непонятно, что должна
  воспроизвести);
- регрессии протокольного поведения легко проползают;
- PR-ы в SDK нечем валидировать кроме существующих интеграционных тестов
  бандлов.

**Как.** Новый раздел `tools/tabula-testbed/contract/` с фикстур-плагинами
(каждый — минимальный, под один сценарий):

- empty-plugin (только handshake, никаких tools/hooks);
- tool-only (один tool, корректный reply);
- hook-only (одна subscription, void/modifying варианты);
- slow-tool (deadline expiry);
- malformed-line recovery (3-в-10s threshold);
- update_tools mid-flight;
- graceful-shutdown-with-children (PG cleanup);
- infinite-timeout-disconnect (как в `hooks_test.go`);
- log-with-fields (для P1.4);
- crash-with-traceback (для P1.4).

CLI: `tabula-testbed contract --plugin <path>` → pass/fail. Каждый сценарий
параметризован путём к плагину; одна и та же команда работает против
Python и (когда появится) Node SDK.

**Стоимость.** 2–3 дня поверх существующего testbed runner’а.

### P1.2 Зафиксировать ABI обоих протоколов в `docs/PROTOCOL.md`

**Проблема.** Сейчас контракты разнесены:

- `internal/kernel/protocol.go` (WebSocket константы);
- `internal/kernel/plugin/protocol.go` (NDJSON константы);
- текстовые описания в `docs/ARCHITECTURE.md`;
- creative-документы (`memory-bank/creative/*`), которые не в этом репо.

Для людей, пишущих свою SDK, это блокер. Для Hermes-сообщества это самая
частая жалоба — не повторять.

**Как.** Один документ `docs/PROTOCOL.md` с разделами:

1. **WebSocket v1** (kernel ↔ client): таблица сообщений, обязательные поля,
   error envelope, capability negotiation, политика unknown-fields,
   версионирование.
2. **Plugin stdio v1** (kernel ↔ plugin): NDJSON framing, `MaxLineSize`,
   методы, `callId` correlation, malformed-line recovery, register
   handshake, restart contract.
3. **Observability contract** (закрывает дыру из P1.4): что плагин кладёт в
   `Fields`, какие keys зарезервированы (`metric_name`, `metric_value`,
   `metric_kind`, `traceback`, `trace_id`), что ядро гарантирует
   прокинуть в slog.
4. **Versioning policy**: bump rules, optional fields, fallback semantics.

Сослаться из обоих `protocol.go` файлов и из `README.md`.

**Стоимость.** Полдня. Самый дешёвый высокоэффективный шаг.

### P1.3 Прибрать мёртвые контракты в core

**Проблема.** AGENTS.md прямо запрещает back-compat shims без миграции,
но в коде есть несколько таких следов. Это шум, мешающий читать
архитектуру и осваивать её новым контрибьюторам.

Конкретные точки:

- `docs/ARCHITECTURE.md:369` — упоминает `before_spawn`/`after_spawn` как
  «reserved legacy». В `internal/kernel/hooks.go:44 HookEvents` их нет.
  Удалить упоминание из доки.
- `internal/kernel/plugin/manifest.go:73-75` — `Manifest.Config` помечен
  legacy, runtime config переехал в `config/global.toml` и
  `config/plugins/<id>/config.toml`. Удалить поле и
  `BootEntry.Config` (`manifest.go:104`).
- `examples/plugin-hello/plugin.toml:22` — содержит `[config.defaults]`,
  `manifest.go:149` молча игнорирует. Reference-плагин противоречит
  правилу AGENTS.md «не добавлять новые runtime-config блоки в
  plugin.toml». Удалить блок из примера, либо поднять явный warning
  при загрузке.
- `internal/kernel/policy.go:27-32 CanConnect(token)` — параметр существует
  только чтобы вернуть ошибку при непустом токене (legacy spawn-token).
  Сузить сигнатуру, убрать параметр.
- `internal/kernel/tool_service.go:160 parseCommandToolInput` — `nolint:unused`
  с комментарием «Phase 1 D1.11(b) dead-code-keep policy». Если фаза давно
  прошла — вычистить; если нужно — задокументировать в файле, что именно
  она ждёт, и завести issue.
- `internal/kernel/protocol.go:38 PluginProtocolVersion` живёт в `kernel`
  пакете, в то время как все остальные plugin-протокольные константы — в
  `internal/kernel/plugin/protocol.go`. Декларировано как обход import
  cycle, но это readability wart. Решается re-export константой в `plugin/`
  пакете, либо вынесением `PluginProtocolVersion` в `plugin/` и переменной
  в `kernel/` ссылкой.

**Стоимость.** 1 день суммарно, включая миграцию вызовов и тесты.

### P1.4 Observability across process boundary

**Проблема.** Граница процессов — заявленная сила Tabula, но observability
через неё сейчас слабее, чем у in-process Hermes:

1. **`MethodLog` теряет `Fields` в ядре** (`plugin_tools.go:188-204`):
   ```go
   h.Logger.LogAttrs(context.Background(), level, params.Msg,
       slog.String("plugin", handle.ID()))
   ```
   `params.Fields` отбрасывается. Документированная конвенция
   `metric_name/metric_value/metric_kind` (`plugin/protocol.go:155-157`,
   используется в `examples/plugin-hello/run.py:27-33`) **не работает**:
   reference-плагин использует фичу, в kernel её нет.

2. **stderr — multiline tracebacks теряют структуру** (`plugin/runtime.go:279`).
   Python traceback из 8 строк → 8 отдельных записей `Info`, размазанных в
   логе. Никакой связи с последующим `OnExit` в `handlePluginExit`
   (`plugin_runtime.go:105`). В snapshot’е `LastError` — `"exit status 1"`,
   бесполезно.

3. **Нет turn/trace ID.** `callId` — локальная корреляция request/reply.
   Нет идентификатора, который связал бы `before_message → before_tool_call →
   tool_call → tool_result → after_tool_call → after_message`. `Session` —
   слишком грубая гранулярность (десятки turn’ов внутри).

4. **Нет SLA-метрик хуков в snapshot.** Когда плагин просрочил
   `before_tool_call` — `dispatchHook` отрабатывает по fail-open/fail-closed
   стратегии, но факт нарушения никуда не пишется. В
   `snapshot.go:27-36 snapshotPluginInfo` нет `timeouts_count` или
   `last_timeout_at`.

5. **`/health` отдаёт только `ProtocolVersion`** (WebSocket),
   `PluginProtocolVersion` снаружи невидим.

Цена ошибки: вы платите за процессную изоляцию (медленнее, сложнее), а
observability-преимущества границы не получаете. Hermes хвастается OTel,
но это лёгкая задача в одном процессе. У вас принципиально дороже —
именно поэтому надо хотя бы базовую часть закрыть.

**Как (в порядке снижающейся отдачи):**

1. **Чинить `logPluginMessage` (`plugin_tools.go:188`).** Прокидывать
   `params.Fields` в `slog.Attr`, разворачивая map в attrs. ~30 минут.
   Закрывает (1), активирует уже работающий `metric_*` контракт.

2. **`stderr_tail` в `LastError`.** В `forwardStderr` (`runtime.go:279`)
   держать ring-buffer на N=64 строки. На `OnExit` с err != nil —
   записать в `state.LastError` структурированно. Сейчас `LastError` —
   `string` (`snapshot.go:32`), сделать его объектом с полями
   `{exit, stderr_tail[], pid}` или добавить `LastStderrTail []string`.
   Закрывает (2). 0.5 дня.

3. **Hook timeout метрики.** В `HookEngine` инкрементировать счётчик
   per-plugin per-event на каждый timeout. Добавить в
   `snapshotPluginInfo` поля `timeouts: {event_name: count, ...}` и
   `last_timeout_at`. Дёшево, мощно для диагностики. 0.5 дня.

4. **SDK перехват unhandled exceptions.** `_handle_tool_call`
   (`examples/plugin-sdk-python/src/tabula_plugin_sdk/api.py:140`) сейчас
   превращает исключение в `tool_result.error=str(exc)` без traceback.
   До записи tool_result отправлять один `log` с `level=error` и
   `Fields={traceback: "...", traceback_lines: [...]}`. Не требует
   изменения протокола, чисто SDK-side. 0.5 дня.

5. **`turn_id`/`trace_id` в session lifecycle.** Генерировать при
   `BeginTurn` (`session.go`), включать как поле в:
   - `Message` envelope (новое optional поле `trace_id` рядом с `id`);
   - `EventParams` и `ToolCallParams` (`plugin/protocol.go`) — добавить
     `trace_id` (back-compat optional);
   - все `slog.Info/Warn` calls в kernel — приклеивать через
     `Logger.With("trace_id", ...)` через context.

   Контрактное изменение протокола, но **не требует bump
   `PluginProtocolVersion`** если поле optional. 2 дня. Закрывает (3).

6. **`/health` показывает обе версии.** Добавить
   `plugin_protocol_version` рядом с `protocol_version`. 15 минут.
   Закрывает (5).

7. **(Опционально, отдельный цикл.)** OTel/OTLP. Провод понятен: ядро
   берёт `OTEL_EXPORTER_OTLP_ENDPOINT`, root-span на turn, передаёт W3C
   trace context (`traceparent`) в каждый `tool_call/event` через params,
   SDK создаёт child span. **Не делать сейчас** — поле `trace_id` из
   шага 5 достаточно для будущей интеграции.

**Стоимость.** Шаги 1+6 — час каждый. Шаги 2+3+4 — день каждый. Шаг 5 —
2 дня. Итого ~5 дней работы, сильно повышающие диагностируемость
production-проблем.

---

## P2

### P2.1 Завершить миграцию subagent в плагин

**Проблема.** `docs/ARCHITECTURE.md:415-418` явно фиксирует, что
`MaxSpawnDepth/MaxChildren/auth` должны жить в subagent plugin, не в
kernel. До завершения миграции «маленький kernel» больше декларация,
чем факт. Это же выравнивает plugin-границу для будущих subagent-альтернатив
(DeepAgents-style bounded specialist и пр.).

**Как.**

1. Перенести оставшуюся kernel-bridge логику в `drivers/subagent` plugin
   (`tabula-bundles`).
2. Добавить тесты depth/child-count/lifecycle/cleanup внешним плагином.
3. Удалить kernel-side enforcement.

Требует параллельной работы в `tabula-bundles` — координировать.

**Стоимость.** ~1 неделя.

### P2.2 Decouple kernel-side тесты от sibling-репо identities

**Проблема.** AGENTS.md запрещает distro-specifics в core-тестах, но
есть нарушения:

- `internal/kernel/plugin_gateway_distrib_test.go:52-53` — хардкод путей в
  `tabula-distrib`, ссылки на `gateway-telegram`.
- `internal/kernel/plugin_bundles_test.go` — путь в `tabula-bundles`,
  ссылки на конкретные бандлы (hooks, MCP, subagents).
- `examples/plugin-hello` — reference plugin живёт в этом репо и
  используется `internal/kernel/plugin_live_test.go:18,78`. По правилам
  должен быть в `tabula-bundles`.

Тесты gated на наличие sibling-репо (skip cleanly), но факт coupling’а
остаётся. Усложняет вынесение бандлов в отдельную организацию,
выпускной цикл и читабельность core-тестов.

**Как.** Два варианта по каждому случаю:

1. **Вынести тест в sibling-репо.** Если тест проверяет интеграцию с
   конкретным бандлом — он принадлежит бандлу.
2. **Generic-фикстура.** Если тест проверяет kernel-поведение, а бандл
   используется как «любой плагин с таким-то поведением» — заменить на
   inline-фикстуру (TempDir + минимальный generated `plugin.toml` + Python
   stub). `plugin_live_test.go` уже почти так делает, нужно довести.

`examples/plugin-hello` оставить как SDK-демо в README, но `plugin_live_test.go`
переписать на generated fixture, не зависимый от файла под `examples/`.

**Стоимость.** 2–3 дня.

### P2.3 Quality-of-life CLI: `tabula plugins`

**Проблема.** Hermes имеет интерактивный `hermes plugins` TUI для просмотра
и переключения. У вас snapshot отдаётся через
`/internal/snapshot/plugins`, но human-friendly CLI нет. Это не
архитектура, но повышает порог входа.

**Как.** `tabula plugins list/show/restart/logs` — тонкий CLI поверх
существующего `/internal/snapshot/plugins` + tail логов из
`$TABULA_HOME/logs/`. `restart` дёргает `Hub.ReloadPlugins` через signal
или через локальный HTTP endpoint (loopback-only).

**Стоимость.** 1–2 дня. Низкий приоритет — отложить пока не появится
явный запрос от пользователей.

---

## Anti-goals (что из Hermes сознательно НЕ берём)

Эти пункты упомянуты, чтобы не возвращаться к обсуждению:

- **In-process plugin host (Python ABCs).** Hermes использует
  ABC + orchestrator pattern (`ContextEngine`, `MemoryProvider`,
  `ProviderTransport`, `ImageGenProvider`) — single instance per slot,
  выбор через config. У нас **физическая** граница процессов, и это
  base bet. Не подмешивать в kernel «слоты под Memory/Provider» — это
  product-specific policy, AGENTS.md прямо запрещает.

- **Provider/General разделение в kernel.** Если distro нужен «один
  активный Memory» — это конвенция уровня distro composition (бандл
  ставит ровно один `memory-*` plugin). Kernel не должен знать про
  «slot».

- **`plugins.enabled` allow-list внутри ядра.** У нас allow-list — это
  состав distro, выраженный через `distro.toml`+`bundle.toml`. Лучше:
  declarative и воспроизводимо.

- **Lifecycle hooks в манифесте декларативно.** Сейчас плагины
  объявляют subscriptions в `register{}` reply (программно). Это
  **сильнее** манифестной декларации Hermes — плагин может
  динамически подписываться/отписываться. Не возвращаться к
  манифестному пути.

- **Переписывать что-либо на Python ради совместимости с Hermes.**
  Потеря главного преимущества. Kernel остаётся Go.

---

## Vector of development (короткий вывод)

Tabula — не «улучшенный Hermes». Это другая категория: kernel + package
manager для агентов, а не in-process plugin host. Соответственно:

- **Не имитировать** Hermes-ные ABC, plugin-loader-в-host, OTel-в-process.
- **Двигаться** в сторону интероперабельности на границах (читать
  чужие `SKILL.md`, чужие MCP-серверы через адаптер-плагины), не
  копировать чужие модели внутрь.
- **Развивать как USP**: distro/bundle pipeline (lock-файлы, источники,
  generations, `reload.touch` уже есть). Если довести до публичного
  registry — у вас появляется то, чего нет ни у кого.

Closing P0/P1 из этого документа — необходимое условие, чтобы USP не
протух за время, пока вы строите registry.
