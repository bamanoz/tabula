14. Хорошо подходит для одного узла, хуже для распределения

  Текущее ядро естественно single-node по своей модели состояния:

  - `sessions` живут в локальном `SessionRegistry`;
  - `clients` живут в локальном `ClientRegistry`;
  - `processes` живут в локальном `ProcessSupervisor`;
  - `spawn tokens` и `pending hooks` держатся в in-memory map;
  - маршрутизация опирается на локальные mutex, локальные client pointers и локальные PID.

  Для локального runtime это нормально и даже полезно:

  - меньше latency;
  - проще reasoning;
  - честная process isolation;
  - низкая архитектурная стоимость.

  Но для multi-node почти все ключевые механизмы пришлось бы пересобирать, а не просто "обернуть сетью":

  - session routing;
  - hook dispatch;
  - child process ownership;
  - pending hook correlation;
  - snapshots;
  - session/client registries;
  - spawn token validation;
  - cancel delivery;
  - session lifecycle cleanup.

  Почему это важно

  Текущая архитектура использует локальную идентичность как часть контракта:

  - `SessionRegistry` хранит session objects в памяти процесса;
  - `ClientRegistry` хранит живые `*Client` pointers и назначает session локально;
  - `HookEngine` маршрутизирует hook result через in-memory `pending[id] -> chan`;
  - `ProcessSupervisor` считает, что владеет локальными дочерними процессами и их PID;
  - `SnapshotSessions()` собирает truth из локальных registries и локального process state.

  Это означает, что второй kernel instance сейчас не может корректно ответить на базовые вопросы:

  - кто владеет session;
  - где живет активный driver;
  - на какой node должен прийти `cancel`;
  - где ждать `hook_result`;
  - какой node имеет право убить child process;
  - какой snapshot является canonical.

  Где именно находятся текущие single-node assumptions

  - `internal/kernel/session.go`
    `SessionRegistry` — это обычная локальная map без external store и without lease/ownership model.
  - `internal/kernel/client_registry.go`
    registry держит live clients как process-local objects; это не переносится на distributed routing.
  - `internal/kernel/hook_engine.go`
    pending hooks коррелируются только через локальный channel map внутри одного процесса.
  - `internal/kernel/process_supervisor.go`
    supervisor предполагает локальный контроль над PID, signal и wait lifecycle.
  - `internal/kernel/process_manager.go`
    `Spawn()` запускает child process рядом с текущим kernel и регистрирует его локально.
  - `internal/kernel/snapshot.go`
    snapshots строятся как локальная агрегация памяти текущего узла.
  - `internal/kernel/kernel.go`
    disconnect cleanup и session removal происходят локально при уходе последнего клиента данного процесса.
  - `internal/kernel/message_router.go`
    dispatch предполагает, что sender, target session и hook handling доступны внутри одного in-memory hub.

  Что именно сломается при наивном multi-node

  1. Session routing

  Сейчас gateway или skill может отправить сообщение, а kernel определяет target session локально.
  В multi-node нужен как минимум session directory:

  - `session_id -> owner node`;
  - `session_id -> active members`;
  - `session_id -> active turn/task`.

  Иначе разные nodes смогут одновременно считать себя владельцами одной и той же session.

  2. Hook dispatch и pending correlation

  Сейчас `sendAndWait()` работает, потому что request и response живут внутри одного `HookEngine`.
  В distributed режиме нужен уже другой контракт:

  - globally unique hook request id;
  - routable reply address;
  - timeout owner;
  - persisted pending state или broker-backed correlation.

  Без этого `hook_result` легко потеряется или прилетит не на тот node.

  3. Child process ownership

  Сейчас `ProcessSupervisor` честно владеет локальными процессами:

  - он их стартует;
  - он их interrupt/kill;
  - он ждет `done`.

  В multi-node надо вводить явное различие:

  - session owner;
  - process owner;
  - scheduler/router;
  - remote executor.

  Иначе `KILL`, `LIST`, `after_spawn`, cleanup и crash recovery станут неоднозначными.

  4. Pending approvals / cancels / inflight work

  Сейчас `cancel` и session lifecycle живут как локальная side effect-механика.
  При нескольких узлах понадобится:

  - единая turn/task state machine;
  - idempotent cancel protocol;
  - ownership transfer rules;
  - inflight-operation registry.

  Иначе один node может уже завершить turn, пока другой еще считает его активным.

  5. Snapshots и observability

  Сейчас snapshot — это локальный срез памяти узла.
  В distributed системе такой snapshot перестает быть truth.

  Нужно будет выбирать одно:

  - либо centralized state store как source of truth;
  - либо event log + projectors;
  - либо shard-aware aggregated snapshot service.

  Без этого `/sessions` превращается в "что этот узел видит у себя", а не "состояние системы".

  Архитектурный вывод

  Это не делает текущую архитектуру плохой.
  Наоборот:

  - для local-first agent platform single-node ядро — рациональный выбор;
  - текущая сила `Tabula` именно в компактности и process-isolated local orchestration;
  - premature distributed rewrite сейчас только размоет сильную сторону проекта.

  Но важно честно зафиксировать ограничение:

  текущий kernel не "почти распределенный", а именно локальный orchestration runtime с хорошими seams для будущей эволюции.

  Что стоит сделать сейчас, чтобы не закрыть путь в будущее

  Не строить multi-node прямо сейчас, а подготовить архитектуру так, чтобы этот путь оставался открытым.

  1. Явно ввести ownership-модель

  Нужны отдельные идентичности:

  - `node_id`;
  - `session_owner`;
  - `process_owner`;
  - `task_owner`.

  Даже если пока все это всегда один и тот же локальный узел.

  2. Отделить identity от in-memory объекта

  Сейчас многие контракты завязаны на локальные pointers и локальные maps.
  Нужно двигаться к тому, чтобы ключевые сущности имели стабильные IDs и сериализуемый state:

  - session;
  - task;
  - approval;
  - hook request;
  - process record.

  3. Перенести truth слоя lifecycle в durable state

  В первую очередь это касается:

  - session state;
  - task state;
  - process records;
  - approvals;
  - snapshots;
  - live agent registry.

  То есть тот же `SQLite + event log + projectors`, который уже нужен `Tabula` даже без распределения.

  4. Сделать routing явным control-plane слоем

  Сейчас routing по сути растворен в `Hub`.
  Со временем лучше выделить отдельные понятия:

  - addressable session;
  - routable client endpoint;
  - message envelope;
  - delivery semantics;
  - correlation id.

  Это полезно и для single-node hardening, и для будущего federation/distribution.

  5. Развести orchestration и execution ownership

  Особенно важно для subagents и spawn:

  - orchestration может оставаться за `Tabula`;
  - execution может в будущем быть локальным или удаленным;
  - policy owner должен оставаться один.

  Это хорошо совпадает с идеей bounded executors и potential sidecars.

  Практический backlog по этому пункту

  Не делать сейчас "distributed Tabula", а добавить preparatory tasks:

  - ввести `node_id` и owner fields в session/task/process records;
  - сделать session/task/process сущности persisted и адресуемыми по ID;
  - убрать зависимость snapshot path от локального `exec.Cmd`;
  - вынести hook correlation ids в явную модель request/response;
  - зафиксировать control-plane API для `resume`, `fork`, `cancel`, `inspect`;
  - отделить process record от факта локального PID;
  - подготовить abstraction для local/remote executor без отказа от multiprocess model.

  Короткая формула

  `Tabula` сегодня должна оптимизироваться не под cluster-first distributed runtime, а под надежный single-node control plane.

  Но делать это нужно так, чтобы потом можно было эволюционировать в multi-node через:

  - durable state;
  - explicit ownership;
  - routable control plane;
  - separable execution backends.

  Это сохранит главное преимущество `Tabula` сейчас и не закроет ей следующий архитектурный шаг в будущем.
