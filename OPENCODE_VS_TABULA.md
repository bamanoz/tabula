# OpenCode vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `opencode`: `/Users/mak/src/opencode`

За основу по `tabula` взят уже подготовленный `ANALYSIS.md`.

По `opencode` анализ основан на:

- структуре монорепозитория;
- `README.md`, `package.json`, `AGENTS.md`, `turbo.json`;
- ключевых runtime-модулях `packages/opencode/src/session/*`, `tool/*`, `plugin/*`, `provider/*`, `server/*`, `storage/*`, `control-plane/*`, `mcp/*`, `snapshot/*`, `project/*`;
- package surfaces для `packages/app`, `packages/desktop`, `packages/plugin`, `sdks/vscode`;
- спецификациях `specs/project.md`, `specs/v2/session.md`;
- тестовом дереве `packages/opencode/test/*`.

Важно: в отличие от `ANALYSIS.md` по `tabula`, здесь не делался полный прогон тестов `opencode`, потому что в локальной копии сейчас нет установленных зависимостей (`node_modules` отсутствует). Поэтому это глубокий архитектурный и кодовый анализ, а не полная runtime-валидация сборки.

## Короткий вывод

`opencode` и `tabula` решают очень близкую тему, но делают на нее принципиально разные архитектурные ставки.

- `tabula` сегодня ближе к agent microkernel:
  - Go-ядро;
  - собственный wire protocol;
  - Python-skills как отдельные процессы;
  - hooks / gateways / drivers / subagents как внешние runtime-компоненты.
- `opencode` ближе к интегрированной local-first agent platform:
  - один TypeScript/Bun host runtime;
  - сильная модель state/persistence;
  - session/event sourcing;
  - in-process plugins;
  - богатый control plane;
  - несколько клиентов и device surfaces.

Если очень коротко:

- `opencode` заметно сильнее как целостная продуктовая платформа и control plane;
- `tabula` чище и элегантнее как маленькая мультипроцессная агентная ОС;
- `opencode` уже выиграл в durability, session management, permission UX, workspace abstractions и операторской зрелости;
- `tabula` все еще сильнее в runtime isolation, multi-language extensibility и архитектурной ясности базовой идеи.

Главный практический вывод:

- `tabula` не стоит пытаться переписать “под opencode” целиком;
- но `tabula` очень стоит заимствовать из `opencode` слой durable state, session serialization, permission engine, manifest-first extensibility и project/workspace abstractions.

## Что на самом деле представляет собой каждый проект

## Tabula

Согласно `ANALYSIS.md`, `tabula` построена как собственная агентная платформа:

- Go-kernel отвечает за маршрутизацию, жизненный цикл процессов, сессии и протокол;
- `boot.py` собирает prompt, tools, commands, memory и skill discovery;
- Python-skills реализуют драйверы, hooks, gateways, tools и subagents;
- связи между частями идут через WebSocket protocol и session-oriented kernel.

Это архитектура платформы, где разделение на kernel и внешние процессы является главным design choice.

## OpenCode

`opencode` устроен иначе. Это большой Bun/TypeScript монорепозиторий, где:

- в корне живет workspace на `bun`;
- в `packages/opencode` находится основной runtime;
- отдельные пакеты обслуживают app/web/desktop/plugin/sdk/ui/storybook/enterprise/slack и другие surfaces;
- серверный runtime написан на `Effect`, `Hono`, `Drizzle`, SQLite и AI SDK;
- session state, permission state, project state и sync events хранятся централизованно;
- расширение идет через plugins, config-driven agents, skills, custom tools и workspace adaptors.

На уровне масштаба это уже не “ядро + несколько skills”, а цельная agent platform shell. По статике дерева это хорошо видно:

- в `packages/` 19 top-level пакетов;
- только в `packages/opencode/src` около 356 `.ts` файлов;
- в `packages/opencode/test` около 153 тестовых файлов.

То есть `opencode` уже живет как крупный продуктовый стек, а не как компактный runtime.

## Самое важное архитектурное различие

Разница между проектами не в языке, а в выбранной границе системы.

### Tabula

Граница проходит между:

- kernel;
- протоколом;
- внешними skill-процессами.

Это делает систему более распределенной даже локально.

### OpenCode

Граница проходит вокруг одного host runtime, внутри которого уже живут:

- sessions;
- tools;
- permissions;
- plugins;
- providers;
- pty;
- sync;
- project/workspace state;
- server/API;
- часть UI и desktop integration.

Это делает систему более цельной и консистентной, но менее изолированной.

Из этого следует почти все остальное:

- `tabula` сильнее там, где важны isolation и heterogenous runtime seams;
- `opencode` сильнее там, где нужны сквозные контракты, durable state и богатый control plane.

## Глубокое сравнение

## 1. Архитектурная ставка

### Tabula

Ставка `tabula`:

- маленькое ядро;
- отдельные skill-процессы;
- multi-language extensibility;
- protocol-first orchestration.

Плюсы:

- хороший conceptual separation;
- естественная process isolation;
- проще встраивать навыки на другом языке;
- kernel можно держать относительно компактным.

Минусы:

- стыки process/session/protocol становятся самыми уязвимыми местами;
- durability и unified state нужно строить отдельно;
- gateway lifecycle легко “расползается” по нескольким skill-реализациям.

### OpenCode

Ставка `opencode`:

- один host runtime;
- strong typing на уровне почти всей платформы;
- инъекция сервисов через `Effect` layers;
- единая модель sessions/projects/providers/tools/plugins.

Плюсы:

- очень сильная сквозная согласованность;
- одна общая модель state;
- проще строить feature layers, которые проходят через всю систему;
- меньше случайных разрывов между API, persistence и UI.

Минусы:

- резкий рост сложности;
- сильнее blast radius при ошибке в runtime;
- плагины и расширения сидят внутри того же процесса;
- кодовая база уже требует большого количества service boundaries, flags и инфраструктурных соглашений.

### Вердикт

- по архитектурной элегантности базовой идеи `tabula` мне все еще кажется чище;
- по зрелости platform shell `opencode` существенно впереди.

## 2. Runtime-модель и жизненный цикл

### Tabula

`tabula` делает orchestration через kernel и внешние процессы. Это дает реальную изоляцию, но уже сейчас в `ANALYSIS.md` видно, что session/process bookkeeping является одним из самых болезненных мест.

### OpenCode

`opencode` делает почти все внутри одного runtime, но компенсирует это более сильной внутренней дисциплиной:

- `SessionRunState` сериализует активность на session;
- `SessionProcessor` держит единый поток обработки LLM/tool событий;
- `SessionPrompt` связывает prompt assembly, compaction, tools, MCP, LSP и permissions;
- `SessionStatus` и `SessionSummary` встроены в тот же жизненный цикл.

Это важное отличие: в `opencode` concurrency и lifecycle не “оставлены gateway-слоям”, а встроены в core session runtime.

Практический вывод:

- там, где `tabula` сегодня страдает от session-level races в gateway-слое, `opencode` уже выносит это на уровень платформенного контракта;
- это один из самых ценных слоев, который `tabula` стоит перенять концептуально.

## 3. Sessions, state и durability

Это, пожалуй, самая большая структурная победа `opencode` над текущей `tabula`.

### Что есть в OpenCode

У `opencode` session model заметно глубже:

- SQLite persistence через `Drizzle`;
- event-sourced слой `SyncEvent`;
- projectors для materialized state;
- session/message/part tables;
- session diff/summary/revert/compact;
- sync log для replay;
- project/workspace привязка к сессии;
- snapshot-слой на базе отдельного git-хранилища.

Особенно сильные места:

- `packages/opencode/src/session/session.ts`
- `packages/opencode/src/session/projectors.ts`
- `packages/opencode/src/sync/*`
- `packages/opencode/src/session/summary.ts`
- `packages/opencode/src/session/compaction.ts`
- `packages/opencode/src/snapshot/snapshot.ts`

Сильнейшая идея здесь не просто в том, что есть SQLite. Главное, что mutation path и replay path уже думаются как одна система.

Это дает несколько свойств, которых `tabula` сейчас не хватает:

- session state переживает процессы и клиенты;
- есть replay model, а не только “текущее состояние в памяти”;
- workspace restore становится возможным не через ad hoc экспорт, а через event stream;
- diff/snapshot/revert живут рядом с session model, а не отдельно от нее.

### Где Tabula проигрывает

В `ANALYSIS.md` у `tabula` наоборот главные риски как раз вокруг:

- session/process accounting;
- snapshot/process bookkeeping mismatch;
- gateway-owned lifecycle;
- слабого cleanup/TTL/session serialization.

### Вердикт

Если выбирать один слой, который `tabula` больше всего должна заимствовать у `opencode`, это именно durable session architecture:

- event log;
- projectors;
- persisted session state;
- session-level busy/idle semantics;
- встроенный diff/snapshot/summary lifecycle.

## 4. Модель расширения

### Tabula

Расширение в `tabula` идет через skills:

- `SKILL.md`;
- `run.py`;
- tool skills;
- gateways;
- hooks;
- drivers;
- bundles.

Это очень сильная платформа для независимых runtime-компонентов.

### OpenCode

У `opencode` расширение идет сразу по нескольким осям:

- config-defined agents;
- config-defined commands;
- config-defined skills;
- plugins из npm или file targets;
- custom tools из project directories;
- workspace adaptors;
- provider hooks;
- TUI plugins.

Плюс к этому есть отдельный plugin SDK в `packages/plugin`.

Сильная сторона `opencode` не только в наличии plugins, а в том, что extensibility сделана manifest-first и typed-first:

- plugin target можно резолвить и валидировать;
- есть compatibility checks;
- package exports трактуются как формальный контракт;
- server/tui surfaces разведены;
- tools получают типизированный SDK context.

### Где OpenCode слабее

Главная цена этой модели:

- почти все расширения живут in-process;
- `Plugin.trigger(...)` вызывает hooks последовательно и синхронно;
- plugin latency попадает в critical path;
- плохой или тяжелый plugin влияет на основной runtime напрямую.

То есть `opencode` выигрывает в согласованности extensibility, но проигрывает `tabula` в fault isolation.

### Вердикт

- по quality of extensibility contracts `opencode` уже сильнее;
- по runtime isolation `tabula` остается архитектурно лучше.

Лучший путь для `tabula`:

- не отказываться от process boundary;
- но сделать metadata/discovery/install/compatibility настолько же явными, как у `opencode`.

## 5. Tools, skills и subagents

### OpenCode

`opencode` собрал tool system в единый typed registry:

- builtin tools;
- custom file-based tools;
- plugin tools;
- skill tool;
- task tool;
- bash/read/edit/write/apply_patch/webfetch/websearch/lsp и т.д.

Это дает очень сильную целостность:

- один контекст исполнения;
- единый truncate/output contract;
- единый permission path;
- единый способ включать и скрывать инструменты.

Отдельно сильна модель `task`:

- subagent создается как дочерняя session;
- наследует controlled permission subset;
- имеет resumable `task_id`;
- интегрирован в тот же session store.

Это менее изолированно, чем subagent-процессы `tabula`, но гораздо лучше связано с общей session architecture.

### Tabula

У `tabula` subagents сильны именно как реальные отдельные runtime entities. Это концептуально красивее и безопаснее, но lifecycle сложнее и уже сейчас бьет по надежности.

### Вердикт

- `tabula` сильнее в “настоящей” изоляции subagents;
- `opencode` сильнее в том, что subagents являются частью общей durable model, а не отдельным островом.

Для `tabula` здесь идеальный урок такой:

- сохранить отдельные процессы;
- но сделать child sessions и subagent state полноценной persisted сущностью ядра.

## 6. Permissions и governance

Здесь `opencode` выглядит очень сильно.

Что есть в `opencode`:

- явный permission service;
- `allow` / `deny` / `ask`;
- `once` / `always` / `reject`;
- persisted project-level approvals;
- permission requests как first-class events;
- отдельный `external_directory` permission;
- анализ bash-команд для вычисления patterns и scope ask.

Особенно важно, что это не просто “hook somewhere in the stack”, а встроенный продуктовый workflow.

`tabula`, согласно `ANALYSIS.md`, имеет интересную hook/policy architecture как идею, но пока не дотягивает в operational maturity:

- modifying hooks не всегда согласованы с runtime behavior;
- policy path и observability path местами смешаны;
- governance концептуально сильная, но продуктово и инфраструктурно еще не закрыта.

### Вердикт

`opencode` уже выиграл не только в policy mechanism, но и в UX/operability permission-системы.

Для `tabula` полезно заимствовать:

- persistent approval model;
- явные permission events;
- session-aware approval state;
- более узкие permission types вместо общего “hook can intervene”.

## 7. Providers, auth и внешние интеграции

### OpenCode

Provider layer у `opencode` очень широкий и зрелый:

- много встроенных AI SDK провайдеров;
- auth workflows;
- custom provider transforms;
- model discovery;
- OpenAI/Copilot/GitLab/Bedrock/OpenRouter и др.;
- plugin hooks для provider expansion.

Плюс MCP сделан как полноценный сервис, а не просто внешний скрипт:

- stdio/SSE/HTTP transports;
- OAuth flow;
- browser open fallback;
- prompt/resource/tool integration;
- lifecycle и status tracking.

Есть и ACP implementation как отдельный formal integration layer.

### Tabula

У `tabula` драйверы и MCP skills реализованы архитектурно неплохо, но по breadth и продуктовой завершенности этот слой пока заметно тоньше.

### Вердикт

Если говорить про ecosystem readiness, `opencode` далеко впереди.

Но при этом важно:

- `opencode` достигает этого ценой большой зависимости от TypeScript/Bun/AI SDK stack;
- `tabula` может не хотеть тащить весь этот объём ради собственных целей.

Значит, заимствовать стоит не breadth, а discipline:

- единый provider contract;
- stronger auth story;
- formal MCP lifecycle;
- typed status/errors.

## 8. Gateway, control plane и клиенты

Здесь `opencode` уже явно не просто CLI-агент, а platform shell.

Что у него есть:

- Hono-based server;
- instance routes;
- control-plane routes;
- WebSocket PTY;
- typed OpenAPI descriptions;
- app/web/desktop/TUI surfaces;
- VS Code SDK surface;
- workspace adaptors;
- remote/local workspace targets.

Очень важный слой здесь: `project` и `workspace`.

В `opencode` сессия живет не просто в “какой-то директории”, а внутри более общей модели:

- проект;
- worktree;
- sandbox;
- workspace;
- удаленное восстановление session events в workspace.

Для `tabula` это важный недостающий уровень абстракции. Сейчас `tabula` сильна в kernel/protocol, но заметно слабее в том, как platform-level entities названы и сохранены.

### Вердикт

`opencode` выигрывает у `tabula` не столько числом клиентов, сколько тем, что control plane уже является отдельной архитектурной осью.

`tabula` не обязана повторять web/desktop/app surfaces, но ей очень полезно иметь более явные сущности:

- project;
- workspace;
- client session;
- persisted session ownership;
- sync/export/replay contracts.

## 9. Тестовый контур и инженерная зрелость

### Tabula

У `tabula` тесты по широте уже хорошие, и это отмечено в `ANALYSIS.md`, но реальная operational зрелость пока все еще отстает от архитектурного замысла.

### OpenCode

По структуре тестов `opencode` видно, что команда тестирует именно платформенные контракты:

- session behavior;
- tool behavior;
- MCP lifecycle;
- plugin loading;
- permissions;
- PTY;
- workspaces;
- providers;
- storage;
- sync;
- server routes.

Это хороший признак: тестируется не только “основной happy path”, а вся система как платформа.

Отдельно хороший инженерный сигнал:

- root package прямо запрещает запуск тестов из корня;
- package-level test discipline формализована;
- `AGENTS.md` фиксирует testing conventions для контрибьюторов.

### Но есть и обратная сторона

У `opencode` уже настолько большой surface area, что полноценная уверенность требует тяжелой инфраструктуры:

- Bun;
- native dependencies;
- desktop stacks;
- Tauri;
- возможно browser/e2e paths.

То есть зрелость выше, но и стоимость сопровождения выше.

## 10. Главные риски и слабые места OpenCode

Ниже не “критика ради критики”, а именно то, что я считаю архитектурной ценой его подхода.

### P1. Слишком большой blast radius у in-process extensibility

Plugins, custom tools, provider hooks и часть skill-loading живут внутри одного host runtime.

Это значит:

- ошибка в расширении бьет по центральному процессу;
- latency hook-а попадает в critical path;
- isolation хуже, чем у `tabula`.

### P1. Сложность платформы уже очень высокая

`opencode` сегодня тянет:

- core runtime;
- server;
- control plane;
- sync;
- snapshots;
- MCP;
- ACP;
- PTY;
- desktop;
- app;
- plugin SDK;
- remote workspace abstractions.

Это мощно, но такой стек тяжелее держать под архитектурным контролем, чем `tabula`.

### P2. Много feature flags и experimental seams

По коду видно существенное число `OPENCODE_EXPERIMENTAL_*` путей.

Это не плохо само по себе, но это признак того, что часть границ еще движется. Для платформы такого размера это означает:

- больше combinatorial complexity;
- больше контрактов, которые могут разойтись между клиентами;
- сложнее объяснить platform behavior внешним контрибьюторам.

### P2. Документация периферийных пакетов местами отстает от основного ядра

В репозитории заметны следы template/readme drift у некоторых package-level README. Это не проблема core runtime, но хороший сигнал того, что периферийные поверхности уже труднее поддерживать синхронно.

## Где Tabula все еще архитектурно лучше

Это важная часть, потому что по продуктовой зрелости `opencode` легко переоценить.

### 1. Чище базовая идея

`tabula` проще объяснить одной фразой:

- kernel orchestrates independent skills.

`opencode` объясняется дольше, потому что он уже не один runtime seam, а набор связанных платформенных слоев.

### 2. Лучше isolation model

Отдельные processes в `tabula` дают:

- естественный fault containment;
- сильную language independence;
- более честные boundary contracts.

### 3. Меньше platform gravity

`tabula` пока не затянута в обслуживание большого числа surfaces и therefore может быстрее улучшать core architecture, если сконцентрироваться.

### 4. Hooks как идея у Tabula потенциально богаче

Несмотря на текущие gaps, идея `tabula` с hook categories и security/domain/observability separation концептуально очень сильная. В `opencode` многое решено прагматично и хорошо, но именно как “policy architecture” `tabula` может со временем стать интереснее.

## Что Tabula стоит перенять из OpenCode

Ниже то, что я бы рекомендовал заимствовать в первую очередь.

### 1. Durable session core

Нужны:

- SQLite или другой локальный durable store;
- message/part/session tables;
- event log;
- projector model для materialized state.

### 2. Session run-state serialization

Нужен платформенный контракт “одна активная session operation за раз”, а не попытка держать это на уровне каждого gateway отдельно.

### 3. Permission service как first-class subsystem

Нужны:

- persisted approvals;
- explicit ask/allow/deny model;
- session-aware permission requests;
- возможно separate permission objects для tool/bash/external_directory.

### 4. Manifest-first extensibility

`tabula` стоит добавить более явный metadata layer для skills/plugins/hooks:

- совместимость;
- entrypoints;
- версии;
- capability declarations;
- install/discovery policy.

### 5. Project/workspace abstraction

Даже без удаленных workspaces в полном виде `tabula` выиграет, если введет более формальные сущности:

- project;
- worktree/root;
- session owner;
- child session;
- replay/export target.

### 6. Snapshot/diff/revert как часть session architecture

Снимки должны быть не отдельной удачной функцией, а частью общей модели сессии и ее истории.

## Что Tabula не стоит копировать

### 1. Не стоит копировать весь product surface

`tabula` не нужно сейчас тащить:

- desktop;
- web app;
- extension ecosystem;
- remote workspace stack;
- TUI plugin system.

Это преждевременно, пока core reliability не закрыта.

### 2. Не стоит отказываться от process isolation

Именно здесь `tabula` может остаться уникально сильной. Заимствовать нужно metadata и lifecycle discipline, а не переходить целиком на in-process plugins.

### 3. Не стоит повторять весь complexity budget `opencode`

`Effect`-style service graph, большой monorepo и множество surfaces уместны для продукта масштаба `opencode`, но они легко перегрузят `tabula`, если брать их без разбора.

## Практическая формула для Tabula после этого сравнения

Если переводить сравнение в конкретную стратегию, я бы предложил такую последовательность.

### Этап 1. Удержать сильную сторону Tabula

Сохраняем:

- Go kernel;
- Python skills;
- multiprocess model;
- protocol-centric orchestration.

### Этап 2. Добавить то, чего не хватает

Добавляем:

- durable session store;
- session event log;
- projector/materialized state;
- child-session persistence;
- session busy/idle/run-state.

### Этап 3. Укрепить control plane

Добавляем:

- более формальную project/workspace model;
- typed API surface;
- лучший install/config/discovery contract;
- permission subsystem как отдельный слой.

### Этап 4. Только потом расширять surface area

Новые клиенты и продуктовые оболочки стоит строить только после того, как ядро унаследует у `opencode` зрелость state/lifecycle, не теряя своей process-isolated архитектуры.

## Архитектурный вердикт

`opencode` сегодня выглядит сильнее `tabula` как целостная agent platform:

- лучше state management;
- лучше permission/governance UX;
- лучше control plane;
- лучше project/workspace abstractions;
- лучше durability и replay story;
- богаче интеграции и клиентские поверхности.

Но это не делает `tabula` слабой идеей. Скорее наоборот:

- `tabula` по-прежнему очень сильна как маленькая, честная, modular agent runtime;
- ее проблема сейчас не в архитектурной ставке, а в operational maturity;
- в этом смысле `opencode` показывает не “другую правильную идею”, а следующий эволюционный этап зрелости платформы.

Если коротко:

- `opencode` — это образец того, как выглядит зрелый platform shell вокруг локального coding agent;
- `tabula` — это образец того, как может выглядеть более чистое и изолированное agent core;
- лучшая стратегия для `tabula` — не стать копией `opencode`, а встроить его сильные организационные и stateful решения внутрь своей более удачной multiprocess-архитектуры.

## Итог в одной фразе

`opencode` выигрывает у `tabula` как продуктовая платформа и durable control plane, но `tabula` все еще может выиграть как более чистая и изолированная агентная ОС, если быстро подтянет persistence, session lifecycle и permission architecture до уровня, который `opencode` уже демонстрирует.
