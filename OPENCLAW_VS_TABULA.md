# OpenClaw vs Tabula

Дата: 2026-04-16

## Контекст

Этот документ сравнивает два локально доступных репозитория:

- `tabula`: `/Users/mak/src/tabula`
- `openclaw`: `/Users/mak/src/openclaw`

Цель документа:

1. Зафиксировать глубокое сравнение `openclaw` и `tabula`.
2. Понять, на каком архитектурном слое проекты действительно сопоставимы, а где они решают уже разные классы задач.
3. Выделить, что именно `tabula` имеет смысл заимствовать из `openclaw`, а что копировать не стоит.

За основу по `tabula` взят уже подготовленный `ANALYSIS.md`, дополнительно сверенный по ключевым файлам:

- `boot.py`
- `internal/kernel/tool_service.go`
- `skills/gateway-api/run.py`
- `skills/gateway-telegram/run.py`

По `openclaw` анализ основан на:

- структуре монорепозитория;
- `README.md`, `package.json`, `AGENTS.md`;
- docs по архитектуре, plugin internals, session model и sandboxing;
- ключевых runtime-модулях `src/gateway/*`, `src/plugins/*`, `src/tasks/*`, `src/agents/*`, `src/security/*`, `src/media/*`;
- выборочной проверке plugin manifests и package surfaces.

Ключевые опорные файлы `openclaw`, на которые я в основном опирался:

- `docs/concepts/architecture.md`
- `docs/plugins/architecture.md`
- `docs/concepts/session.md`
- `src/gateway/server-http.ts`
- `src/gateway/protocol/index.ts`
- `src/plugins/loader.ts`
- `src/plugins/runtime.ts`
- `src/plugins/manifest-registry.ts`
- `src/plugins/hooks.ts`
- `src/plugin-sdk/core.ts`
- `src/plugin-sdk/plugin-entry.ts`
- `src/tasks/task-registry.ts`
- `src/tasks/task-flow-registry.store.sqlite.ts`
- `src/security/audit.ts`
- `src/infra/heartbeat-runner.ts`
- `src/agents/agent-command.ts`
- `src/agents/subagent-control.ts`

Важно: в отличие от `ANALYSIS.md` для `tabula`, здесь не делался полный прогон тестового контура `openclaw`. Это архитектурный сравнительный анализ по коду и контрактам, а не runtime-валидация полного монорепо.

## Короткий вывод

`openclaw` и `tabula` пересекаются по теме "локальный агент с инструментами, сессиями и gateway", но архитектурно это уже системы разного класса.

- `tabula` сегодня ближе к компактной агентной платформе или "agent microkernel":
  - Go-ядро;
  - собственный wire protocol;
  - Python-skills как отдельные процессы;
  - boot-time discovery;
  - hooks / gateways / subagents как расширяемые внешние компоненты.
- `openclaw` ближе к полноценной local-first assistant platform:
  - единый TypeScript-host;
  - typed WebSocket control plane;
  - in-process plugin platform;
  - channel ecosystem;
  - task/cron/heartbeat layer;
  - sandboxing;
  - mobile/mac apps;
  - operator tooling, onboarding, doctor, update flows.

Если очень коротко:

- `openclaw` заметно сильнее как продуктовая и операционная система вокруг персонального ассистента;
- `tabula` чище и интереснее как компактная multi-process agent platform;
- `openclaw` уже выглядит как большой продукт с высокой стоимостью сопровождения;
- `tabula` выглядит как сильная архитектурная идея, которая пока не доведена до operational maturity.

Главный практический вывод:

- `tabula` не стоит пытаться "превратить в openclaw целиком";
- но у `openclaw` есть несколько очень сильных слоев, которые `tabula` стоит изучить и частично перенять:
  - manifest-first metadata;
  - operator/security tooling;
  - session/task persistence;
  - typed gateway control plane;
  - onboarding/doctor/update story.

## Что на самом деле представляет собой каждый проект

## Tabula

`tabula` построена как самостоятельная агентная система с собственным ядром:

- Go-kernel в `internal/kernel/*` и `cmd/tabula/main.go`;
- boot-слой в `boot.py`, который сканирует skills, собирает prompt, tools и slash-команды;
- Python-skills в `skills/*`;
- WebSocket protocol между kernel и skills;
- gateway-слой как набор отдельных skills (`gateway-cli`, `gateway-api`, `gateway-telegram`);
- hook/policy-модель внутри kernel.

Это архитектура платформы, в которой ядро и навыки сознательно разделены по языкам и процессам.

Ключевая идея правильная: orchestration и lifecycle живут в kernel, доменная логика и интеграции живут в skills.

## OpenClaw

`openclaw` - это уже не просто "агент с gateway", а большой local-first assistant stack:

- единый long-lived gateway/control plane;
- typed WebSocket protocol между gateway, CLI, UI и nodes;
- in-process plugin platform с manifest discovery и SDK;
- отдельные channel plugins, provider plugins, tool/hook plugins;
- task registry, task flows, cron, heartbeat, pairing, sandboxing;
- media pipeline, memory host SDK, browser/canvas surfaces;
- companion apps для macOS/iOS/Android.

Важно не спутать терминологию:

- в `tabula` слово "skills" обозначает основной runtime seam;
- в `openclaw` основным runtime seam являются именно `plugins`, а `skills` - это скорее prompt/workspace artifacts и user-facing instructions.

То есть на уровне расширения проекты устроены принципиально по-разному.

## Самое важное архитектурное различие

`tabula` и `openclaw` находятся на разных уровнях абстракции.

### Tabula

Мысленно это:

- kernel;
- process runtime;
- protocol bus;
- отдельные skill processes;
- pluggable gateways / hooks / drivers.

Она ближе к "агентной ОС" в миниатюре.

### OpenClaw

Мысленно это:

- gateway/control plane;
- embedded agent runtime;
- plugin platform;
- channel/message operating environment;
- task automation platform;
- local companion apps.

Она ближе к "продуктовой платформе персонального ассистента".

Из этого вытекает ключевой вывод:

- `tabula` стоит сравнивать с `openclaw` не как "два одинаковых продукта";
- правильнее сравнивать `tabula` как platform core с центральными runtime-слоями `openclaw`.

Именно поэтому местами `openclaw` выглядит "намного сильнее", но это не всегда означает, что его решения подходят `tabula` без потери ее сильных сторон.

## Глубокое сравнение

## 1. Архитектурная ставка

### Tabula

Главная ставка `tabula` - разделить систему на:

- жесткий kernel;
- wire protocol;
- отдельные skill-процессы;
- file-based boot assembly.

Плюсы:

- отличная концептуальная модульность;
- естественная multi-language extensibility;
- хорошая process isolation;
- небольшое ядро легче держать в голове.

Минусы:

- больше operational рисков на стыках protocol/process/session lifecycle;
- каждый gateway по сути собирает собственный session/driver story;
- install/dev path и runtime contracts сейчас еще не стабилизированы.

### OpenClaw

Главная ставка `openclaw` - один центральный host runtime, внутри которого:

- живет gateway;
- грузятся plugins;
- держатся channels/providers/tools/hooks;
- ведутся tasks, sessions, cron, heartbeat;
- обслуживаются control-plane клиенты и device nodes.

Плюсы:

- очень сильный единый control plane;
- меньше "случайной распределенности" между независимыми процессами;
- легче строить сквозные feature layers;
- намного богаче operator story.

Минусы:

- вся система существенно сложнее;
- in-process plugin failures потенциально опаснее для общего хоста;
- кодовая база уже требует большого числа boundary rules и специальных guardrails.

### Вердикт

- по архитектурной простоте и чистоте идеи `tabula` сейчас выглядит элегантнее;
- по зрелости platform shell и control plane `openclaw` намного впереди.

## 2. Runtime-модель

### Tabula

Runtime `tabula` - это kernel плюс skill-процессы.

Ключевой поток:

1. kernel стартует;
2. `boot.py` собирает config;
3. kernel поднимает процессы;
4. skills делают `connect` / `join`;
5. driver получает prompt/tools;
6. дальше идет message/tool/hook loop.

Это очень осмысленная схема, но `ANALYSIS.md` уже показывает, что реальная надежность lifecycle еще отстает от архитектуры:

- process bookkeeping и snapshot state расходятся;
- gateway sessions живут слишком свободно;
- session cleanup и concurrency местами недоведены.

### OpenClaw

Runtime `openclaw` значительно более монолитен по execution model:

- gateway - центр всего control plane;
- agent runtime embedded;
- plugins регистрируются in-process;
- channels/providers/hooks/tasks работают в рамках одного host runtime;
- Docker sandboxing включается как policy/runtime layer поверх этого хоста.

Это дает:

- единый state ownership;
- единый session store;
- typed task/task-flow layer;
- единый event stream;
- единые policy hooks и security checks.

Но цена:

- сложность концентрируется внутри одного большого TypeScript runtime;
- вместо простых process seams появляются внутренние contract seams между десятками runtime surfaces.

### Вердикт

- `tabula` сильнее как идея распределенного локального runtime;
- `openclaw` сильнее как интегрированный host runtime.

## 3. Control Plane и gateway-архитектура

### Tabula

У `tabula` gateway-история пока функциональна по замыслу, но сыровата по исполнению.

По `ANALYSIS.md` и выборочной проверке:

- `gateway-api` держит session state с одной kernel connection и одним event queue;
- использует `HTTPServer`, а не threaded server;
- session ownership и cleanup выглядят недодуманно;
- при этом в `gateway-api` есть и буквальные runtime-разрывы по именам protocol-констант;
- `gateway-telegram` повторяет те же lifecycle-patterns вокруг chat->session mapping и driver spawning.

То есть у `tabula` gateway сейчас скорее thin adapter поверх kernel, чем зрелый control plane.

### OpenClaw

У `openclaw` gateway - это центральный продуктовый слой.

Он:

- держит typed WebSocket API;
- обслуживает control-plane clients;
- обслуживает node clients;
- поднимает HTTP surfaces;
- отдает control UI, canvas host, plugin routes;
- ведет health, cron, heartbeat, pairing и auth flows.

Здесь важен не только объем, но и то, что gateway является owner state:

- sessions принадлежат gateway;
- UI и CLI спрашивают gateway;
- pairing/device auth проходят через gateway;
- tasks/cron/heartbeat интегрированы в тот же control plane.

### Вердикт

Это одна из крупнейших зон превосходства `openclaw`.

`tabula` имеет kernel-first архитектуру, но ее gateway-слой пока слаб.
`openclaw` имеет настоящий control-plane продукт.

## 4. Модель расширения

### Tabula

Основной seam - skill-directory с `SKILL.md` и `run.py`.

Сильные стороны:

- расширение очень прозрачно;
- skill можно мыслить как отдельный capability process;
- хорошо подходят heterogenous integrations;
- можно расширять систему без распухания ядра.

Слабые стороны:

- metadata contract пока менее формализован, чем в зрелых plugin systems;
- duplicate/override semantics для tools уже расходятся между тестами и реальным boot path;
- install/runtime dependencies выражены слабее, чем фактическая сложность skills.

### OpenClaw

Основной seam - plugin manifest плюс SDK plus registration API.

Особенно сильные стороны:

- manifest-first discovery и validation;
- разделение discovery/config validation и runtime load;
- capability registration как typed contract;
- разные plugin kinds: channel/provider/tool/hook/service/runtime helpers;
- plugin-specific public SDK subpaths и boundary rules;
- отдельные docs именно для plugin architecture.

Это существенно более зрелая модель расширения, чем у `tabula`.

Но есть и цена:

- plugin architecture стала отдельной подсистемой с собственной сложностью;
- потребовались многочисленные guardrails, AGENTS guides, contract tests, import boundaries;
- для небольшого проекта такую систему рано копировать целиком.

### Вердикт

- если смотреть на extensibility maturity, `openclaw` сильно впереди;
- если смотреть на extensibility simplicity, `tabula` пока приятнее и легче.

## 5. Hooks и policy-модель

### Tabula

У `tabula` hooks - одна из самых интересных идей проекта.

Сильные стороны:

- хороший набор lifecycle points;
- понятная попытка разделить security/domain/observability semantics;
- hooks встроены в общую platform model.

Проблема не в идее, а в незавершенности contracts:

- `before_tool_call` объявлен сильнее, чем фактически реализован;
- observer оказывается подключен к критическим path;
- contract шире, чем реальное поведение.

### OpenClaw

У `openclaw` hook-модель уже оформлена как часть plugin platform:

- hooks typed;
- есть priority ordering;
- есть failure policies;
- есть набор modifying hooks для prompt/model/reply/tool/session/subagent/gateway/install phases;
- hooks находятся в общей plugin registration story.

Это выглядит существенно взрослее и системнее.

### Вердикт

- по концептуальной свежести `tabula` hook-layer интересен;
- по зрелости и завершенности contract-а `openclaw` значительно сильнее.

## 6. Sessions, tasks, cron и long-running work

### Tabula

У `tabula` сессии и процессы - базовые сущности архитектуры.

Это хорошо:

- session scope встроен в модель kernel;
- subagents уже мыслятся как отдельные session/process сущности;
- gateways естественно опираются на sessions.

Но operational story пока еще хрупкая:

- snapshot/process bookkeeping расходится;
- gateway sessions не имеют достаточного lifecycle management;
- сессии и driver ownership местами размазаны по gateway logic.

### OpenClaw

У `openclaw` вокруг sessions построена почти целая подсистема:

- документированная session model;
- session store + transcript files;
- maintenance / cleanup policies;
- task registry;
- task flows;
- cron isolated agents;
- heartbeat-driven autonomous work;
- explicit session tools;
- session routing across channels/agents.

Это большой скачок по зрелости.

Отдельно важно, что `openclaw` не ограничивается "есть subagent":

- у него есть отдельный слой detached work accounting;
- task runs и flows можно summarise, persist, cancel, notify, reconcile;
- cron и heartbeat уже встроены в operational loop.

### Вердикт

Это еще одна большая зона превосходства `openclaw`.

`tabula` уже придумала правильные базовые сущности.
`openclaw` эти сущности довела до уровня platform operations.

## 7. Subagents

### Tabula

Subagent в `tabula` - это по-настоящему отдельный process/session scope.

Плюсы:

- реальная process isolation;
- естественная модель для delegation;
- субагент не декоративен, а является отдельной runtime сущностью.

Минусы:

- process lifecycle дороже и хрупче;
- session/process accounting должен быть очень точным;
- aggregation и cleanup нельзя "спрятать" внутри одного процесса.

### OpenClaw

Subagents в `openclaw` встроены в общий agent runtime и поверх него обвязаны registry/control/task layers.

Это дает:

- сильный контроль;
- task visibility;
- nested subagent registry;
- steer/kill/wait/list control;
- интеграцию с task registry, sessions и delivery.

Но это меньше process isolation и больше integrated orchestration.

### Вердикт

- если нужен hard process boundary, `tabula` идея сильнее;
- если нужен управлямый production-grade subagent UX, `openclaw` впереди.

## 8. Безопасность

### Tabula

В `tabula` безопасность пока больше выражена как policy ambition:

- permissions через hooks;
- observer / permissions / session hooks;
- идея governance присутствует;
- архитектурно место под security есть.

Но зрелого hardening-слоя пока не видно:

- нет богатого operator-grade audit path;
- gateway surfaces слабее;
- policy contracts еще не везде совпадают с runtime behavior.

### OpenClaw

Безопасность - одна из самых сильных и самых зрелых сторон `openclaw`.

Что особенно выделяется:

- device pairing и DM pairing;
- gateway auth modes;
- explicit non-loopback / plaintext WS hardening;
- sandbox modes и per-agent tool policies;
- security audit / doctor story;
- safe-bin policies;
- SecretRef model;
- strong emphasis on public/private contract boundaries;
- separate docs для security, sandboxing, pairing, remote access.

То есть `openclaw` уже спроектирован как система, которую реально могут открыть наружу и при этом контролировать.

### Вердикт

По security maturity `openclaw` на голову выше `tabula`.

## 9. Media, channels и product surface

### Tabula

`tabula` пока концентрируется на базовом агентном core:

- CLI;
- API gateway;
- Telegram;
- memory;
- MCP;
- files/weather и другие tool-skills.

Это логично для ранней стадии платформы.

### OpenClaw

`openclaw` уже несет огромный product surface:

- десятки messaging channels;
- browser/canvas surfaces;
- speech/STT/TTS/realtime/media understanding;
- image/video/music generation capabilities;
- memory host SDK;
- nodes и companion apps;
- mobile/mac integrations.

Это не просто "много фич", а другой класс платформенности.

### Вердикт

По breadth of product surface сравнение почти одностороннее: `openclaw` намного шире.

## 10. Developer Experience и operator experience

### Tabula

Здесь `ANALYSIS.md` уже довольно жесткий, и по делу:

- install path расходится с реальностью;
- launcher scripts и README не совпадают;
- gateway/runtime entrypoints местами сломаны;
- dependency story не собрана в единый контракт.

То есть DX сейчас слабее, чем сама архитектурная идея.

### OpenClaw

У `openclaw` DX/Ops story уже заметно зрелее:

- `openclaw onboard`;
- `openclaw doctor`;
- rich config/docs story;
- update channel story;
- plugin install/update flows;
- explicit architecture and plugin docs;
- strong repo conventions и boundary documentation.

Но и цена тут тоже есть:

- DX уже требует поддерживать огромный docs/config/tooling surface;
- build/test/release flow очень тяжелые;
- архитектура нуждается в значительном количестве внутренних правил.

### Вердикт

`openclaw` безусловно сильнее по DX/Ops.
`tabula` пока просто не дошла до этой стадии.

## 11. Тестовая зрелость

### Tabula

По `ANALYSIS.md` у `tabula` хороший breadth coverage для стадии проекта, но:

- часть e2e/network tests зависит от окружения;
- есть реальные product/test mismatches;
- тестовый контур подтверждает идею проекта, но не гарантирует зрелую эксплуатационную надежность.

### OpenClaw

У `openclaw` тестовый контур выглядит гораздо массивнее и институционализированнее.

На уровне грубой оценки по дереву репозитория:

- около `106` bundled extension directories;
- около `4064` colocated `*.test.ts` / `*.contract.test.ts` / `*.live.test.ts` файлов по `src`, `extensions`, `packages`, `apps`, `skills`.

Кроме количества, важна именно структура:

- отдельные contract tests;
- live tests;
- docker tests;
- extension boundary tests;
- protocol/config drift checks;
- performance/startup/import-cycle gates.

Это уже зрелая engineering machine.

### Вердикт

По testing maturity `openclaw` намного впереди.

## 12. Масштаб и цена сопровождения

Здесь важна не только сила `openclaw`, но и ее стоимость.

### OpenClaw

Сильные стороны `openclaw` одновременно являются ее главным риском:

- монорепо очень большое;
- product surface огромен;
- plugin/platform/channel layers сложны;
- boundary policy зафиксирована в большом числе правил;
- часть архитектуры уже требует специальных cache/import/load guardrails.

Это проект уровня "сильный продукт + сильная внутренняя платформа", но и цена его поддержки очень высока.

### Tabula

`tabula` пока во много раз меньше:

- по грубой оценке порядка `174` файлов в `internal`, `skills`, `cmd`, `tests`;
- `26` skill directories;
- тестовый и runtime surface на порядок компактнее.

То есть `tabula` легче стабилизировать, чем сопровождать систему масштаба `openclaw`.

### Вердикт

`openclaw` сильнее, но дороже почти во всем.
`tabula` слабее по зрелости, но имеет шанс быстрее стать надежной именно потому, что она намного компактнее.

## 13. Главные архитектурные риски OpenClaw

Чтобы сравнение было честным, важно явно назвать и цену зрелости `openclaw`.

### 1. Очень высокий complexity tax

`openclaw` уже находится в зоне, где значительная часть инженерной силы уходит не на новые capability, а на удержание boundaries:

- import guardrails;
- plugin boundary rules;
- runtime caching rules;
- protocol/config drift checks;
- install/update/release contracts.

Это цена большой внутренней платформы.

### 2. In-process plugin blast radius

Главный seam `openclaw` - плагины внутри общего host runtime.
Это удобно и быстро, но означает:

- меньше системной изоляции;
- выше цена runtime side effects;
- больше давление на loader/runtime contracts и test discipline.

То, что в `tabula` может быть локальной проблемой одного skill-process, в `openclaw` чаще становится проблемой общего host-а.

### 3. Очень широкий product surface как источник архитектурной инерции

Каналы, apps, nodes, browser/canvas, cron, tasks, pairing, memory, media, plugins, docs, installers - все это дает огромную силу, но одновременно:

- замедляет изменения;
- усложняет refactors;
- повышает цену любого cross-cutting redesign.

### 4. Высокий порог входа для сопровождающих

Даже с хорошей документацией `openclaw` уже требует большого контекстного объема:

- нужно понимать gateway;
- plugin system;
- channel model;
- task/session model;
- sandbox/security policy;
- apps/nodes surface.

Это зрелая система, но она не "дешева в голове".

## Где OpenClaw сильнее Tabula

Ниже - зоны, где превосходство `openclaw` кажется системным, а не случайным.

### 1. Gateway как реальный control plane

У `openclaw` gateway - центральная система координации.
У `tabula` gateway пока скорее thin adapter вокруг kernel.

### 2. Plugin platform maturity

Manifest-first discovery, typed capability registration, SDK seams, contract tests и registry architecture у `openclaw` намного зрелее, чем skill metadata model у `tabula`.

### 3. Security и hardening

Pairing, sandboxing, audit, auth modes, tool policies и operator guidance у `openclaw` на существенно более высоком уровне.

### 4. Session/task/cron/heartbeat story

`openclaw` уже думает не только "как выполнить turn", но и "как сопровождать agent system во времени".
Для production-grade assistant platforms это критично.

### 5. DX/Ops tooling

Onboarding, doctor, update, docs, release conventions и structured config story у `openclaw` намного сильнее.

### 6. Test discipline

`openclaw` не просто имеет много тестов - он формализует contracts, boundaries, drift checks и operational gates.

## Где Tabula по-прежнему лучше или интереснее

Здесь важно не потерять сильную сторону `tabula`, просто глядя на более зрелый соседний проект.

### 1. Чистота core architecture

Go-kernel + Python-skills + wire protocol - очень сильная композиция.
Она проще, яснее и потенциально долговечнее, чем гигантский single-host runtime.

### 2. Настоящая process isolation

У `tabula` subagents и skills по замыслу действительно отдельные процессы, а не просто разные runtime modes одного host-а.

### 3. Multi-language extensibility

`tabula` легче мыслить как platform bus для heterogenous runtimes.
`openclaw` намного сильнее завязан на один большой TypeScript-host.

### 4. Меньшая архитектурная стоимость

`tabula` проще довести до надежного состояния, чем сопровождать систему масштаба `openclaw`.

### 5. Ниже риск "platform overgrowth"

`openclaw` уже очень велик и несет немалую internal complexity tax.
`tabula` пока еще можно удержать в виде компактной, понятной и строгой системы.

## Что Tabula стоит заимствовать из OpenClaw

Не целиком продукт, а именно удачные архитектурные идеи.

### 1. Manifest-first metadata слой

`tabula` стоит усилить skill metadata так, чтобы:

- discovery можно было валидировать без запуска skill runtime;
- install/dependency story стала частью контракта;
- tool/hook/gateway capabilities были формализованы заранее.

Это один из лучших уроков `openclaw`.

### 2. Единый operator-grade gateway story

Не обязательно копировать весь `openclaw` gateway, но `tabula` точно нужна:

- единая модель session ownership;
- cleanup/TTL/maintenance;
- более сильный control API;
- health/status/doctor surfaces.

### 3. Security audit и doctor path

Для `tabula` это был бы очень сильный multiplier:

- проверка install/runtime drift;
- проверка gateway auth/port/session state;
- проверка permissions/hooks contracts;
- проверка отсутствующих зависимостей и broken entrypoints.

### 4. Явная task/session persistence

`tabula` уже имеет sessions и subagents, но пока почти не имеет отдельного слоя detached work accounting.

Даже без огромной подсистемы `openclaw` стоит изучить:

- task registry;
- flow records;
- session maintenance;
- cleanup and archival policies.

### 5. Структурированный public SDK boundary

В `tabula` стоит рано зафиксировать, какие seams являются публичными для skills/bundles/extensions, а какие нет.
Это сильно помогает не расползтись архитектурно.

## Что Tabula НЕ стоит копировать из OpenClaw

### 1. Полный product surface

Сейчас это был бы путь к распылению усилий.
`tabula` сначала нужна стабилизация ядра и gateway lifecycle.

### 2. Полную plugin-platform сложность

У `openclaw` эта сложность оправдана его масштабом.
Для `tabula` она пока будет premature architecture tax.

### 3. In-process host as universal runtime

Это разрушит одну из лучших сторон `tabula`: process isolation и ясное разделение по языкам.

### 4. Слишком раннюю стандартизацию на десятки поверхностей

`openclaw` уже вынужден жить с огромным количеством rules/contracts.
`tabula` лучше зафиксировать несколько сильных seams, а не сразу пытаться покрыть все случаи.

## Практический вывод для стратегии Tabula

Если мыслить прагматично, то `tabula` сейчас находится в точке:

- архитектурно она сильнее, чем показывает текущая operational надежность;
- product-wise она сильно меньше `openclaw`;
- именно поэтому ей выгоднее пройти цикл stabilization, а не цикл масштабного расширения.

Лучший следующий вектор выглядит так:

1. довести install/bootstrap/runtime contracts до надежности;
2. нормализовать process/session lifecycle;
3. укрепить gateway concurrency и cleanup;
4. только потом усиливать metadata, doctor и public SDK seams;
5. и лишь затем расширять продуктовый surface.

То есть правильная траектория для `tabula` не "догонять breadth `openclaw`", а:

- сохранить свою сильную platform-core архитектуру;
- подтянуть operational maturity;
- затем выборочно заимствовать зрелые operator/extensibility-паттерны из `openclaw`.

## Архитектурный вердикт

`openclaw` сегодня сильнее `tabula` почти по всем признакам зрелого продукта:

- control plane;
- security;
- extensibility contracts;
- sessions/tasks/cron;
- docs;
- testing;
- onboarding and operations.

Но это не означает, что `tabula` "хуже как архитектура".

Скорее так:

- `openclaw` - это зрелая и очень широкая продуктовая платформа;
- `tabula` - это более компактная и концептуально чистая агентная платформа, которая пока не прошла hardening phase.

Если оценивать "что впечатляет как инженерная композиция", `tabula` по-прежнему очень интересна.
Если оценивать "что сегодня выглядит как production-ready local-first assistant platform", `openclaw` впереди очень заметно.

## Финальная формула

Самая точная рабочая формула сравнения сейчас такая:

- `openclaw` - сильнее как product platform;
- `tabula` - интереснее как compact agent kernel architecture;
- `openclaw` стоит изучать как источник зрелых решений;
- `tabula` стоит развивать в сторону hardening своей собственной модели, а не переписывания под `openclaw`.

Если коротко в одну строку:

`openclaw` - это система, у которой operational maturity уже догнала архитектуру; `tabula` - это система, у которой архитектура пока опережает operational maturity.
