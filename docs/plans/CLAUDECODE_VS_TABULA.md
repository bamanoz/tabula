# Claude Code vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `claude-code`: `/Users/mak/src/claude-code`

Цель документа:

1. Зафиксировать глубокое архитектурное сравнение `claude-code` и `tabula`.
2. Использовать уже подготовленный `ANALYSIS.md` как базу по `tabula`, а не пересобирать оценку с нуля.
3. Понять, на каком слое проекты реально сопоставимы, а на каком уже решают разные классы задач.
4. Выделить, что именно `tabula` стоит заимствовать из `claude-code`, а что копировать было бы стратегической ошибкой.

За основу по `tabula` взят `ANALYSIS.md`, дополнительно сверенный по ключевым файлам:

- `boot.py`
- `cmd/tabula/main.go`
- `internal/kernel/tool_service.go`
- `internal/kernel/process_manager.go`
- `internal/kernel/policy.go`
- `internal/kernel/snapshot.go`
- `skills/lib/driver_runtime.py`
- `skills/lib/subagent_runtime.py`
- `skills/gateway-cli/run.py`
- `skills/gateway-api/run.py`
- `skills/gateway-telegram/run.py`

По `claude-code` анализ основан на структуре snapshot-репозитория и чтении ключевых runtime-модулей:

- `src/entrypoints/cli.tsx`
- `src/main.tsx`
- `src/QueryEngine.ts`
- `src/query.ts`
- `src/Tool.ts`
- `src/tools.ts`
- `src/hooks/toolPermission/PermissionContext.ts`
- `src/tools/AgentTool/AgentTool.tsx`
- `src/tasks/LocalAgentTask/LocalAgentTask.tsx`
- `src/skills/loadSkillsDir.ts`
- `src/tools/SkillTool/SkillTool.ts`
- `src/state/AppStateStore.ts`
- `src/screens/REPL.tsx`
- `src/bridge/sessionRunner.ts`
- `src/bridge/bridgeMain.ts`
- `src/remote/RemoteSessionManager.ts`
- `src/server/types.ts`
- `src/server/directConnectManager.ts`
- `src/server/createDirectConnectSession.ts`
- `src/cli/structuredIO.ts`
- `src/services/plugins/PluginInstallationManager.ts`
- `src/utils/plugins/pluginLoader.ts`
- `src/plugins/builtinPlugins.ts`
- `src/services/teamMemorySync/index.ts`
- `src/entrypoints/mcp.ts`

Важно: локальный `claude-code` здесь выглядит именно как source snapshot, а не как полный buildable monorepo. В корне нет обычных package/build-файлов, поэтому это прежде всего архитектурный анализ по коду, а не полноценная валидация install/test path.

## Короткий вывод

`claude-code` и `tabula` пересекаются по теме агентного coding assistant-а, но архитектурно это проекты разного уровня.

- `tabula` сегодня ближе к компактной агентной платформе или "agent microkernel":
  - маленькое Go-ядро;
  - собственный wire protocol;
  - Python-skills как отдельные процессы;
  - boot-time assembly prompt/tools/commands;
  - hooks, gateways и subagents как внешние модули.
- `claude-code` ближе к полноценной интегрированной среде выполнения агентного продукта:
  - единый TypeScript/Bun host;
  - огромный typed tool/command/runtime layer;
  - REPL/UI/state/task/bridge/server/remote/plugin ecosystem;
  - сложная permission и session orchestration;
  - богатая operator/developer story.

Если очень коротко:

- `claude-code` намного сильнее как продуктовая, user-facing и operationally mature система;
- `tabula` чище и интереснее как компактное платформенное ядро;
- `claude-code` не просто "более развитая версия tabula", а проект на другом слое абстракции;
- `tabula` не стоит пытаться превратить в `claude-code` целиком;
- но `tabula` очень стоит позаимствовать у `claude-code` отдельные зрелые слои: typed tool contracts, permission pipeline, first-class task/session lifecycle, plugin metadata и operator tooling.

Главный практический вывод:

- если цель `tabula` — быть хакуемой, модульной, process-oriented агентной платформой, ее базовая ставка остается правильной;
- если цель — догнать `claude-code` как end-user coding product, одного hardening ядра будет мало: потребуется отдельный большой продуктовый слой поверх ядра.

## Что на самом деле представляет собой каждый проект

## Tabula

`tabula` организована вокруг собственного kernel/runtime:

- `cmd/tabula/main.go` поднимает сервер и one-shot режим;
- `boot.py` собирает system prompt, tools, slash-команды и spawn-конфигурацию;
- Go-ядро маршрутизирует клиентов, сессии, процессы и hooks;
- Python-skills реализуют драйверы, gateway-и, memory, MCP, permissions, observer и subagents;
- все компоненты соединены собственным WebSocket-протоколом `connect -> join -> init -> message/tool loop`.

Это архитектура платформенного ядра.

## Claude Code

`claude-code` в этом snapshot-е выглядит как единый большой host runtime:

- CLI bootstrap и fast-paths живут в `src/entrypoints/cli.tsx`;
- основной продуктовый CLI/command layer сконцентрирован в `src/main.tsx`;
- модельный цикл живет в `src/QueryEngine.ts` и `src/query.ts`;
- tool system типизирован через `src/Tool.ts`, `src/tools.ts` и десятки tool-модулей;
- permissions, state, REPL, tasks, bridge, remote sessions, server mode, plugins, MCP, memories и scheduled work живут внутри одного большого TypeScript-мира.

Это архитектура интегрированной продуктовой среды, а не только agent kernel.

## Самое важное архитектурное различие

`tabula` и `claude-code` отличаются не только количеством фич, а самой ставкой.

### Tabula

Мысленно это:

- kernel;
- protocol;
- process supervision;
- pluggable skills;
- gateways как внешние клиенты ядра.

То есть `tabula` ближе к "agent OS core".

### Claude Code

Мысленно это:

- application runtime;
- typed tool platform;
- session/task/state machine;
- UI/REPL/bridge/server surfaces;
- plugin and skill ecosystem;
- user/product operations layer.

То есть `claude-code` ближе к "integrated agent product runtime".

Отсюда вытекает почти все остальное:

- `tabula` сильнее там, где важны модульность, process isolation и простая модель ядра;
- `claude-code` сильнее там, где важны цельный UX, сложная permission orchestration, background workflows, remote control и богатый operational shell.

Это не совсем сравнение "кто лучше". Это сравнение "на каком слое каждый проект делает главную ставку".

## Масштаб и цена сложности

По грубому локальному счету разница очень большая:

- `tabula` содержит около 100 `.go/.py` исходников и порядка 19k строк Go/Python-кода;
- `claude-code` snapshot в `src/` содержит около 1884 `.ts/.tsx` файлов и около 512k строк TypeScript-кода;
- в `tabula` около 39 test-файлов;
- в `claude-code` snapshot-е тестовая структура в корне не материализована так же явно, как runtime-дерево.

Это важно не как trivia, а как архитектурный фактор:

- маленький размер `tabula` пока дает ей редкую способность быстро менять форму;
- огромный размер `claude-code` означает, что в нем уже закодировано гораздо больше operational knowledge, но цена изменений и когнитивная нагрузка несопоставимо выше.

Поэтому "почему в `claude-code` есть X, а в `tabula` нет" очень часто объясняется не качеством инженеров, а тем, что это проект другого бюджета сложности.

## Глубокое сравнение

## 1. Runtime-модель

### Tabula

Runtime `tabula` строится вокруг kernel + внешних процессов:

- skill подключается к ядру как отдельный клиент;
- gateway тоже отдельный клиент;
- driver — отдельный процесс;
- subagent — отдельный процесс;
- kernel ведет sessions, processes, hooks и dispatch.

Плюсы:

- настоящая изоляция компонентов;
- естественная модель для heterogenous skills;
- можно смешивать языки и роли без утяжеления ядра;
- протокол является реальной границей системы, а не внутренней договоренностью.

Минусы:

- каждая межпроцессная граница создает lifecycle-риск;
- ошибки контрактов бьют сильно, потому что ядро и skill расходятся во времени;
- gateway-слой вынужден сам собирать session/driver lifecycle;
- часть проблем из `ANALYSIS.md` именно об этом: snapshot processes, cleanup, gateway concurrency, install/runtime drift.

### Claude Code

Runtime `claude-code` в основном in-process:

- `QueryEngine` держит conversation lifecycle;
- `query.ts` управляет tool loop, compaction, retries и transitions;
- tools, skills, tasks, permissions и REPL живут внутри одного хоста;
- background work организуется через task abstraction, а не через универсальный внешний kernel;
- remote/server paths существуют, но не как отдельный независимый "skill bus", а как расширение основного host runtime.

Плюсы:

- намного проще проводить сквозные инварианты через tools, UI, permissions и state;
- меньше IPC-стоимости и меньше протокольных разрывов внутри основной петли;
- легче строить общие task/progress/session abstractions;
- намного богаче user-facing поведение можно собирать поверх одного AppState и одного QueryEngine.

Минусы:

- система становится очень большой и тесно связанной;
- рост фич вынуждает опираться на feature flags, lazy imports и сложную state-модель;
- отказ в центральном runtime имеет более широкий blast radius, чем падение отдельной skill в `tabula`.

### Вердикт

- как интегрированный runtime product намного сильнее выглядит `claude-code`;
- как компактный, process-oriented orchestration core элегантнее выглядит `tabula`.

## 2. Tool system и контракт инструментов

### Tabula

В `tabula` tool story проста и понятна:

- kernel знает встроенные `EXEC`, `SPAWN`, `KILL`, `LIST`;
- `boot.py` discovers skill tools из `SKILL.md`;
- skill tool фактически сводится к `exec`-команде и JSON input;
- hook-слой может вмешаться до вызова tool.

Сильные стороны:

- очень низкий ceremony;
- легко добавить новый tool как еще одну skill;
- tool-экосистема naturally polyglot и process-native.

Слабые стороны:

- metadata и validation остаются тонкими;
- граница между declared contract и real runtime behavior местами не стабилизирована;
- пример из `ANALYSIS.md`: modifying-hook для tool input формально существует, но `ToolService` продолжает исполнять старый `msg.Input`;
- duplicate precedence для tools тоже пока не формализована как зрелый контракт.

### Claude Code

В `claude-code` tool system выглядит как полноценная typed platform:

- `Tool.ts` задает общий контракт tool-а;
- `buildTool(...)` нормализует описание, input/output schema, progress, permission hooks и display semantics;
- `tools.ts` собирает пул инструментов с учетом feature flags и permission context;
- built-in tools, MCP tools, skill tools и task tools собираются в общую модель;
- tool exposure может меняться до показа модели, а не только в runtime-check path.

Сильные стороны:

- гораздо более зрелый и формализованный контракт;
- input/output schemas и validation встроены в систему;
- есть activity descriptions, progress events, permission-aware filtering, aliasing;
- tool platform реально рассчитана на сотни вариантов поведения.

Слабые стороны:

- высокая цена входа для новых capability;
- больше boilerplate и сильнее coupling с host runtime;
- расширение чаще происходит не как внешний executable seam, а как встраивание в монолит.

### Вердикт

- по чистоте и быстроте расширения `tabula` приятнее;
- по зрелости tool contracts и operability заметно впереди `claude-code`.

Это один из главных слоев, который `tabula` действительно стоит заимствовать концептуально, не копируя весь monolith.

## 3. Permissions, safety и governance

### Tabula

`tabula` уже придумала хорошую модель policy/hooks:

- `before_message`;
- `before_tool_call`;
- `session_start`;
- `before_spawn`;
- `after_*`.

Это хороший фундамент:

- policy отделена от доменной логики;
- есть шанс строить security, observability и governance на одном каркасе;
- spawn и tool use проходят через единый audit point.

Но на текущем уровне зрелости там еще много незавершенности:

- modifying semantics реализованы не до конца;
- observer заходит в security/modifying path, хотя должен быть наблюдателем;
- gateway/session lifecycle слабее, чем policy-модель на бумаге.

### Claude Code

В `claude-code` permission system уже operationalized:

- есть `ToolPermissionContext` с режимами, rules и persistent updates;
- permission request может проходить через hooks, classifier, interactive UI, coordinator path и remote bridge;
- `PermissionContext.ts` умеет возвращать не только allow/deny, но и `updatedInput`;
- одна и та же permission story работает для local REPL, background agents, CCR/remote sessions и structured IO.

Особенно важно:

- permissions здесь не отдельная "проверка перед tool", а целый orchestration layer;
- запросы разрешений имеют собственный control protocol;
- remote session и bridge paths умеют relay-ить permission decisions через сеть.

### Вердикт

Здесь разрыв особенно большой:

- у `tabula` есть правильный каркас;
- у `claude-code` этот каркас уже превращен в зрелую систему.

Если выбирать один слой, который больше всего стоит изучать `tabula`, это именно permission pipeline.

## 4. Sessions, tasks, background work и subagents

### Tabula

В `tabula` session и subagent story опирается на реальные процессы:

- session принадлежит kernel;
- driver подключается к session;
- subagent создается как отдельный runtime через `SPAWN` и отдельную session scope;
- `subagent_runtime.py` ведет отдельный message/tool loop.

Это сильная сторона:

- subagent здесь не декоративная абстракция, а реальная изолированная сущность;
- concurrency model естественная;
- isolation очень прозрачная.

Но operational gaps тоже видны:

- snapshot/process bookkeeping уже расходится для spawned processes;
- gateway sessions живут слишком долго и плохо очищаются;
- нет зрелой task abstraction поверх этих runtime entities.

### Claude Code

В `claude-code` agent/session/task story заметно более продуктовая:

- `AgentTool` умеет запускать агента как foreground, background, remote или worktree-isolated задачу;
- `LocalAgentTask` и другие task-типы дают progress, notifications, output files и lifecycle;
- scheduled work, background sessions и remote tasks включены в единую модель состояния;
- server/direct-connect mode вводит session server с idle timeout, max sessions и persistent session index.

То есть здесь важен не просто факт subagent-а, а то, что вокруг него есть полноценный task platform layer.

### Вердикт

- как "настоящая process isolation" сильнее и чище выглядит `tabula`;
- как user-visible task/session product намного впереди `claude-code`.

Именно task/session layer в `tabula` сейчас практически отсутствует как отдельная зрелая абстракция. Это большой structural gap.

## 5. UX и gateway surfaces

### Tabula

Gateway story в `tabula` модульная:

- CLI gateway;
- API gateway;
- Telegram gateway.

Это красиво архитектурно:

- любой transport можно вынести в skill;
- ядру не нужно знать о конкретном UX;
- system остается composable.

Но расплата очевидна:

- каждый gateway частично переизобретает session management;
- concurrency, cleanup и streaming semantics размазаны по разным skill-процессам;
- из `ANALYSIS.md` уже видно, что API и Telegram path повторяют одни и те же lifecycle-слабости.

### Claude Code

`claude-code` строит UX как центральную часть системы:

- огромный `REPL.tsx`;
- десятки slash-команд;
- doctor/resume/session/config/mcp/plugin surfaces;
- bridge к IDE и remote-control flows;
- structured IO для headless/SDK;
- direct-connect server;
- multiple screens и dialogs.

Здесь UX не добавлен сбоку, а является одним из главных слоев архитектуры.

### Вердикт

- `tabula` выигрывает в архитектурной чистоте transport seam;
- `claude-code` выигрывает в цельности и богатстве UX на порядок.

Если `tabula` хочет расти как продукт, ей потребуется не просто еще один gateway, а единый session UX layer.

## 6. State management и память

### Tabula

В `tabula` память и состояние пока в основном file-based и сравнительно простые:

- boot собирает prompt из файлов и skills;
- memory skill пишет Markdown + index;
- driver пишет history в `history.jsonl`;
- session model mostly ephemeral относительно продуктового state layer.

Это сохраняет простоту и прозрачность, но пока не дает сильной единой state model.

### Claude Code

В `claude-code` состояние развито гораздо сильнее:

- `AppStateStore.ts` содержит огромный единый state shape;
- есть transcript/session storage, task output storage, file history, attribution, plugin state;
- `memdir` встроен в системный prompt и behavioral model;
- есть auto memory, team memory sync и отдельные sync semantics;
- server mode умеет хранить persistent session index.

Это дает намного более зрелый продуктовый behavior, но и очень увеличивает связанность системы.

### Вердикт

- по ясности и минимализму `tabula` приятнее;
- по depth of stateful behavior `claude-code` несопоставимо сильнее.

Для `tabula` правильный урок отсюда не "сделать гигантский global state", а "ввести явные first-class модели session, task и memory lifecycle".

## 7. Skills, plugins, MCP и модель расширения

### Tabula

У `tabula` одна главная seam-модель: skills.

Это сильная черта:

- одна понятная единица расширения;
- skill может быть tool, hook, gateway, driver или daemon;
- `SKILL.md` + `run.py` образуют очень легкую и понятную extension surface.

Главная слабость:

- metadata беднее, чем зрелая plugin platform;
- enablement, trust, health, versioning и dependency story пока почти не формализованы.

### Claude Code

У `claude-code` расширение многоуровневое:

- built-in tools;
- bundled skills;
- file-based local skills;
- plugins;
- builtin plugins;
- MCP servers и MCP-backed skills;
- remote/discoverable skill paths.

Это дает огромную гибкость, но уже не такую чистую mental model:

- extension surface очень богата;
- зато разработчику сложнее понять "что здесь главный способ расширения".

### Вердикт

- `tabula` выигрывает в концептуальной простоте;
- `claude-code` выигрывает в зрелости ecosystem layer.

Если `tabula` будет развивать extensibility, ей стоит сохранять primacy skills-модели, а не плодить несколько конкурирующих extension систем.

## 8. Plugins и metadata discipline

Это отдельный важный слой, потому что именно здесь видно отличие "чистой идеи" от "операционной платформы".

### Tabula

В `tabula` metadata в основном живет:

- в frontmatter `SKILL.md`;
- в boot discovery;
- в конвенциях install/run path.

Это быстро, но хрупко:

- install scripts и runtime уже расходились;
- duplicate precedence для tools не стабилизирована;
- Windows launchers и server launchers могут указывать не туда.

### Claude Code

В `claude-code` plugin/skill metadata гораздо дисциплинированнее:

- plugin loader и manifest schema отделены;
- built-in plugins и bundled skills оформлены как first-class registry;
- background installation и refresh имеют собственный lifecycle;
- skill loading и plugin loading идут через более богатую metadata-модель.

### Вердикт

Здесь `tabula` определенно есть чему учиться:

- не нужно копировать весь plugin marketplace;
- но точно стоит отделить runtime exec path от declarative metadata и сделать manifests/validation first-class.

## 9. Remote, bridge и server story

### Tabula

У `tabula` есть:

- kernel server;
- API gateway;
- Telegram gateway;
- session snapshot endpoint.

Но это скорее transport/access layer вокруг ядра, чем полноценная remote-control platform.

### Claude Code

У `claude-code` remote layer намного богаче:

- direct-connect session server;
- remote session manager;
- WebSocket + HTTP hybrid transport;
- structured control protocol;
- bridge mode и remote-control server;
- child session spawning, access tokens, reconnects и heartbeats.

Это уже не просто "доступ к агенту по сети", а отдельная распределенная control-plane story.

### Вердикт

Если `tabula` когда-нибудь захочет стать не только локальным kernel, а платформой для multiple clients/devices/IDEs, именно здесь gap будет одним из самых больших.

## 10. Operational maturity

### Tabula

Сильная сторона `tabula` в том, что ее еще реально можно быстро довести до состояния "небольшой, но очень надежной платформы".

Но пока operational maturity ограничена:

- install path расходился с реальным layout;
- launchers расходились с CLI contract;
- gateway lifecycle еще сырой;
- API path выглядит не только архитектурно слабым, но местами и буквально сломанным runtime-ом.

### Claude Code

У `claude-code` operational shell гораздо богаче:

- startup fast-paths;
- lazy loading;
- feature-gated subsystems;
- setup/onboarding/doctor/update/server flows;
- plugin background installation;
- multiple permission and transport modes.

Даже если snapshot не позволяет честно прогнать весь build/test path, по коду уже видно, что проект мыслит категориями production system, а не только agent runtime.

### Вердикт

По operational maturity `claude-code` значительно впереди.

## Где каждый проект реально сильнее

## Где сильнее Tabula

- Когда нужен небольшой и понятный agent kernel.
- Когда важна жесткая process isolation между capabilities.
- Когда хочется строить систему вокруг wire protocol, а не вокруг большого in-process host.
- Когда приоритетом является модульность по ролям и языкам.
- Когда важнее сохранить архитектурную простоту, чем быстро нарастить продуктовую оболочку.

## Где сильнее Claude Code

- Когда нужен полноценный coding product, а не только ядро.
- Когда нужны rich permissions, background tasks, remote sessions и structured UX.
- Когда важны server/bridge/IDE/control-plane сценарии.
- Когда нужна зрелая tool/task/session/product abstraction.
- Когда нужно много operational tooling вокруг самого агентного цикла.

## Что Tabula стоит заимствовать

Ниже то, что действительно выглядит полезным заимствованием, а не слепым копированием.

### 1. Typed tool contracts

`tabula` стоит ввести более строгий контракт tool-а:

- schema для input;
- optional schema для output;
- activity description;
- нормализованный validation/result model;
- единый registration contract поверх boot discovery.

Это можно сделать, не отказываясь от process-oriented skills.

### 2. First-class permission pipeline

Нужно развить hooks/policy до полноценного permission orchestration layer:

- allow/deny/ask как first-class outcomes;
- реальная поддержка modified payload;
- единая модель user approval;
- готовность к future remote approval и non-CLI gateways.

### 3. First-class task/session layer

`tabula` сейчас умеет процессы и сессии, но почти не умеет tasks как продуктовую сущность.

Полезно заимствовать идею:

- у задачи есть ID;
- lifecycle;
- progress;
- output artifact;
- notification;
- timeout/cleanup policy.

Это особенно важно для subagents.

### 4. Manifest-first metadata

Стоит отделить:

- metadata;
- discovery;
- enablement;
- exec path;
- trust/health/version info.

Сейчас у `tabula` слишком многое держится на мягких конвенциях вокруг `SKILL.md` и shell path.

### 5. Operator surfaces

Даже в маленьком проекте полезны минимальные версии:

- `doctor`;
- `setup`/`install` self-check;
- session inspection;
- health/status command;
- более явная diagnostics story.

Это даст большой выигрыш в зрелости без переписывания архитектуры.

## Что Tabula не стоит копировать

### 1. Монолитный host runtime целиком

Сила `tabula` как раз в том, что у нее ядро небольшое и границы явные.

Если механически перенести логику в большой единый host, проект почти наверняка потеряет свой главный architectural edge раньше, чем успеет получить product-level benefits `claude-code`.

### 2. Слишком раннее умножение surface area

`claude-code` может позволить себе:

- огромный REPL;
- bridge;
- server;
- plugin system;
- task system;
- remote control;
- companion surfaces.

`tabula` пока не прошла фазу hardening ядра, поэтому расширять surface area раньше стабилизации process/session/tool contracts было бы ошибкой.

### 3. Несколько параллельных extension systems

У `tabula` уже есть сильная единица расширения: skill.

Лучше усилить ее metadata и contracts, чем плодить рядом plugin-layer, bundle-layer и отдельные command ecosystems без необходимости.

## Главный стратегический вывод

`claude-code` показывает, как выглядит зрелый full-stack agent product.

`tabula` показывает, как может выглядеть хороший компактный agent kernel.

Это не взаимоисключающие позиции, но важно не перепутать порядок развития.

Для `tabula` лучший путь сейчас такой:

1. Довести до зрелости существующее ядро.
2. Починить install/session/process/gateway contracts из `ANALYSIS.md`.
3. Ввести более строгий typed layer для tools, permissions и tasks.
4. Только потом расширять product surfaces.

Если пытаться прямо сейчас "догонять `claude-code` по поверхности", есть высокий риск потерять сильные стороны `tabula` и не получить зрелость `claude-code`.

Если же использовать `claude-code` как ориентир не по объему фич, а по качеству contracts вокруг tools, permissions, tasks и sessions, это будет очень продуктивное направление.

## Итоговый вердикт

Если сформулировать совсем коротко:

- `tabula` сегодня интереснее как архитектурная идея платформы;
- `claude-code` сегодня сильнее как зрелая агентная продуктовая система;
- `tabula` не нужно становиться копией `claude-code`;
- `tabula` нужно стать более надежной, контрактно строгой и operationally disciplined версией самой себя.

Именно тогда заимствования из `claude-code` будут усиливать проект, а не размывать его архитектурную идентичность.
