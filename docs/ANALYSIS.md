# Глубокий анализ проекта Tabula

Дата: 2026-04-16

## Контекст

Этот документ собран по текущему состоянию репозитория `tabula` и предназначен как базовый рабочий артефакт для следующих доработок.

Анализ основан на:

- структуре репозитория;
- исходниках Go-ядра и Python-скиллов;
- сценариях запуска, установки и тестирования;
- прогоне `go test ./...`;
- прогоне `pytest tests/ -q` в локальном окружении репозитория.

## Короткий вывод

Tabula уже выглядит не как прототип одной функции, а как настоящая агентная платформа с удачно выбранным разделением ответственности:

- Go используется там, где важны маршрутизация, жизненный цикл процессов и протокол;
- Python используется там, где нужна скорость разработки навыков, интеграций и провайдеров;
- архитектура через WebSocket и отдельные процессы дает хорошую расширяемость;
- система skills / hooks / gateways / subagents спроектирована гибко и масштабируемо по идее.

При этом проект сейчас находится в фазе, где концепция уже сильнее, чем операционная надежность. Главные проблемы не в общей архитектуре, а в нескольких системных разрывах между контрактами, инфраструктурой и реальным поведением:

- dev/install-путь выглядит сломанным;
- есть ошибки в менеджменте процессов и сессий;
- API/gateway-слой пока слаб по concurrency и lifecycle;
- тестовый контур частично подтверждает качество, но частично еще не соответствует заявленным контрактам.

Итог: переписывать проект с нуля не нужно. Основа хорошая. Нужен целевой этап стабилизации ядра, запуска и session/process management.

## Что представляет собой проект

Tabula — модульный AI-агент, где:

- Go-ядро поднимает WebSocket kernel, ведет сессии, маршрутизирует сообщения, управляет процессами, хуками и встроенными tool-вызовами;
- `boot.py` собирает системный prompt, сканирует skills, подмешивает memory, slash-команды, permissions и MCP;
- Python-skills реализуют:
  - драйверы LLM (`driver-anthropic`, `driver-openai`, `driver-mock`);
  - gateway-слой (`gateway-cli`, `gateway-api`, `gateway-telegram`);
  - hooks (`hook-permissions`, `hook-logger`, `observer`);
  - tool-skills (`files`, `memory`, `weather` и др.);
  - subagent runtime и провайдеры.

Ключевая архитектурная идея правильная: kernel ничего не знает о доменной логике навыков, а skills ничего не знают друг о друге и общаются только через протокол и сессию.

## Сильные стороны проекта

### 1. Правильное разделение по языкам и зонам ответственности

Go-ядро закрывает:

- протокол;
- маршрутизацию;
- контроль подключения;
- spawn/process supervision;
- session lifecycle.

Python закрывает:

- интеграции с внешними API;
- быстро меняющиеся навыки;
- gateway UX;
- memory / MCP / hooks.

Это сильное инженерное решение: быстрый слой сверху, строгий слой снизу.

### 2. Хорошая модульность навыков

Формат `SKILL.md` + `run.py` понятный и масштабируемый. Идея, что skill описывается документацией, фронтматтером и отдельным executable-entrypoint, делает систему расширяемой без усложнения ядра.

Особенно удачны:

- автоматическое discover tools;
- slash-команды через `user-invocable`;
- отдельные bundles;
- единая shared library в `skills/lib`.

### 3. Осмысленная модель хуков

В ядре уже есть почти полноценный policy/event framework:

- `before_message`;
- `before_tool_call`;
- `session_start`;
- `before_spawn`;
- `after_*`.

Разделение hook-типов на `security`, `domain`, `observability` и разные стратегии (`void`, `modifying`, `claiming`) — зрелая идея. Это хороший фундамент для правил, аудитинга и future governance.

### 4. Продуманная модель subagents

Subagent-архитектура сделана не декоративно, а как отдельный runtime:

- отдельные session scopes;
- spawn token depth;
- parent/child separation;
- сбор результатов обратно в родителя;
- частичная обработка crash/timeout.

Это одна из сильнейших частей проекта по замыслу.

### 5. Неплохое покрытие тестами по широте

В проекте много тестов:

- kernel tests;
- hook tests;
- MCP e2e;
- subagent e2e;
- mock-driver e2e;
- observer tests;
- system prompt / boot / slash commands.

Даже с оговорками по окружению это хороший сигнал: команда уже думает категориями контрактов, а не только “ручной проверки”.

## Архитектура в текущем виде

### Поток запуска

1. `cmd/tabula/main.go` поднимает kernel.
2. Выполняется `TABULA_BOOT`.
3. `boot.py` собирает:
   - `system_prompt`;
   - `spawn`;
   - `tools`;
   - `commands`.
4. Ядро стартует HTTP/WebSocket сервер.
5. Из boot-config поднимаются процессы skills.
6. Skills делают `connect`, затем `join`.
7. После `join` драйвер получает `init` с prompt и tools.
8. Дальше идет обычный message/tool/hook loop.

### Где находится логика

- orchestration и lifecycle: `internal/kernel/*`
- boot и discovery: `boot.py`
- LLM state machine: `skills/lib/driver_runtime.py`, `skills/lib/providers.py`
- subagents: `skills/lib/subagent_runtime.py`
- gateways: `skills/gateway-*`
- memory / mcp / files: соответствующие skills

### Общая оценка архитектуры

На архитектурном уровне проект собран лучше, чем на инфраструктурном. Это хорошая новость: чинить нужно надежность, а не саму идею.

## Главные риски и проблемы

Ниже перечислены ключевые проблемы в порядке важности.

### P0. Сломан контракт установки и локального dev-онбординга

Сейчас в репозитории нет `tabula.yaml`, но он явно ожидается:

- `scripts/install-dev.sh` копирует `tabula.yaml`;
- `scripts/install-dev.ps1` тоже копирует `tabula.yaml`;
- `Makefile` использует install target, который вообще вызывает несуществующий путь `bash install-dev.sh`, а не `bash scripts/install-dev.sh`.

Следствия:

- свежая локальная установка из исходников в текущем виде выглядит сломанной;
- developer experience под угрозой;
- README и install path могут расходиться с реальным состоянием репозитория.

Это не “косметика”, а базовый operational blocker.

### P0. Есть опасный разрыв между process bookkeeping и snapshot sessions

В `skills/sessions/run.py` daemon опрашивает `/sessions`, а `internal/kernel/snapshot.go` формирует snapshot процессов через `proc.Cmd.Process.Pid`.

Проблема: в `internal/kernel/process_manager.go` для процессов, поднятых через `SPAWN`, создается “служебный” `exec.Cmd`, у которого `Process` фактически не установлен; реальный процесс живет в `Handle`.

Из этого следует риск nil-доступа в snapshot path для spawned processes. То есть:

- boot-spawned процессы выглядят безопаснее;
- tool-spawned процессы логируются иначе;
- `/sessions` и session daemon становятся уязвимыми к runtime-ошибкам именно в момент, когда subagent/process management нужен больше всего.

Это уже системная проблема модели состояния процессов.

### P1. API gateway в текущем виде не готов к нормальной параллельной нагрузке

`skills/gateway-api/run.py` использует обычный `HTTPServer`, а не `ThreadingHTTPServer`.

Практический эффект:

- один долгий streaming/SSE запрос может блокировать обработку следующих;
- gateway плохо масштабируется даже на умеренную параллельность;
- поведение будет особенно тяжелым при длинных ответах и long-lived streams.

Дополнительно сам session state там устроен так, что:

- на session хранится одна kernel connection;
- один общий queue событий;
- нет явной сериализации turn-by-turn на session;
- нет cleanup/TTL для старых session state.

Это создает риск:

- перемешивания ответов при гонках;
- накопления висящих driver-процессов;
- роста памяти и открытых соединений со временем.

### P1. Telegram gateway имеет те же lifecycle-проблемы

В `skills/gateway-telegram/run.py` session на chat создается лениво и потом живет бессрочно. Очистка есть только на shutdown.

Риски:

- долгоживущие чаты накапливают driver/process state;
- нет eviction/idle cleanup;
- при конкурентных сообщениях в один чат есть риск гонок на общем session state и общем event queue.

То есть Telegram и API gateway сейчас повторяют одну и ту же системную слабость: session lifetime продуман слабее, чем message protocol.

### P1. Контракт modifying hooks для tool calls реализован не до конца

`before_tool_call` в hook registry объявлен как `modifying`, а `PolicyEngine.CanUseTool(...)` действительно возвращает `(payload, ok)`.

Но в `ToolService.HandleToolUse(...)` модифицированный payload игнорируется: результат `CanUseTool(...)` проверяется только на block/pass, после чего выполнение продолжается со старым `msg.Input`.

Следствия:

- hook технически не может переписать tool input;
- заявленный контракт хуков шире, чем фактическое поведение;
- часть governance/use-cases не работает, хотя выглядит будто поддерживается.

Это типичный случай “архитектура уже придумана, реализация еще не догнана”.

### P1. Observer подключен к security/modifying hooks и добавляет лишний критический путь

`skills/observer/run.py` подписан не только на observability events, но и на:

- `before_message`;
- `before_tool_call`;
- `session_start`;
- `before_spawn`.

Он отвечает `pass`, чтобы не блокировать выполнение, но сам факт его участия в modifying/security chain означает:

- лишнюю синхронную зависимость;
- лишнюю latency на критических путях;
- дополнительную точку деградации архитектуры.

Для observer это неверный уровень включения. Метрики должны быть побочным наблюдением, а не участником security path.

### P1. Логика compaction не синхронизирована с дефолтной моделью OpenAI

В `skills/driver-openai/run.py` дефолтная модель — `gpt-5.4`.

В `skills/lib/compaction.py` карта context windows содержит:

- `gpt-4.1`, `gpt-5`, `o3`, `o4-mini`,

но не содержит `gpt-5.4`.

Следствие:

- для дефолтного openai path compaction threshold будет рассчитываться по fallback `DEFAULT_CONTEXT_WINDOW = 200_000`;
- это почти наверняка не то, что ожидалось авторами;
- поведение compaction для одного из главных путей уже не полностью согласовано с runtime defaults.

### P2. Поведение при конфликте tool names не согласовано с тестами и намерением

Сейчас `boot.py` в `discover_skill_tools()` при duplicate tool name оставляет первое вхождение и молча игнорирует последующие.

Но тест `tests/test_boot.py::test_duplicate_tool_override` ожидает обратное: последнее определение должно перезаписать первое.

Это важно не само по себе, а как сигнал:

- контракт расширения tools не стабилизирован;
- порядок discover может влиять на runtime;
- bundles / overrides / локальные кастомизации могут вести себя непредсказуемо.

### P2. Observer-метрики неполные как operational truth

Даже если вынести observer из modifying hooks, метрики там пока ограниченные:

- spawn помечается как `alive=True`, но обратного пути для уверенного `alive=False` нет;
- ошибки инструментов учитываются не полностью;
- метрики завязаны на hook semantics, а не на отдельный event stream/telemetry channel.

Для ранней стадии это нормально, но как основа наблюдаемости еще сыровато.

### P2. Репозиторий перегружен бинарниками и архивами релизов

В корне лежат:

- готовые бинарники;
- `.tar.gz` архивы;
- `.zip` релизы.

Размер дерева около 190 MB, значимая часть — артефакты поставки.

Это создает:

- шум в навигации по репозиторию;
- риск случайных коммитов/диффов;
- смешение исходников и release artifacts.

Для разработки и ревью это неудобно.

## Что показали тесты

### Go

Команда `go test ./...` не прошла полностью.

Наблюдения:

- часть падений вызвана ограничениями окружения: локальное тестовое bind/listen в этой среде запрещен;
- по крайней мере один failure указывает на реальную логическую проблему, а не на sandbox:
  - `TestHookVoid_FireAndForget` падает в тестовой инфраструктуре через `httptest.NewServer`, потому что текущая среда запрещает открытие локального порта;
- поэтому Go-тесты в этом окружении не дают полного сигнала о runtime качестве сетевого слоя.

### Python

`pytest tests/ -q` дал:

- 164 passed
- 30 failed

Из них:

- подавляющее большинство падений вызвано ограничением среды на bind локальных портов (`PermissionError: [Errno 1] Operation not permitted`);
- один явный продуктовый конфликт, не связанный с sandbox:
  - `tests/test_boot.py::TestDiscoverSkillTools::test_duplicate_tool_override`

Это означает:

- логическое ядро покрыто и частично стабильно;
- e2e/network-тесты завязаны на окружение;
- есть минимум один реальный разъезд между тестами и реализацией.

## Состояние инженерных контрактов

### Где контракты уже хорошие

- wire protocol между kernel и skills;
- connect/join/init lifecycle;
- разделение `driver_runtime` / `provider` / `subagent_runtime`;
- идея permissions и hooks;
- prompt assembly в `boot.py`.

### Где контракты еще “текут”

- install/dev bootstrap;
- session/process snapshot model;
- duplicate tool precedence;
- gateway concurrency model;
- hook payload mutation semantics;
- lifecycle cleanup для session-owned drivers.

## Что я бы сохранял без переписывания

Ниже то, что выглядит стратегически удачным и не требует полной переделки:

- Go kernel как центр маршрутизации;
- Python skill ecosystem;
- boot-time prompt and tool discovery;
- subagent session isolation;
- permissions через hook layer;
- единый protocol contract для skills.

То есть в проекте уже есть сильный каркас. Следующий этап — не “новая архитектура”, а hardening текущей.

## Приоритетный план доработок

### Этап 1. Стабилизация запуска и базовых контрактов

1. Починить install/dev flow.
2. Явно определить источник конфигурации:
   - вернуть `tabula.yaml`, если он обязателен;
   - либо убрать его из install scripts и документации.
3. Исправить `Makefile` install target.
4. Зафиксировать ожидаемое поведение duplicate tools:
   - first wins;
   - last wins;
   - explicit error on conflict.

Это самый дешевый и самый важный этап.

### Этап 2. Починить lifecycle процессов и сессий

1. Нормализовать модель `SpawnedProcess`:
   - PID должен быть отдельным полем, а не неявно читаться через `Cmd.Process`;
   - snapshot не должен зависеть от того, локальный это `exec.Cmd` или абстрактный `Handle`.
2. Пересобрать `/sessions` вокруг безопасного process state.
3. Добавить cleanup/TTL/idle eviction для gateway sessions.
4. Ввести session-level turn serialization там, где одна сессия обслуживается одним driver connection.

Это снимет самые опасные operational риски.

### Этап 3. Укрепить gateway-слой

1. Перевести API gateway на threaded server.
2. Явно защитить session state lock-ами.
3. Развести transport-level concurrency и session-level serialization.
4. Добавить политику завершения idle drivers.

Без этого platform story останется красивой, но хрупкой.

### Этап 4. Довести governance/observability до соответствия архитектуре

1. Либо реально поддержать modified payload для `before_tool_call`, либо упростить контракт.
2. Убрать observer из security/modifying path.
3. Разделить telemetry и policy hooks.
4. Расширить метрики процесса, ошибок и session health.

### Этап 5. Привести тестовый контур к зрелому состоянию

1. Отделить logic tests от socket/e2e tests.
2. Для e2e использовать более управляемую test harness abstraction.
3. Сделать так, чтобы sandbox-restricted среды падали предсказуемо и диагностично.
4. Добиться состояния, где:
   - unit tests стабильны без сети;
   - e2e tests явно маркированы и запускаются отдельно.

## Архитектурный вердикт

Tabula — сильный по идее проект с хорошим инженерным вкусом в базовой композиции:

- ядро маленькое и осмысленное;
- расширяемость встроена в дизайн;
- skills-модель удачная;
- subagent-история перспективная.

Главная текущая проблема проекта не в том, что он “плохо задуман”, а в том, что операционные детали еще не доведены до уровня самой архитектуры.

Если коротко:

- стратегия проекта — хорошая;
- реализация ядра — уже многообещающая;
- слабое место — lifecycle, install path, concurrency и согласованность контрактов;
- лучший следующий шаг — не расширять фичи, а провести 1 цикл hardening/stabilization.

## Рекомендуемая рабочая формула на ближайшие доработки

Использовать этот документ как основу и идти в таком порядке:

1. install/bootstrap;
2. process/session model;
3. gateway concurrency;
4. hooks contract;
5. observability;
6. только потом новые функции из roadmap.

Это даст максимальный прирост надежности без потери уже удачной архитектурной базы.

## Дополнение: новые подтвержденные находки

Ниже — то, что дополнительно подтвердилось при повторном проходе по текущему состоянию репозитория.

### P0. `gateway-api` в текущем виде выглядит фактически нерабочим

В `skills/gateway-api/run.py` модуль импортирует только часть protocol-констант, но дальше использует неимпортированные имена:

- `MSG_CANCEL`;
- `MSG_STREAM_START`;
- `MSG_STREAM_DELTA`;
- `MSG_STREAM_END`;
- `MSG_DONE`;
- `MSG_ERROR`.

Это особенно опасно потому, что синтаксически файл валиден и `py_compile` это не ловит, но при первом реальном создании session state путь `SessionState.connect()` начинает ссылаться на отсутствующие имена.

Практический вывод:

- OpenAI-compatible API path сейчас выглядит сломанным не только архитектурно, но и буквально на уровне runtime;
- проблема выше по приоритету, чем обсуждение threaded/non-threaded server, потому что сначала этот gateway нужно довести до базовой исполнимости.

### P0. Есть новый крупный разрыв между launch-скриптами, CLI contract и README

Выявилось несколько независимых, но связанных разъездов:

- `bin/tabula-server` запускает просто `tabula`, без `serve`;
- `bin/tabula-server.ps1` делает то же самое;
- при этом `cmd/tabula/main.go` без subcommand не стартует сервер, а печатает usage и завершает процесс;
- README утверждает, что `tabula-server` запускает persistent server, а в API-разделе даже подписывает его как `kernel + API`.

Следствие:

- установленный launcher `tabula-server` в текущем виде не соответствует реальному CLI-контракту;
- документация и runtime path расходятся уже на базовом сценарии запуска;
- это отдельный operational blocker, не описанный в предыдущей версии анализа.

### P0. Windows CLI launcher указывает на несуществующий путь

В `bin/tabula-cli.ps1` gateway path собран как `skills/gateways/gateway-cli/run.py`, тогда как в репозитории фактический путь — `skills/gateway-cli/run.py`.

Это означает:

- Windows launcher для CLI выглядит сломанным даже если kernel и venv установлены корректно;
- проблема не концептуальная, а точечная, но она полностью ломает один из главных entrypoint-ов для Windows.

### P1. Install path по Python-зависимостям тоже расходится с реальными skill imports

Повторная проверка installer-ов показала дополнительный operational разрыв:

- `scripts/install.sh` ставит `websocket-client`, `prompt_toolkit`, `rich`;
- `scripts/install-dev.sh` и `scripts/install-dev.ps1` ставят только `websocket-client` и `pytest`;
- при этом `skills/gateway-telegram/run.py` и `skills/mcp/client.py` используют `requests`.

Итог:

- после обычной установки часть навыков может быть физически установлена, но не исполнима из-за отсутствующих зависимостей;
- install path сейчас не формализует Python dependency graph проекта как единый контракт.

Это особенно неприятно, потому что ошибка проявляется уже не как “feature incomplete”, а как поздний runtime failure в конкретных skills.

### P1. В gateway-слое есть еще один скрытый concurrency bottleneck: lock удерживается во время сетевого handshake

И в `skills/gateway-api/run.py`, и в `skills/gateway-telegram/run.py` создание новой session происходит под общим lock, внутри которого выполняется:

- WebSocket connect;
- join;
- driver spawn;
- ожидание `member_joined`.

То есть lock держится во время операций, которые сами по себе могут занять десятки секунд.

Практические последствия:

- создание одной новой session сериализует создание остальных;
- деградация будет проявляться даже раньше, чем начнутся проблемы event-queue и lifetime cleanup;
- текущая concurrency-модель gateway-слоя слабая не только на long-lived requests, но и на холодном старте session.

### P1. Release/install/runtime story описана сильнее, чем реально собрана

При повторном проходе стало заметно, что проблема не локальна в одном файле, а системная:

- service templates ожидают `tabula serve`;
- shell wrappers частично запускают не тот CLI-контракт;
- README местами описывает запуск API и server в терминах, которые не совпадают с launcher-реальностью;
- install scripts, launchers и docs не выглядят как единый проверенный путь.

Это важный системный вывод:

- проект уже архитектурно взрослее, чем его операционная упаковка;
- следующий стабильный этап должен включать не только hardening ядра, но и полную нормализацию “install -> launch -> connect -> gateway -> stop”.

## Обновление приоритетов после дополнения

Если учесть новые находки, реальный порядок работ я бы уточнил так:

1. привести в согласованное состояние launch/install/runtime contract;
2. починить `gateway-api` до гарантированно исполнимого состояния;
3. исправить process/session snapshot model;
4. затем уже заниматься concurrency hardening и cleanup policy для gateways;
5. после этого возвращаться к hooks/observability и roadmap-функциям.

Иначе есть риск стабилизировать сложные архитектурные места, оставив сломанными самые базовые entrypoint-ы продукта.
