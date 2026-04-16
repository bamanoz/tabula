# DeepAgents vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `deepagents`: `/Users/mak/src/deepagents`

Цель документа двойная:

1. Зафиксировать глубокое сравнение `deepagents` и `tabula`.
2. Определить, есть ли практические сценарии, в которых `tabula` могла бы интегрировать `deepagents` внутрь себя.

Анализ основан на чтении кода, структуры репозиториев, конфигурации пакетов, CLI/runtime слоев и тестовой инфраструктуры.

## Короткий вывод

`deepagents` и `tabula` решают близкие, но не одинаковые задачи.

- `deepagents` — это в первую очередь opinionated agent harness и продуктовая оболочка вокруг LangGraph/LangChain.
- `tabula` — это скорее модульная агентная платформа с собственным kernel/protocol/runtime, ближе к "agent microkernel" или "agent OS".

Поэтому правильная рамка сравнения не "что лучше вообще", а "на каком слое каждая система сильнее".

Если очень коротко:

- `deepagents` сильнее в готовом developer experience, продуктовой CLI-обвязке, middleware-архитектуре, чекпоинтинге, зрелости Python-экосистемы и интеграции с внешними sandbox/server workflows.
- `tabula` сильнее как собственная платформа оркестрации: у нее есть свой kernel, wire protocol, skill-bus, hooks, gateways, session routing и мультипроцессная модель.

Главный вывод по интеграции:

- `tabula` не стоит пытаться "заменить" на `deepagents`;
- зато `tabula` вполне может использовать `deepagents` как встроенный специализированный execution engine для отдельных классов задач.

Самый естественный путь: `tabula` как внешняя orchestration/control plane, `deepagents` как внутренний Python-powered worker для coding/research/sandbox-heavy задач.

## Что представляет собой каждый проект

## Tabula

`tabula` построена как собственная агентная система:

- Go-ядро в `internal/kernel/*` и `cmd/tabula/main.go`
- Python-skills в `skills/*`
- boot-слой в `boot.py`
- собственный WebSocket protocol между kernel и skills
- отдельные gateways: CLI, API, Telegram
- hooks, memory, MCP, sessions, subagents

Ключевая идея: ядро маршрутизирует сообщения и управляет процессами, а все capabilities живут в pluggable skills.

Это архитектура платформы.

## DeepAgents

`deepagents` — Python-monorepo из нескольких пакетов:

- `libs/deepagents/` — SDK/harness
- `libs/cli/` — terminal product поверх SDK
- `libs/acp/` — ACP adapter для editor-like integrations
- `libs/partners/*` — sandbox/provider integrations

Главная точка входа SDK — `create_deep_agent(...)` в `libs/deepagents/deepagents/graph.py`.

Ключевая идея: не строить свой kernel/protocol, а собирать "deep agent" как LangGraph graph c набором middleware:

- todo/planning
- filesystem
- shell
- skills
- memory
- subagents
- summarization
- HITL / permissions

Это архитектура harness/runtime library.

## Самое важное архитектурное различие

`tabula` и `deepagents` находятся на разных уровнях абстракции.

### Tabula

Мысленно это:

- kernel
- protocol
- process runtime
- session bus
- pluggable skills

Она ближе к "операционной системе" для агентных компонентов.

### DeepAgents

Мысленно это:

- готовый агентный граф
- слой middleware
- backend abstraction
- productized CLI/server shell вокруг LangGraph

Она ближе к "framework + batteries-included harness".

Отсюда вытекает почти все остальное:

- `tabula` удобнее, если нужен собственный протокол, много внешних клиентов, skill ecosystem и независимые процессы;
- `deepagents` удобнее, если нужен сильный готовый coding/research agent без написания своего runtime.

## Глубокое сравнение

## 1. Runtime-модель

### Tabula

Runtime `tabula` основан на собственном kernel:

- клиенты и skills общаются по WebSocket;
- каждая skill — отдельный процесс;
- subagent — тоже отдельный процесс;
- kernel ведет sessions, clients, hooks, tools, spawn lifecycle.

Плюсы:

- сильная изоляция компонентов;
- легко подключать heterogenous skills;
- естественная модель для gateways и фоновых служб;
- архитектура по-настоящему распределенная даже локально.

Минусы:

- своя инфраструктура сложнее в поддержке;
- нужно самостоятельно держать корректность protocol/process lifecycle;
- operational bugs бьют больнее, чем в framework-based runtime.

### DeepAgents

Runtime `deepagents` основан на LangGraph/LangChain:

- агент собирается как graph;
- возможности добавляются middleware-слоями;
- storage/execution делегируются backend-абстракциям;
- CLI в server mode поднимает LangGraph server и работает через remote client.

Плюсы:

- много готового runtime behavior приходит из экосистемы;
- проще строить кастомные agent configurations;
- checkpointing, remote graph, middleware composition уже системно поддержаны.

Минусы:

- тяжелая зависимость от внешней экосистемы;
- меньше низкоуровневого контроля, чем у собственного kernel;
- при сложных integration-cases иногда приходится подстраиваться под LangGraph idioms.

### Вердикт

`deepagents` сильнее как готовый runtime library.
`tabula` сильнее как самостоятельный platform runtime.

## 2. Модель расширения

### Tabula

Расширение идет через skills:

- `SKILL.md`
- `run.py`
- tool-skills
- hook-skills
- gateways
- bundles

Это очень сильная идея для платформы:

- расширение идет без модификации ядра;
- навыки изолированы процессно;
- skill может быть драйвером, gateway, hook, tool, daemon.

Но это еще и более "инфраструктурная" модель. Чтобы добавить новую capability, нужно думать не только о бизнес-логике, но и о lifecycle/kernel contract.

### DeepAgents

Расширение идет в основном через:

- middleware
- backends
- custom tools
- skills directories
- subagent specs
- CLI overlays

Это быстрее для Python-разработки:

- новый capability часто можно добавить как middleware или tool;
- меньше ceremony;
- сильнее интеграция внутри одного runtime.

Но это менее "операционная" модель. Она отлично работает внутри Python agent graph, но хуже описывает мир независимых long-running процессов.

### Вердикт

- для agent framework extensibility лучше выглядит `deepagents`;
- для platform extensibility лучше выглядит `tabula`.

## 3. Subagents

### Tabula

Subagent в `tabula` — это буквально отдельный процесс и отдельная session scope.

Плюсы:

- сильная изоляция;
- естественный concurrency model;
- subagents не просто логический abstraction, а реальный runtime entity.

Минусы:

- сложнее lifecycle management;
- появляются риски с PID/session accounting;
- aggregation и timeout semantics нужно поддерживать вручную.

### DeepAgents

В `deepagents` есть два основных режима:

- inline subagents через `SubAgentMiddleware`
- async remote subagents через `AsyncSubAgentMiddleware`

Плюсы:

- гибкая модель;
- удобно описывать declarative subagent specs;
- есть путь и для blocking, и для remote async delegation.

Минусы:

- inline subagent — не отдельный OS-process, а дочерний agent graph path;
- изоляция больше логическая, чем системная.

### Вердикт

Если нужна реальная process/session isolation — сильнее `tabula`.
Если нужна быстрая декларативная subagent orchestration — сильнее `deepagents`.

## 4. Контекст, память и compaction

### Tabula

В `tabula` prompt собирается boot-слоем:

- templates
- project files (`IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md`)
- memory
- MCP tools
- environment

Compaction живет в provider runtime и сейчас выглядит как локально реализованный механизм поверх provider APIs.

Сильная сторона:

- prompt assembly очень прозрачный;
- понятно, что именно попадает в system prompt;
- удобная модель пользовательских project files.

Слабая сторона:

- compaction проще и более ad hoc;
- меньше системной интеграции между history persistence и summarization;
- меньше abstraction around offloading old context.

### DeepAgents

У `deepagents` это решено более framework-native:

- memory как middleware;
- skills как middleware;
- summarization как middleware;
- отдельный tool-based compaction;
- offload conversation history в backend.

Это выглядит более зрелым именно как runtime capability.

### Вердикт

По контекст-менеджменту и compaction сейчас сильнее `deepagents`.
По прозрачности prompt-assembly и кастомной project persona-модели интереснее `tabula`.

## 5. Интерфейсы и продуктовая оболочка

### Tabula

У `tabula` уже есть:

- CLI gateway
- API gateway
- Telegram gateway

И все они встроены в общую архитектуру kernel/skills/sessions.

Это сильный platform trait: один runtime, много каналов.

### DeepAgents

У `deepagents` очень сильная CLI product layer:

- отдельный CLI package
- TUI
- resume
- server mode
- remote sandboxes
- MCP integration
- ask-user / approval UX
- ACP integration for editors

То есть у `deepagents` интерфейсная часть выглядит более зрелой как end-user product.

### Вердикт

- по ширине channel architecture сильнее `tabula`;
- по polish и product maturity CLI/editor workflows сильнее `deepagents`.

## 6. Безопасность и governance

### Tabula

У `tabula` есть хорошая базовая идея:

- hook engine
- security/domain/observability hook classes
- permissions skill
- before/after tool hooks
- before_spawn

Это архитектурно очень сильный фундамент.

Но реализация пока менее зрелая:

- часть контрактов еще недоведена;
- жизненный цикл hooks/process/session местами хрупкий;
- enforcement выглядит скорее развивающимся, чем законченным.

### DeepAgents

У `deepagents` security story устроена иначе:

- более честная формулировка "trust the LLM"
- explicit HITL
- shell allow-list middleware
- filesystem permission middleware
- threat model docs

Это не "более безопасно по умолчанию" в абсолютном смысле, потому что `LocalShellBackend` очень опасен и честно это декларирует. Но это лучше описано, лучше документировано и лучше оформлено продуктово.

### Вердикт

- как security architecture idea интереснее `tabula`;
- как documented security posture и practical product controls зрелее `deepagents`.

## 7. DX, packaging, tests, engineering discipline

### DeepAgents

Здесь `deepagents` выглядит заметно сильнее:

- monorepo с четким делением пакетов;
- `uv`, `Makefile`, typing, linting;
- много тестов;
- отдельные threat model документы;
- package-level readme и tooling discipline;
- хорошо оформленная CLI/package story.

По текущему дереву видно существенно более широкий automated test surface, чем у `tabula`.

### Tabula

У `tabula` есть полезное тестовое покрытие и хорошие направления мысли, но текущий engineering contour пока менее стабилен:

- install/dev path местами расходится с реальным состоянием репозитория;
- часть e2e зависит от окружения;
- есть разъезды между тестами и фактическим поведением;
- operational hardening еще не догнал архитектуру.

### Вердикт

По DX, packaging и overall engineering maturity сейчас заметно сильнее `deepagents`.

## 8. Зависимости и архитектурная стоимость

### Tabula

Плюс `tabula`:

- легкое Go-ядро;
- меньше внешней framework-inertia на критическом runtime-слое;
- полный контроль над protocol.

Минус:

- надо самим поддерживать почти все системные абстракции.

### DeepAgents

Плюс `deepagents`:

- reuse мощной экосистемы;
- меньше кастомной низкоуровневой инфраструктуры;
- быстрее velocity в Python-only домене.

Минус:

- высокий dependency surface;
- риск framework coupling;
- часть поведения определяется не только своим кодом, но и LangGraph/LangChain stack.

### Вердикт

- `tabula` дороже в поддержке platform internals;
- `deepagents` дороже по dependency/ecosystem surface.

Это разные виды сложности.

## Где DeepAgents сильнее Tabula

Ниже области, где `deepagents` сейчас выглядит сильнее практически:

- готовый coding/research harness без необходимости строить свой runtime;
- зрелая middleware-композиция;
- checkpointing и resume story;
- compaction/summarization/offloading истории;
- продуктовая CLI/TUI;
- remote sandbox integrations;
- ACP/editor path;
- тестовая и packaging дисциплина;
- документирование threat model и product behavior.

## Где Tabula сильнее DeepAgents

Ниже области, где `tabula` архитектурно сильнее:

- собственный kernel и protocol;
- мультипроцессная skill architecture;
- native multi-gateway system;
- hooks как platform primitive;
- независимые daemons/skills как first-class citizens;
- хороший фундамент для heterogeneous agent platform, а не только одного agent harness;
- реальная session/process isolation для subagents.

## Прямые выводы по сравнению

Самый важный вывод:

`deepagents` и `tabula` не являются прямыми заменами друг друга.

Более точная формулировка:

- `deepagents` — хороший готовый агентный runtime/framework;
- `tabula` — хорошая основа для собственной модульной агентной платформы.

Поэтому:

- если цель — быстро получить сильного coding agent в Python-экосистеме, `deepagents` выигрывает;
- если цель — строить собственную платформу с skill-bus, gateways, hooks и своим control plane, `tabula` остается более уместной архитектурой.

## Может ли Tabula интегрировать DeepAgents внутрь себя?

Да. Причем есть несколько вполне реалистичных сценариев.

Но правильная модель интеграции такая:

- `tabula` остается outer orchestration layer;
- `deepagents` используется как embedded specialist runtime.

Именно так эти проекты комплементарны.

## Самый естественный сценарий

### 1. DeepAgents как специализированный skill/worker внутри Tabula

Это лучший и самый реалистичный сценарий.

Идея:

- в `tabula` добавляется новый skill, например `skills/deepagents-runner/`;
- этот skill получает задачу от `tabula`;
- внутри skill поднимается `deepagents.create_deep_agent(...)`;
- задача выполняется в выделенном backend/workspace;
- назад в `tabula` возвращается короткий результат, summary и артефакты.

Что это дает:

- `tabula` не нужно переписывать свой kernel;
- `deepagents` не нужно заставлять говорить по tabula-protocol напрямую;
- появляется сильный "встроенный эксперт" для code-heavy и research-heavy задач.

Когда это полезно:

- большой codebase analysis;
- multi-step coding task;
- сложный refactor;
- tasks, где нужен хорошо собранный filesystem/shell/todo harness;
- задачи, где пригодятся deepagents middleware и compaction.

### Почему это лучший путь

Потому что здесь соблюдается естественное разделение ролей:

- `tabula` — orchestration, policy, user-facing sessions, gateways;
- `deepagents` — dense task execution inside a bounded work unit.

## Другие реалистичные сценарии интеграции

### 2. DeepAgents как sandbox execution backend для Tabula

`deepagents` уже имеет развитую историю вокруг:

- local shell backend;
- filesystem backend;
- composite backend;
- remote sandboxes;
- LangSmith / Daytona / Modal / Runloop integrations.

`tabula` могла бы использовать `deepagents` не как "основной агент", а как слой безопасного или удаленного выполнения для отдельных задач.

Практически это может выглядеть так:

- Tabula получает задачу "выполни сложную coding job в sandbox";
- skill внутри Tabula поднимает deepagent с нужным backend;
- результат возвращается в Tabula session.

Польза:

- `tabula` не нужно самостоятельно строить всю sandbox ecosystem;
- можно быстрее получить доступ к remote execution stories.

### 3. DeepAgents как long-running async worker для сложных задач

У `deepagents` есть server/remote graph story и async-subagent model.

Это можно использовать так:

- `tabula` создает долгую задачу;
- делегирует ее в отдельный deepagents worker;
- хранит task id / session mapping;
- периодически запрашивает статус или получает финальный результат.

Это особенно полезно для:

- deep research;
- долгих code generation pipelines;
- background tasks;
- scheduled/cron jobs с тяжелой локальной логикой.

### 4. Использовать только идеи и паттерны DeepAgents, а не runtime целиком

Даже если полноценной интеграции не делать, `tabula` явно может заимствовать из `deepagents` подходы:

- backend abstraction для file/sandbox layers;
- middleware-like decomposition на capabilities;
- compaction/offload design;
- structured HITL;
- более зрелую shell allow-list модель;
- richer CLI product patterns;
- ACP/editor integration strategy.

Это тоже форма интеграции, только архитектурной, а не бинарной.

## Где интеграция особенно уместна

Ниже кейсы, где интеграция выглядит особенно логичной.

### Кейс A. "Тяжелый coding task внутри Tabula"

Пример:

- пользователь общается с Tabula через CLI/API/Telegram;
- Tabula понимает, что задача очень code-heavy;
- вызывается `deepagents-runner`;
- он в своей изолированной среде:
  - строит todos,
  - читает/редактирует файлы,
  - делает shell operations,
  - сжимает контекст,
  - возвращает краткий итог.

Это очень естественный сценарий.

### Кейс B. "Tabula как agent platform, DeepAgents как coding coprocessor"

Пример:

- основная личность, память, hooks, routing и gateways живут в Tabula;
- но реальная code execution pipeline делегируется DeepAgents.

Это, вероятно, самый стратегически сильный вариант.

### Кейс C. "Tabula нужен удаленный sandbox, но не хочется строить его с нуля"

Если у `tabula` появится сильный запрос на remote execution:

- CI sandboxes
- ephemeral coding environments
- safe isolated tool execution

то интеграция deepagents backend story может дать short path к рабочему решению.

### Кейс D. "Tabula нужен editor bridge"

`deepagents` уже имеет ACP-layer.

Если `tabula` захочет идти в editor workflows, можно:

- либо заимствовать ACP-подход,
- либо построить bridge-слой, где editor-side talking layer uses deepagents ACP style, а фактическая orchestration остается за Tabula.

Это менее прямой сценарий, но он реальный.

## Где интеграция не очень уместна

Ниже сценарии, которые выглядят плохой идеей.

### 1. Встраивать DeepAgents CLI внутрь Tabula

Это почти наверняка лишняя сложность.

Причина:

- CLI `deepagents` — уже самостоятельный продукт;
- `tabula` уже имеет свои gateways;
- получится наложение двух user-facing orchestration layers.

Если интегрировать — то SDK/runtime, а не CLI product.

### 2. Пытаться заменить kernel Tabula на LangGraph server

Это сломает основную ценность `tabula`:

- собственный protocol;
- process-based skills;
- gateway-bus;
- kernel hooks.

То есть интеграция должна быть вложенной, а не замещающей.

### 3. Держать две независимые policy/approval systems без явного владельца

Это опасный anti-pattern.

У обеих систем есть свои механики управления риском:

- у `tabula`: hooks / permissions / session control;
- у `deepagents`: HITL / permissions middleware / shell allow-list.

Если сложить их без дизайна, получится:

- непредсказуемый UX;
- двойные approvals;
- конфликтующие решения;
- сложная отладка.

Нужен один явный policy owner.

### 4. Нативно смешивать память и историю обеих систем без boundary

Если `tabula` и `deepagents` одновременно будут вести:

- свои histories,
- свои summaries,
- свои memory layers,

без ясной модели синхронизации, получится трудноуправляемый контекстный шум.

Нужно либо:

- передавать `deepagents` ограниченную рабочую задачу и только итог назад;
- либо строить очень аккуратный session/thread bridge.

## Главное архитектурное правило интеграции

Если `tabula` интегрирует `deepagents`, то нужно интегрировать его как bounded executor, а не как второй главный мозг.

Хорошая формула:

- `tabula` решает, когда делегировать;
- `deepagents` исполняет;
- `tabula` решает, что показать пользователю и как встроить итог обратно в session.

Плохая формула:

- `tabula` и `deepagents` параллельно конкурируют за ownership prompts, approvals, memory и file authority.

## Практические варианты реализации интеграции

## Вариант 1. SDK-внутри-skill

Самый рекомендуемый.

Схема:

1. В `tabula` создается skill `deepagents-runner`.
2. Skill получает JSON input с задачей.
3. Внутри skill создается `deepagent` через `create_deep_agent(...)`.
4. Используется `FilesystemBackend` или ограниченный backend.
5. По завершении skill возвращает summary + optional artifact paths.

Преимущества:

- минимальный integration surface;
- простой rollback;
- нет необходимости поднимать отдельный server layer.

Недостатки:

- streaming будет сложнее, если нужен live token-by-token relay;
- lifecycle полностью придется оборачивать внутри skill.

## Вариант 2. Отдельный deepagents sidecar server

Схема:

1. `tabula` при необходимости поднимает или использует уже поднятый deepagents server.
2. Общение идет через client/server API.
3. `tabula` хранит mapping: `tabula_session -> deepagents_thread`.

Преимущества:

- лучше для long-running tasks;
- проще reuse remote/checkpoint/server features.

Недостатки:

- больше operational complexity;
- появляются cross-system lifecycle и deployment concerns.

## Вариант 3. Асинхронный фоновый deepagents worker

Схема:

1. `tabula` запускает задачу;
2. deepagents работает асинхронно;
3. `tabula` показывает status/результат.

Преимущества:

- хорошо для долгих задач;
- хорошо для cron/background flows.

Недостатки:

- нужен четкий task protocol и state bridge.

## Какой путь рекомендован первым

Если делать интеграцию, то первым прототипом должен быть именно `SDK-внутри-skill`.

Почему:

- fastest path to value;
- smallest blast radius;
- не требует переделки ядра;
- не создает вторую control plane;
- можно быстро понять, дает ли DeepAgents практическую прибавку именно внутри Tabula.

## Что нужно решить до интеграции

Перед интеграцией нужно договориться о нескольких архитектурных правилах.

### 1. Кто владеет approvals/policy

Нужно выбрать одно:

- либо outer policy owner — `tabula`;
- либо inner policy owner — `deepagents`.

Рекомендация:

- outer policy owner должен быть `tabula`;
- внутри `deepagents` лучше отключать или минимизировать пересекающиеся approval-механики, если они уже контролируются снаружи.

### 2. Кто владеет session state

Рекомендация:

- `tabula` session остается primary;
- `deepagents` thread/checkpoint должен быть derivation от `tabula` session, а не отдельной равноправной сущностью для пользователя.

### 3. Кто владеет filesystem boundary

Нужно четко решить:

- какой root разрешен;
- кто проверяет path access;
- можно ли shell;
- где живут артефакты.

### 4. Что именно возвращается обратно

Лучше возвращать не полный trace, а:

- summary;
- findings;
- artifact paths;
- structured result.

Иначе основной контекст `tabula` будет быстро захламляться внутренней рабочей историей `deepagents`.

## Конкретные кейсы, в которых интеграция особенно перспективна

Вот список кейсов, где интеграция выглядит по-настоящему полезной:

1. Большие coding tasks с множеством file operations и shell steps.
2. Долгий анализ репозитория с последующей краткой выдачей результата в основной сессии Tabula.
3. Фоновые research/coding jobs, где нужен отдельный task lifecycle.
4. Безопасное или удаленное выполнение в sandbox через уже существующие deepagents integrations.
5. Будущие editor-oriented workflows, если Tabula захочет идти в ACP/editor ecosystem.

## Кейсы, где лучше не интегрировать

1. Если задача уже хорошо решается текущими skills Tabula и не требует более богатого runtime.
2. Если цель — просто заменить часть prompt logic, а не получить новый execution substrate.
3. Если команда не готова обслуживать дополнительный Python/LangGraph dependency surface.
4. Если нет ясного ответа, кто владеет approvals, memory и history.

## Итоговый вердикт

`deepagents` сильнее `tabula` не "вообще", а в конкретной роли:

- как готовый coding/research harness;
- как продуктово зрелый Python agent stack;
- как источник sandbox/checkpoint/middleware patterns.

`tabula` сильнее `deepagents` не "вообще", а в другой роли:

- как своя модульная агентная платформа;
- как kernel/protocol/gateway system;
- как control plane для набора agent capabilities.

Поэтому лучший способ совместить их:

- не противопоставлять,
- не пытаться делать миграцию одной системы в другую,
- а использовать `deepagents` как встроенный специализированный двигатель внутри `tabula`.

## Рекомендуемое решение на практике

Если начинать работу, то лучший следующий шаг такой:

1. Сделать в `tabula` экспериментальный skill `deepagents-runner`.
2. Ограничить его одной задачей: code-heavy delegation.
3. На первом этапе использовать только SDK `deepagents`, не CLI.
4. Не тащить сразу memory/HITL/async-server complexity — сначала bounded execution.
5. Возвращать в `tabula` только summary и артефакты.

Если этот прототип покажет реальную прибавку:

- тогда уже можно рассматривать второй этап:
  - remote sandboxes,
  - async jobs,
  - richer session bridge,
  - editor/ACP story.

Это выглядит самым рациональным и технически здоровым путем.
