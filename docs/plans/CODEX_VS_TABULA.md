# Codex vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `codex`: `/Users/mak/src/codex`

По `tabula` за основу взят уже подготовленный `ANALYSIS.md`, где основной вывод такой:

- архитектурная идея сильная;
- разделение Go-kernel и Python-skills удачное;
- главные проблемы сейчас лежат в install/runtime contract, lifecycle, gateway concurrency и session/process management.

По `codex` анализ основан на локальном репозитории, без обращения к внешней документации, и опирается в первую очередь на:

- `README.md`
- `docs/config.md`
- `docs/install.md`
- `docs/contributing.md`
- `codex-rs/README.md`
- `codex-rs/core/README.md`
- `codex-rs/app-server/README.md`
- `codex-rs/tools/README.md`
- `codex-rs/exec-server/README.md`
- `codex-rs/execpolicy/README.md`
- `codex-rs/shell-escalation/README.md`
- `codex-rs/core/src/lib.rs`
- `codex-rs/core/src/codex.rs`
- `codex-rs/core/src/thread_manager.rs`
- `codex-rs/core/src/tools/orchestrator.rs`
- `codex-rs/core/src/tools/parallel.rs`
- `codex-rs/core/src/unified_exec/mod.rs`
- `codex-rs/core/src/hook_runtime.rs`
- `codex-rs/core/src/agent/registry.rs`
- `codex-rs/core/src/config/mod.rs`
- `codex-rs/core/src/skills.rs`
- `codex-rs/core/src/spawn.rs`
- `codex-rs/core/src/memories/README.md`
- `codex-rs/app-server/src/lib.rs`
- `codex-rs/rollout/src/recorder.rs`
- `codex-rs/state/src/lib.rs`

Важно: это глубокий архитектурный анализ по коду и локальной документации. В отличие от `ANALYSIS.md` для `tabula`, здесь не делался полный прогон всего тестового контура `codex`, потому что рабочая область существенно больше и включает Cargo/Bazel/SDK/release-инфраструктуру.

## Короткий вывод

`codex` и `tabula` решают близкую тему, но находятся на разных уровнях зрелости и немного на разных уровнях абстракции.

- `tabula` сегодня ближе к компактной агентной платформе или "agent microkernel":
  - свой kernel;
  - свой wire protocol;
  - много логики вынесено в отдельные skill-процессы;
  - хорошая языковая и процессная модульность.
- `codex` ближе к полноценно продуктализированной local coding agent platform:
  - большой Rust workspace;
  - единая thread/turn/item модель;
  - TUI, CLI, app-server, exec-server, SDK;
  - кроссплатформенный sandboxing;
  - persistence, approvals, policy, hooks, multi-agent, memories, plugins, MCP.

Если очень коротко:

- `codex` заметно сильнее `tabula` по operational maturity;
- `codex` заметно сильнее по session/state/control-plane архитектуре;
- `codex` намного сильнее по sandbox/approval/tool orchestration;
- `tabula` пока чище и проще как идея именно модульного multi-process agent kernel;
- `tabula` не стоит превращать в `codex` целиком;
- но `tabula` очень стоит заимствовать из `codex` несколько инфраструктурных слоев.

Главный практический вывод:

- если цель — надежный локальный coding agent product, `codex` сейчас впереди очень далеко;
- если цель — компактная, расширяемая, language-agnostic агентная платформа, у `tabula` все еще есть более чистая архитектурная ставка;
- лучший путь для `tabula` — не копировать весь `codex`, а импортировать его operational patterns.

## Что представляет собой Codex

По коду видно, что `codex` — это не один бинарник и не один runtime-модуль, а большой набор согласованных слоев.

Важный масштаб:

- в `codex-rs` сейчас 91 workspace member;
- внутри `codex-rs` около 3377 файлов;
- сам `codex-rs` занимает около 37 MB;
- только `.rs` файлов порядка 1458;
- test-like файлов порядка 188;
- кроме Rust-ядра есть TypeScript SDK, Python SDK, release tooling, Bazel-слой, scripts и docs.

Ключевые продуктовые поверхности:

- `codex` как основной CLI multitool;
- `codex-tui` как полноэкранный TUI;
- `codex exec` как headless/non-interactive режим;
- `codex app-server` как JSON-RPC control plane для IDE/app integrations;
- `codex exec-server` как отдельный subprocess/filesystem server;
- `codex mcp-server` как MCP server;
- Python и TypeScript SDK.

Ключевые инфраструктурные слои:

- `codex-core` как бизнес-логика системы;
- `codex-protocol` как общие типы для internal/external protocol;
- `codex-tools` как tool schema/tool spec primitives;
- `codex-hooks` как hook runtime;
- `codex-rollout` и `codex-state` как persistence/state layer;
- `codex-config` и `core/src/config/*` как configuration and policy layer;
- `core-skills`, `skills`, `plugin`, `connectors`, `mcp` как extension surface;
- `sandboxing`, `linux-sandbox`, `shell-escalation`, `process-hardening` как OS/security слой.

То есть `codex` по факту уже не "agent prototype", а крупная платформа вокруг coding agent.

## Самое важное архитектурное различие

`tabula` и `codex` не совсем на одном уровне абстракции.

### Tabula

Мысленно это:

- kernel;
- protocol bus;
- process supervisor;
- отдельные skills как процессы;
- gateway и hooks как внешние capabilities.

### Codex

Мысленно это:

- единый application core;
- thread/turn/item control plane;
- встроенный tool runtime;
- встроенные approvals/sandbox/persistence;
- app-server для внешних клиентов;
- plugin/skills/MCP/connectors поверх одного host runtime.

Из этого следует очень важная вещь:

- `tabula` лучше сравнивать с `codex` не как "два одинаковых продукта";
- правильнее сравнивать `tabula` как agent kernel/platform core, а `codex` как productized agent platform с уже собранным control plane.

Именно поэтому `codex` часто выглядит "намного сильнее", но часть этой силы оплачена гораздо большей системной сложностью.

## Глубокое сравнение

## 1. Runtime-модель

### Tabula

По `ANALYSIS.md`:

- Go-kernel держит маршрутизацию, process lifecycle и protocol;
- Python skills живут как отдельные процессы;
- gateway, drivers, hooks и tools вынесены наружу;
- WebSocket — основной runtime seam.

Плюсы:

- хорошая изоляция;
- heterogenous capabilities по языкам;
- очень ясная идея "ядро отдельно, навыки отдельно";
- легко мыслить систему как platform bus.

Минусы:

- runtime correctness приходится держать на стыках процессов;
- gateway logic вынужденно тащит session/process concerns;
- lifecycle bugs становятся системными быстрее.

### Codex

`codex` делает почти противоположную ставку:

- максимальный объем критической логики держится внутри одного Rust host runtime;
- разные UI и клиенты подключаются к этому runtime через typed protocol;
- внешние процессы выносятся в основном для shell/exec/sandbox и некоторых transport-сценариев, а не для каждой capability;
- session logic, tool orchestration, approvals, persistence, agent control и model interaction сидят в `codex-core`.

Плюсы:

- намного меньше случайной распределенности;
- сильнее типизация и единый state model;
- меньше поверхностей, где gateway должен "достраивать" runtime вручную;
- легче строить сквозные фичи через один core.

Минусы:

- core становится очень большим и тяжелым;
- ошибки в core потенциально имеют более широкий blast radius;
- расширение хуже изолировано процессно, чем в `tabula`.

### Вердикт

`tabula` сильнее как идея process-oriented platform runtime.

`codex` сильнее как централизованный, продуктовый и уже сильно хардненый runtime.

## 2. Session/control-plane модель

### Tabula

По `ANALYSIS.md` главный operational stress у `tabula` сейчас сосредоточен именно здесь:

- session/process bookkeeping расходится;
- gateway sessions живут слишком долго и не всегда корректно чистятся;
- API и Telegram gateways тащат собственный lifecycle;
- turn serialization и cleanup policy в gateway-слое пока слабые.

То есть у `tabula` есть хорошее ядро сообщений, но нет такого же сильного единого control plane поверх него.

### Codex

`codex` здесь выглядит на порядок зрелее.

Из `codex-rs/app-server/README.md` и `codex-rs/app-server/src/lib.rs` видно, что система строится вокруг очень четкой модели:

- `Thread` — разговор;
- `Turn` — один агентный проход;
- `Item` — атомарный input/output/event;
- все это доступно через app-server API;
- TUI, app, IDE и headless-клиенты разговаривают с одной и той же моделью.

Особенно сильные стороны:

- `thread/start`, `thread/resume`, `thread/fork`, `thread/list`, `thread/read`;
- `turn/start`, поток уведомлений и финализация через `turn/completed`;
- единый JSON-RPC contract вместо того, чтобы каждый gateway отдельно собирал session story;
- bounded queues и backpressure;
- явный overload path с `-32001 "Server overloaded; retry later."`;
- graceful restart drain, где сервер дожидается завершения running assistant turns.

Это уже не просто protocol, а полноценный control plane.

### Вердикт

Если смотреть именно на session/control-plane слой, `codex` значительно сильнее `tabula`.

Это один из главных слоев, которые `tabula` стоит изучать и частично копировать.

## 3. Tool execution, approvals и sandboxing

### Tabula

По `ANALYSIS.md` в `tabula` permissions и governance в основном завязаны на hook-архитектуру:

- идея сильная;
- contract гибкий;
- есть modifying/security hooks;
- но часть контрактов пока реализована не до конца;
- operational enforcement слабее, чем сама идея.

То есть у `tabula` permissions сегодня ближе к policy framework, чем к глубоко собранному execution layer.

### Codex

У `codex` это, пожалуй, один из сильнейших слоев всей системы.

Что видно по коду:

- `core/src/tools/orchestrator.rs` централизует approval, sandbox selection, retry и escalation semantics;
- `core/src/unified_exec/mod.rs` держит интерактивные процессы, output caps, reuse, yield time, PTY lifecycle;
- `core/src/spawn.rs` аккуратно управляет child process environment, stdio policy и parent death signal;
- `codex-execpolicy` дает отдельный policy DSL для команд;
- `codex-shell-escalation` и `codex-execve-wrapper` строят shell escalation protocol;
- `core/README.md` описывает реально серьезный cross-platform sandbox stack для macOS, Linux и Windows;
- `guardian/*` добавляет отдельный автоматический reviewer для approval flow.

Особенно важно:

- approval и sandboxing не разбросаны по gateway-слою;
- retry semantics и escalation живут в одном месте;
- есть отдельная модель network approval;
- есть managed requirements и constrained config;
- enforcement опирается не только на prompt-level правила, но и на OS/runtime-level механизмы.

### Вердикт

На слое `tool runtime + permissions + sandboxing` `codex` сильнее `tabula` очень значительно.

Если `tabula` нужно выбирать один слой для заимствования идей в первую очередь, это именно он.

## 4. Модель расширения

### Tabula

`tabula` расширяется через skills как реальные исполняемые единицы:

- `SKILL.md`;
- `run.py`;
- tool-skills;
- hook-skills;
- gateways;
- drivers;
- daemons.

Это очень сильная модель, потому что extension surface совпадает с runtime surface:

- skill — это не только prompt artifact, а реальный компонент системы;
- skill может жить в своем процессе;
- skill может быть написан на другом языке;
- ядро остается сравнительно нейтральным.

### Codex

У `codex` extension surface богаче, но и сложнее:

- skills;
- plugins;
- apps/connectors;
- MCP servers;
- dynamic tools;
- hooks;
- collaboration modes;
- discoverable tools.

Но здесь важно различать семантику.

В `codex` skills — это не эквивалент `tabula`-skills как отдельных процессов. По коду `core-skills`, `skills` и `plugin` видно, что skills здесь в первую очередь:

- prompt/instruction artifacts;
- skill metadata;
- injection rules;
- env var dependencies;
- local/user/repo/system/admin scoped guidance.

Runtime capabilities при этом чаще идут через:

- встроенные tools;
- plugins;
- MCP;
- connectors;
- app-server/tool APIs.

Это сильнее типизировано и лучше встроено в host runtime, но это не та же самая "процессная skill ecosystem", что у `tabula`.

### Вердикт

- по breadth of extension surfaces `codex` богаче;
- по чистоте идеи внешних runtime capabilities `tabula` выглядит элегантнее;
- по реальной продуктовой интеграции расширений `codex` зрелее;
- по openness и language-agnostic extensibility `tabula` потенциально интереснее.

## 5. Multi-agent и delegation

### Tabula

`ANALYSIS.md` справедливо выделяет subagent architecture как одну из сильных идей `tabula`:

- отдельный runtime;
- parent/child separation;
- session scopes;
- depth control;
- сбор результатов обратно.

Проблема не в замысле, а в lifecycle/process/session hardening.

### Codex

`codex` здесь выглядит уже как сильно продуктализированный multi-agent runtime:

- `spawn_agent`, `send_input`, `wait_agent`, `resume_agent`, `close_agent`;
- `AgentRegistry` держит active agent tree;
- есть limits по `max_threads` и `max_depth`;
- дефолтно в config стоят `agent_max_threads = 6` и `agent_max_depth = 1`;
- есть agent path, nickname, role, last task message;
- есть agent jobs и concurrency normalization;
- multi-agent поведение встроено в tool/runtime contract, а не живет как отдельная "почти независимая" подсистема.

Это важно: у `codex` multi-agent не просто возможно, а уже обложено governance и ограничителями.

### Вердикт

По operational maturity и guardrails multi-agent слоя `codex` сильнее.

По concept purity process-isolated subagents `tabula` все еще выглядит очень интересно, но пока менее доведено.

## 6. Persistence, threads и memory pipeline

### Tabula

В `tabula` memory и sessions уже есть, но по `ANALYSIS.md` состояние системы пока слабее именно на lifecycle/state side:

- `/sessions` и snapshot model уязвимы;
- gateway sessions требуют cleanup policy;
- operational truth по процессам и сессиям еще не полностью надежна.

### Codex

У `codex` persistence уже выглядит как отдельный зрелый слой:

- `codex-rollout` пишет JSONL rollouts;
- `codex-state` зеркалит и индексирует metadata в SQLite;
- `thread-store` хранит thread view;
- `app-server` умеет читать, листать, архивировать, возобновлять thread history;
- memory pipeline двухфазный и lease-based.

Особенно показательно `core/src/memories/README.md`:

- Phase 1 claim-ит jobs из state DB;
- есть ownership tokens, lease seconds, retry backoff;
- Phase 2 claim-ит global job;
- selection строится по watermark и usage;
- consolidation делается через отдельный internal agent;
- old artifacts и extension resources аккуратно синхронизируются.

То есть это уже не "memory feature", а полноценная data pipeline внутри agent platform.

### Вердикт

Если сравнивать maturity persistence/state/memory, `codex` снова заметно впереди.

## 7. Hooks и governance

### Tabula

У `tabula` hook-модель шире и амбициознее:

- `before_message`;
- `before_tool_call`;
- `session_start`;
- `before_spawn`;
- `after_*`;
- разные режимы `void/modifying/claiming`.

Это очень мощная идея.

Но `ANALYSIS.md` показывает, что в нескольких местах contract шире реализации:

- modifying payload для tool calls не доведен до конца;
- observer включен в критический path;
- telemetry и policy hooks еще не разведены чисто.

### Codex

У `codex` hook-layer уже и прагматичнее:

- `session_start`;
- `user_prompt_submit`;
- `pre_tool_use`;
- `post_tool_use`;
- `stop`.

По `core/src/hook_runtime.rs` видно, что hooks здесь встроены в runtime аккуратнее:

- есть preview runs;
- hook started/completed events;
- additional context injection;
- block/stop semantics;
- hooks интегрированы с turn/session model.

Это менее "всеобъемлющая" модель, чем у `tabula`, но зато operationally safer.

### Вердикт

- `tabula` сильнее как амбиция policy/event framework;
- `codex` сильнее как приземленный, надежный hook runtime;
- `tabula` имеет смысл сначала довести свой более широкий contract до реальной надежности, а не расширять его дальше.

## 8. Build/test/release/ops story

### Tabula

`ANALYSIS.md` прямо указывает, что install/launch/runtime contract — одна из главных слабостей проекта:

- install scripts и конфиг расходятся;
- launchers и README не полностью совпадают с CLI reality;
- часть gateway/install path сломана;
- packaging еще слабее архитектуры.

### Codex

`codex` здесь заметно сильнее и взрослее:

- есть root `justfile`;
- есть Cargo workspace;
- есть Bazel layer;
- есть GitHub workflows для CI, releases, SDK, Windows, zsh patching, argument-comment lint;
- есть Python и TypeScript SDK;
- есть install docs и release binaries;
- есть support for npm, brew, GitHub Releases;
- есть release-specific tooling и packaging.

Но у этого есть и цена:

- build story сильно сложнее;
- локальный contributor setup тяжелее;
- часть знаний разнесена между Cargo, Bazel, scripts и внешней web-документацией;
- сама codebase уже требует довольно высокого уровня внутреннего контекста.

### Вердикт

По operational packaging и release discipline `codex` далеко впереди.

Но по простоте локального понимания и онбординга `tabula` пока потенциально проще, если стабилизировать ее install/runtime contract.

## Сильные стороны Codex относительно Tabula

Ниже то, в чем `codex` объективно выглядит сильнее по текущему состоянию.

### 1. Единый thread/turn/item control plane

Это, возможно, самый большой разрыв между проектами.

`codex` не заставляет каждый gateway или UI собирать собственную модель разговора. Вместо этого у него уже есть единый control plane, который можно использовать из:

- TUI;
- desktop/app surfaces;
- IDE;
- SDK;
- automation/non-interactive run.

### 2. Намного более зрелый execution/security слой

`ToolOrchestrator`, unified exec, sandbox managers, execpolicy, guardian, shell escalation и cross-platform sandboxing вместе дают `codex` тот уровень operational safety, которого у `tabula` сейчас нет.

### 3. Реально собранный persistence/state layer

Rollout + SQLite + thread store + memory pipeline делают `codex` платформой, у которой уже есть внутренняя data backbone.

### 4. Гораздо более зрелая product shell

В `codex` уже собраны:

- CLI;
- TUI;
- app-server;
- exec-server;
- SDK;
- MCP server;
- config and requirements management.

У `tabula` эта оболочка пока слабее ядра.

### 5. Ограничители и guardrails для сложных возможностей

Это касается:

- multi-agent;
- approvals;
- network policy;
- feature flags;
- cloud requirements;
- config constraints.

В `tabula` многие идеи уже есть, но guardrails пока слабее.

## Где Tabula все еще выглядит лучше или чище

Важно не потерять баланс: `codex` сильнее по зрелости, но не по всем архитектурным ставкам.

### 1. Process isolation как часть основной модели

У `tabula` skills — реальные runtime-компоненты, а не только metadata/instructions/plugins.

Это дает:

- лучшую языковую свободу;
- более естественную изоляцию;
- более явную модульность возможностей.

### 2. Более чистое разделение ядра и capabilities

У `tabula` граница между kernel и skill ecosystem концептуально проще.

У `codex` capabilities распределены по многим поверхностям:

- core tools;
- plugins;
- skills;
- MCP;
- apps;
- connectors;
- hooks;
- dynamic tools.

Это богато, но тяжелее для ментальной модели.

### 3. Меньшая архитектурная цена входа

Даже по одному только масштабу видно, что `codex` уже очень большой.

В `tabula` пока проще понять, где заканчивается ядро и начинаются расширения.

Если `tabula` доведет operational maturity, она может сохранить это преимущество.

## Главные архитектурные риски Codex

Ниже не "баги", а системные риски, которые видны по текущей архитектуре.

### P1. Core/TUI/app-server уже испытывают давление размеров

Показательные факты:

- `core/src/codex.rs` около 8211 строк;
- `tui/src/app.rs` около 11534 строк;
- `app-server/src/bespoke_event_handling.rs` около 4630 строк;
- `core/src/config/mod.rs` около 2372 строк.

Это не автоматически плохо, но это сильный сигнал, что несколько ключевых центров системы уже стали "gravity wells", куда стекаются слишком многие обязанности.

Риск:

- рост сложности ревью и рефакторинга;
- сложнее изолировать поведение;
- выше вероятность boundary erosion.

### P1. Extension surface слишком широкая и частично пересекающаяся

В `codex` одновременно существуют:

- skills;
- plugins;
- connectors/apps;
- MCP;
- dynamic tools;
- hooks;
- collaboration modes;
- tool search/suggest surfaces.

Это мощно, но дороговато концептуально.

Риск:

- пользователю и даже разработчику сложнее понять "правильную" точку расширения;
- разные механизмы могут начать дублировать друг друга;
- config и policy становятся сложнее.

### P1. Система уже больше похожа на product operating environment, чем на библиотеку

Хотя `codex-core` декларируется как reusable business logic, по факту общая система уже очень завязана на:

- product surfaces;
- OpenAI ecosystem;
- app-server contract;
- approvals and policy model;
- rollout/state/memory infrastructure.

Inference: это делает `codex` очень сильным как продукт, но менее нейтральным как "общий агентный kernel".

### P2. In-process extensibility имеет более широкий blast radius, чем у Tabula

`tabula` чаще платит operational complexity на межпроцессных границах.

`codex` чаще платит за счет того, что очень много логики живет внутри одного host runtime.

Риск:

- проще строить сквозные фичи;
- но сложнее локализовать сложные деградации, если они происходят в самом core.

### P2. Repo-local docs слабее полной сложности системы

Это не означает плохую документацию вообще, но внутри репозитория значительная часть `docs/*.md` — это короткие ссылки на внешнюю документацию.

Практический эффект:

- по одному только локальному репозиторию всю систему понять тяжелее;
- часть operational knowledge вынесена наружу;
- offline code-reading дает хороший, но не полностью самодостаточный контекст.

## Что Tabula стоит заимствовать из Codex

Ниже то, что я бы реально считал полезным и практичным для `tabula`.

### 1. Единый control plane вокруг thread/turn/item

Не обязательно копировать JSON-RPC и весь app-server, но сама идея очень сильная:

- одна canonical session model;
- один lifecycle для UI/API/automation;
- один способ читать, резюмировать и возобновлять историю.

Сейчас это один из самых больших пробелов `tabula`.

### 2. Центральный tool orchestrator

`tabula` стоит иметь отдельный слой, который централизует:

- approval;
- sandbox selection;
- retry semantics;
- escalation;
- network rules;
- telemetry around tool decisions.

Это даст гораздо больше надежности, чем попытка держать governance только через hooks.

### 3. Persisted rollout + indexed state DB

`codex` очень хорошо показывает, что raw transcript и indexed state — это разные, но взаимодополняющие артефакты.

Для `tabula` это означало бы:

- безопаснее resume/replay;
- лучше session introspection;
- лучше observability;
- нормальная база для memories и background tasks.

### 4. Backpressure, bounded queues и graceful restart

Это особенно важно для будущего `gateway-api` и любых внешних клиентов.

У `tabula` сейчас как раз gateway concurrency и lifecycle — слабое место.

### 5. Более строгий config/policy слой

В `codex` видно, насколько полезны:

- layered config;
- managed requirements;
- constrained settings;
- explicit approval/sandbox/web-search restrictions.

Для `tabula` это могло бы резко повысить надежность без переписывания архитектуры.

### 6. Guardrails для multi-agent

Не сама идея subagents, а именно:

- depth limits;
- thread limits;
- explicit ownership;
- registry of live agents;
- clear final-state handling.

## Что Tabula не стоит копировать из Codex целиком

### 1. Монорепо-сложность такого же масштаба

91 crate и очень широкий product perimeter — это не бесплатная сила.

Для `tabula` повторять это слишком рано и слишком дорого.

### 2. Максимальную централизацию всего runtime в одном host process

Это одна из причин зрелости `codex`, но это же лишит `tabula` одной из ее лучших идей:

- capabilities как внешние процессы;
- language-agnostic runtime seams;
- чистое отделение ядра от навыков.

### 3. Слишком много конкурирующих extension surfaces

`tabula` лучше сохранять понятную основную точку расширения, чем быстро обрастать параллельными моделями capabilities.

### 4. Product sprawl раньше hardening

Для `tabula` лучший следующий шаг все еще тот же, что зафиксирован в `ANALYSIS.md`:

- install/bootstrap;
- process/session model;
- gateway concurrency;
- hooks contract;
- observability.

Копировать большой feature perimeter `codex` до этого просто опасно.

## Архитектурный вердикт

Если смотреть честно и прагматично:

- `codex` сегодня выглядит как значительно более зрелая и надежная система;
- `codex` особенно силен там, где `tabula` сейчас слаба:
  - control plane;
  - session/state model;
  - sandbox and approvals;
  - persistence;
  - operational packaging.

Но при этом:

- `tabula` не выглядит "неправильной" архитектурно;
- наоборот, у нее есть очень сильная и более чистая ставка на kernel + external skills;
- проблема `tabula` не в общей идее, а в том, что operational hardening пока не догнал архитектуру.

Если свести к одной фразе:

`codex` — это зрелая продуктовая агентная платформа для coding workflows, а `tabula` — перспективная агентная платформа с более чистым microkernel-замыслом, которой сейчас не хватает именно инженерной доводки системных слоев.

## Практический вывод для Tabula

Самый полезный способ использовать этот сравнительный анализ:

1. Не пытаться перепридумать `tabula` как копию `codex`.
2. Сохранить сильные стороны `tabula`:
   - Go-kernel;
   - external skills;
   - process isolation;
   - language openness.
3. Заимствовать из `codex` только самые ценные infrastructure patterns:
   - canonical thread/turn/item model;
   - central tool orchestration;
   - persisted rollouts + indexed state DB;
   - bounded queues and graceful shutdown;
   - stricter policy/config/approval model;
   - agent guardrails.

То есть правильный ориентир такой:

- `tabula` не должна стать "маленьким codex";
- `tabula` должна стать своей собственной платформой, но с качеством control-plane и operational hardening, близким к лучшим слоям `codex`.
