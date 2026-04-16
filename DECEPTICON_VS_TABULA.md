# Decepticon vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `Decepticon`: `/Users/mak/src/Decepticon`

За основу оценки `tabula` взят уже подготовленный [`ANALYSIS.md`](/Users/mak/src/tabula/ANALYSIS.md), дополнительно сверенный по ключевым частям ядра и gateway-слоя.

По `Decepticon` анализ основан на чтении:

- `README.md`
- `pyproject.toml`
- `docker-compose.yml`
- `langgraph.json`
- `decepticon/__main__.py`
- `decepticon/agents/*`
- `decepticon/core/*`
- `decepticon/middleware/*`
- `decepticon/backends/*`
- `decepticon/tools/*`
- `clients/cli/*`
- `docs/architecture/*`, `docs/design/*`, `docs/vision/*`
- `tests/unit/*`

Важно: в отличие от `ANALYSIS.md` по `tabula`, здесь не делался полноценный прогон тестов и контейнерного контура, потому что в текущем локальном окружении для `Decepticon` отсутствуют готовые зависимости (`uv`, `pytest`). Поэтому это глубокий code-and-contract analysis, а не полный runtime audit.

## Короткий вывод

`Decepticon` и `tabula` пересекаются по теме агентных систем, skills и orchestration, но находятся на разных уровнях.

- `tabula` — это платформенное ядро: собственный Go-kernel, wire protocol, мультипроцессная модель skills, gateways и hooks.
- `Decepticon` — это уже вертикальный продукт: opinionated red-team stack поверх `LangGraph`/`deepagents`, с Docker-infra, CLI, sandbox, C2, Neo4j и доменной моделью Red Team engagement.

Если очень коротко:

- `Decepticon` заметно сильнее `tabula` как продуктовая оболочка, operator experience и инфраструктурно собранная система.
- `tabula` заметно сильнее `Decepticon` как самостоятельная агентная платформа с собственным runtime sovereignty.
- `Decepticon` выглядит более зрелым в install/dev/CLI/infrastructure story.
- `tabula` выглядит архитектурно более фундаментальной и более общей.

Главный практический вывод:

- если цель — строить универсальную агентную платформу, `tabula` идейно сильнее;
- если цель — строить именно автономный red-team продукт, `Decepticon` сейчас ближе к usable system;
- если цель — улучшать `tabula`, то полезнее всего заимствовать у `Decepticon` не LangGraph как таковой, а product shell, sandbox execution story, install path и operator tooling.

## Что представляет собой каждый проект

## Tabula

`tabula` построена как собственная агентная платформа:

- Go-ядро в `internal/kernel/*`;
- `boot.py` как конфигурационный и prompt-assembly слой;
- Python-skills как отдельные процессы;
- собственный WebSocket protocol;
- gateway-слой как отдельные skills;
- hooks, permissions, subagents, memory, MCP.

Ключевая идея: ядро владеет lifecycle, routing и protocol contract, а capabilities живут в pluggable skills.

## Decepticon

`Decepticon` — это domain-specific multi-agent red-team system:

- LangGraph graphs зарегистрированы в `langgraph.json`;
- агенты собираются напрямую через `create_agent(...)` в `decepticon/agents/*`;
- execution идет через `deepagents` middleware stack;
- tool execution вынесен в Docker sandbox с persistent `tmux` sessions;
- есть отдельный Ink CLI;
- есть изолированная Docker-infra: LiteLLM, PostgreSQL, Neo4j, sandbox, LangGraph, CLI, C2;
- доменная модель описана через `RoE`, `CONOPS`, `OPPLAN`, `Finding`, `AttackPath`, `DefenseBrief`.

Ключевая идея: не строить свой kernel/protocol, а собрать вертикально интегрированный red-team продукт поверх готового agent runtime.

## Самое важное архитектурное различие

`tabula` и `Decepticon` решают похожую задачу на разных слоях.

### Tabula

Мысленно это:

- kernel;
- protocol;
- process runtime;
- session bus;
- skill ecosystem.

Она ближе к "agent microkernel".

### Decepticon

Мысленно это:

- LangGraph/deepagents harness;
- middleware-composed agents;
- Dockerized execution environment;
- operator CLI;
- domain-specific red-team workflow;
- infrastructure bundle вокруг агента.

Она ближе к "вертикальному автономному security-продукту".

Именно поэтому прямой вопрос "что лучше?" здесь слишком грубый. Правильнее спрашивать: на каком слое каждая система сильнее.

## Глубокое сравнение

## 1. Runtime-модель

### Tabula

Runtime `tabula` держится на своем kernel:

- skill-процессы живут отдельно;
- gateway-процессы живут отдельно;
- subagents — отдельные runtime entities;
- ядро владеет message routing, session lifecycle и process supervision.

Плюсы:

- высокая архитектурная независимость;
- настоящий protocol bus;
- естественная multi-language extensibility;
- сильная компонентная изоляция.

Минусы:

- сложнее поддерживать correctness lifecycle;
- gateway/session/process bugs становятся системными;
- install/dev story пока отстает от архитектуры.

### Decepticon

Runtime `Decepticon` — это graph-based orchestration:

- агенты собираются через `create_agent(...)`;
- поведение добавляется middleware-слоями;
- subagents подключаются через `SubAgentMiddleware`;
- shell execution идет через `DockerSandbox`;
- orchestration наружу публикуется как LangGraph Platform API.

Плюсы:

- намного быстрее собрать end-to-end систему;
- меньше собственного низкоуровневого runtime-кода;
- orchestration, summarization, fallback, prompt caching и filesystem уже приходят как готовые слои;
- легче построить продуктовый UX вокруг agent runtime.

Минусы:

- сильная зависимость от внешнего runtime stack;
- меньше контроля, чем у собственного kernel;
- часть критических контрактов уходит из явного protocol в смесь middleware + prompt discipline.

### Вердикт

- как самостоятельный runtime/platform core сильнее `tabula`;
- как productized execution stack заметно сильнее `Decepticon`.

## 2. Инфраструктура и изоляция

Здесь `Decepticon` выигрывает очень заметно.

В `docker-compose.yml` у него уже собран полноценный operational shell:

- `litellm`;
- `postgres`;
- `neo4j`;
- `sandbox`;
- `langgraph`;
- `cli`;
- profile-based C2 (`c2-sliver`);
- demo victims.

Особенно удачно выглядит разделение сетей:

- `decepticon-net` для control plane;
- `sandbox-net` для offensive execution.

Плюсы такого подхода:

- операторская инфраструктура отделена от атакующей среды;
- sandbox story заранее встроена в архитектуру;
- demo/dev/prod-like сценарии выглядят единообразно;
- проект сразу думает категориями deployment topology, а не только локального запуска.

На фоне `tabula` это сильный контраст. В `tabula` архитектурная идея сильная, но product shell и infra story пока слабее и местами ломаются на install/runtime contracts.

### Вердикт

По инфраструктурной зрелости и operational packaging `Decepticon` уверенно впереди `tabula`.

## 3. Модель расширения

### Tabula

Расширение идет через skills как отдельные executable-компоненты:

- `SKILL.md`;
- `run.py`;
- tool-skills;
- drivers;
- gateways;
- hooks;
- bundles.

Это мощная платформенная модель: capability можно добавлять без переписывания ядра, причем capability может быть не только tool, но и gateway, hook или driver.

### Decepticon

Расширение идет иначе:

- новый агент;
- новый middleware;
- новые tools;
- новые skill directories;
- новые prompt-фрагменты;
- новые Docker/infrastructure surfaces.

Это быстрее и прагматичнее, но слабее как "общая agent platform". `Decepticon` расширяется хорошо внутри своей продуктовой модели, но не пытается быть универсальной skill bus системой как `tabula`.

### Вердикт

- для platform extensibility сильнее `tabula`;
- для feature delivery внутри одного red-team продукта сильнее `Decepticon`.

## 4. Subagents и orchestration

### Tabula

Subagent в `tabula` — это отдельный процесс и отдельный session scope. Это дороже operationally, но честнее как runtime abstraction.

### Decepticon

Subagents в `Decepticon` реализованы через:

- `CompiledSubAgent`;
- `SubAgentMiddleware`;
- `StreamingRunnable`.

Это очень хороший product pattern:

- orchestrator может делегировать работу специализированным агентам;
- streaming событий subagent-а доходит до CLI и HTTP API;
- контекст каждого specialist agent остается относительно чистым.

Но это все же логическая изоляция внутри общего agent ecosystem, а не отдельная protocol/process system как в `tabula`.

### Вердикт

- по глубине runtime isolation сильнее `tabula`;
- по usability и operator visibility subagents сильнее `Decepticon`.

## 5. Session/tool execution story

Это одна из самых сильных сторон `Decepticon`.

`decepticon/tools/bash/bash.py` + `decepticon/backends/docker_sandbox.py` дают:

- persistent `tmux` sessions;
- интерактивный ввод;
- stall detection;
- auto-background;
- size watchdog;
- offloading больших outputs в `/workspace/.scratch/`;
- ANSI stripping и repetitive-line compression.

Это уже не просто "run subprocess", а осмысленный interactive operator shell для агента.

На фоне этого `tabula` сейчас слабее именно по operational execution surface:

- в `ANALYSIS.md` уже были проблемы session/process bookkeeping;
- gateway/session lifecycle там хрупче;
- shell/runtime story менее productized.

### Вердикт

По interactive shell execution и long-running tool UX `Decepticon` выглядит лучше `tabula`.

## 6. Policy, governance и контроль

### Tabula

Сильная сторона `tabula` — сама идея kernel-owned hooks/policy engine:

- `before_message`;
- `before_tool_call`;
- `before_spawn`;
- наблюдаемость и permissions как kernel concern.

Это очень сильный фундамент, несмотря на текущие разрывы в реализации.

### Decepticon

У `Decepticon` governance больше распределен между:

- middleware;
- системными prompt rules;
- domain tools (`OPPLANMiddleware`);
- safe-command блокировками.

Это работает быстро и практично, но опирается сильнее на composition discipline и LLM behavior, чем на собственный protocol-enforced control plane.

### Вердикт

- как архитектурная governance-model сильнее `tabula`;
- как practical product guardrail set в конкретном red-team домене `Decepticon` уже очень убедителен.

## 7. Product shell и developer experience

Здесь `Decepticon` снова выглядит существенно сильнее.

Подтверждения:

- нормальный installer в `scripts/install.sh`;
- launcher/update/config/demo flows;
- `decepticon.__main__` с readiness-check на LangGraph;
- понятный `Makefile`;
- CI, который гоняет Python lint/type/test, CLI typecheck/tests, Docker build, Trivy и Gitleaks;
- отдельный Ink CLI;
- demo story.

Это как раз то, чего `tabula` сейчас не хватает сильнее всего по `ANALYSIS.md`:

- install/dev path у `tabula` сломан;
- entrypoints расходятся с реальным поведением;
- зависимости и launchers не доведены до надежного контракта.

### Вердикт

По DX, onboarding и product packaging `Decepticon` заметно зрелее `tabula`.

## 8. Доменная модель

`Decepticon` очень силен именно как red-team domain model.

Схемы в `decepticon/core/schemas.py` и `decepticon/schemas/defense_brief.py` покрывают:

- engagement documents;
- objective phases и statuses;
- findings;
- evidence;
- attack paths;
- remediation priority;
- defensive feedback loop.

Это дает проекту редкую для agent-систем вещь: не только runtime, но и формализованный domain state.

У `tabula` модель более general-purpose. Это плюс как платформе, но минус, если нужен готовый vertical application.

### Вердикт

По глубине red-team domain modeling `Decepticon` сильнее `tabula`.

## 9. Тестовый и инженерный контур

У `Decepticon` я насчитал:

- 65 файлов unit tests;
- 59 `SKILL.md`;
- 17 зарегистрированных graphs в `langgraph.json`.

Это не означает автоматически более высокое качество, но означает более собранную инженерную дисциплину вокруг продукта.

CI тоже выглядит зрелее, чем у `tabula`:

- проверяются Python и CLI отдельно;
- есть Docker build в CI;
- есть security scanning.

При этом есть и оговорки:

- coverage threshold пока только `30%`;
- часть high-level orchestration выглядит еще не полностью operationalized;
- некоторые важные контракты расходятся между docs, prompts и runtime.

### Вердикт

Инженерный контур `Decepticon` выглядит более продуктово собранным, но это не отменяет нескольких серьезных разрывов в текущей реализации.

## Подтвержденные риски и проблемы Decepticon

Ниже — не абстрактные замечания, а конкретные технические разрывы, подтвержденные чтением кода.

### P0/P1. Engagement loop смотрит не туда: дефолтный `langgraph_url` устарел

В `decepticon/core/engagement.py` `EngagementConfig.langgraph_url` по умолчанию равен `http://localhost:8123`, тогда как:

- launcher в `decepticon/__main__.py` ждет LangGraph на `2024`;
- CLI в `clients/cli/src/hooks/useAgent.ts` тоже использует `http://localhost:2024`;
- `docker-compose.yml` публикует LangGraph именно на `2024`.

Это означает, что convenience-path для `EngagementLoop` по дефолту целится в устаревший адрес. Для attack/vaccine orchestration это уже не просто косметика, а сломанный runtime default.

### P0/P1. Defensive lane по умолчанию применяет действия в атакующем sandbox

В `decepticon/agents/defender.py` и `DockerSandbox`, и `DockerDefenseBackend` получают один и тот же `config.docker.sandbox_container_name`.

Сам файл прямо признает проблему:

- defender сейчас работает против того же контейнера, что и offensive agent;
- "in production" это должно быть перенесено на отдельный defense/hardening target.

Практический вывод:

- vision про attack-defense loop в коде уже заявлена;
- но текущая дефолтная wiring-модель подрывает достоверность defensive verification;
- в текущем виде это ближе к architectural stub, чем к полностью корректной defensive plane.

### P1. Контракт knowledge graph разъехался между кодом, prompt-ами и docs

`decepticon/tools/research/_state.py` уже переведен на Neo4j-only state management.

Но при этом:

- некоторые agent comments и prompts все еще говорят про default `/workspace/kg.json`;
- `docs/design/single-bash-and-graphdb.md` все еще описывает JSON backend как default;
- комментарии в `decepticon/agents/decepticon.py` и `decepticon/agents/vulnresearch.py` тоже отсылают к JSON path.

Это не просто устаревший комментарий. Это уже инженерный разрыв между:

- фактическим backend contract;
- ожиданиями автора prompt-а;
- ожиданиями разработчика/оператора.

### P1. Высокоуровневая orchestration logic продублирована в двух местах

В проекте есть сразу два похожих orchestrator-контура:

- `decepticon/orchestrator.py`;
- `decepticon/core/engagement_loop.py`.

Оба умеют:

- сканировать findings;
- генерировать defense briefs;
- запускать defense/verification path;
- сохранять state.

Это опасный источник drift:

- одна ветка может быть обновлена, другая нет;
- тесты могут прикрывать одну реализацию, а фактический продукт — другую;
- high-level lifecycle становится труднее держать в голове.

Дополнительно поиск по репозиторию показывает, что эти entrypoints почти не интегрированы в основной пользовательский поток и в основном видны самим себе и тестам. То есть часть "attack-defense loop" пока больше похожа на внутреннюю архитектурную заготовку, чем на центральный product path.

### P1. Prompt-contract для OPPLAN расходится с реальными enum-status

В `decepticon/agents/prompts/decepticon.md` orchestrator инструктируется обновлять objective через:

- `status="passed/blocked"`

Но `ObjectiveStatus` в `decepticon/core/schemas.py` поддерживает:

- `pending`
- `in-progress`
- `completed`
- `blocked`
- `cancelled`

Это типичный prompt/runtime mismatch:

- модель подталкивают вызывать tool с невалидным значением;
- middleware потом вынуждено либо отклонять вызов, либо исправлять поведение косвенно;
- orchestration contract становится хрупким.

### P1/P2. Defender agent указывает на skill-source, которого нет в дереве skills

`decepticon/agents/defender.py` подключает:

- `/skills/defender/`

Но в реальном дереве `skills/` такой директории нет.

В лучшем случае это означает, что defender лишен ожидаемого skill-layer.
В худшем — что инициализация skills middleware зависит от несуществующего источника.

Здесь я осторожен: точное runtime-поведение зависит от того, как базовый `SkillsMiddleware` обрабатывает missing source. Но сам разрыв между wiring и деревом репозитория — уже факт.

### P2. Документация отстает от фактической agent surface

README и `docs/agents.md` продолжают описывать систему как набор из пяти specialist agents, тогда как `langgraph.json` регистрирует уже существенно более широкий набор:

- `analyst`
- `reverser`
- `contract_auditor`
- `cloud_hunter`
- `ad_operator`
- `vulnresearch`
- `scanner`
- `detector`
- `verifier`
- `patcher`
- `exploiter`
- `defender`

Это не ломает runtime напрямую, но показывает, что docs lag уже начался.

## Что Decepticon делает лучше Tabula

Ниже — то, где `Decepticon` действительно сильнее `tabula` не по рекламе, а по инженерной реальности.

### 1. Product shell

У `Decepticon` есть законченная оболочка:

- installer;
- launcher;
- config/update/demo flows;
- CLI;
- Dockerized startup.

У `tabula` это сейчас слабое место.

### 2. Infra-first thinking

`Decepticon` с самого начала думает сетями, контейнерами, sandbox-ами, C2, build images и profiles. У `tabula` эта плоскость заметно тоньше.

### 3. Interactive execution surface

Persistent `tmux` sandbox и offloading больших outputs в `Decepticon` реализованы лучше, чем текущий session/process surface `tabula`.

### 4. Domain-specific orchestration

`OPPLANMiddleware`, red-team schemas и prompt discipline делают `Decepticon` более целостной системой именно для offensive security.

### 5. Operator UX и observability в продукте

Streaming subagent events в CLI, canvas, grouped sessions и отдельные visual surfaces дают более зрелый пользовательский опыт, чем текущий gateway story `tabula`.

## Что Tabula делает лучше Decepticon

Важно не потерять и обратную сторону.

### 1. Runtime sovereignty

`tabula` не строится на чужом orchestration stack как на фундаменте. У нее свой kernel, свой protocol, свои session semantics.

### 2. Generality

`tabula` не зашита в один домен. Она ближе к общей agent platform, тогда как `Decepticon` очень opinionated и вертикален.

### 3. Чистота платформенной идеи

Разделение на kernel и внешние skill-процессы у `tabula` концептуально сильнее и долговечнее, чем смесь middleware/prompt/runtime coupling в `Decepticon`.

### 4. Governance как системный слой

Даже с текущими багами hook/policy model `tabula` более фундаментальна, чем governance через middleware + prompt rules.

## Архитектурный вердикт

`Decepticon` — более зрелый продукт.

`tabula` — более сильная платформа.

Это не взаимоисключающие оценки.

Если спрашивать:

- "какой проект сегодня проще показать, запустить и использовать как конкретный red-team продукт?" — ответ скорее `Decepticon`;
- "какой проект выглядит как более фундаментальная база для собственной agent OS / protocol platform?" — ответ скорее `tabula`.

На языке зрелости:

- у `Decepticon` сильнее outer shell;
- у `tabula` сильнее core idea.

На языке рисков:

- у `tabula` самые большие проблемы сегодня — operational hardening;
- у `Decepticon` самые большие проблемы сегодня — contract drift между vision, prompts, docs и реальным wiring.

## Что Tabula стоит заимствовать у Decepticon

Если использовать это сравнение прикладно, то `tabula` особенно полезно перенять следующие слои:

1. Productized install/start/update path.
2. Dockerized sandbox execution story с persistent interactive sessions.
3. Более жесткую operator-facing shell вокруг агента:
   CLI, health checks, demo path, clear readiness signals.
4. CI, который проверяет не только код, но и сборку runtime surfaces.
5. Более явные domain/state schemas для сложных сценариев orchestration.

## Что Tabula не стоит копировать вслепую

Есть и вещи, которые `tabula` не стоит просто повторять.

1. Полную зависимость от `LangGraph`/`deepagents` как основы всей системы.
2. Сильное смешение runtime behavior, prompt contracts и middleware conventions.
3. Сверхтяжелую Docker-centric продуктовую оболочку, если цель остается платформенной.
4. Дублирование orchestration paths без единого source of truth.

## Практическая формула

Если свести все к одной рабочей формуле:

- `tabula` стоит продолжать как platform core;
- `Decepticon` стоит рассматривать как хороший пример того, каким должен стать product shell вокруг сложной агентной системы;
- лучший путь для `tabula` — не "стать Decepticon", а забрать у него operator maturity:
  - install;
  - sandbox execution;
  - product CLI;
  - health/readiness;
  - CI на runtime surfaces.

А для `Decepticon` главный урок от `tabula` был бы обратным:

- меньше drift между contracts и реализацией;
- меньше дублирования orchestration логики;
- более явный ownership runtime semantics на уровне системы, а не только prompt/middleware композиции.
