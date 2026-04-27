# Competitive Lessons

Дата: 2026-04-26
Status: living distillate

## Цель документа

Это **сжатая выжимка** из шести сравнительных анализов
(`*_VS_TABULA.md` — Claude Code, Codex, Decepticon, DeepAgents, OpenClaw,
OpenCode) и `ANALYSIS.md`. Сами сравнительные документы удалены — они
исчерпали свою задачу. Здесь оставлено только то, что должно превратиться
в фичу или архитектурный принцип Tabula.

Все рекомендации фильтруются через текущие архитектурные коммиты:

- runtime для гетерогенных агентов, не coding-assistant;
- микро-Go-kernel + skills (per-call) + plugins (long-lived `register(api)`);
- никаких kind/capabilities — см. [SKILL_PLUGIN_ARCHITECTURE.md](SKILL_PLUGIN_ARCHITECTURE.md);
- sandbox / opencode integration / harness — отдельные направления.

Идеи, конфликтующие с этой моделью, не включены.

## Где Tabula уже сильна (не трогаем)

- маленькое Go-ядро как центр маршрутизации, lifecycle и протокола;
- **skills и plugins как реальные OS-процессы**, а не in-process импорты
  (см. ниже);
- subagents как процессы с собственным WS-подключением, а не in-process tasks;
- protocol-first orchestration; границы системы — реальные wire boundaries;
- multi-language extensibility (Python и TS как first-class, не bindings);
- hooks как kernel primitive, а не «UI-слой поверх».

### Почему процессы, а не in-process plugins

OpenClaw, OpenCode, Claude Code, DeepAgents все грузят плагины как
JS/TS-модули в собственный host runtime. Быстрее на dispatch, но цена:

- **crash blast radius**: ошибка плагина роняет хост целиком;
- **memory isolation**: плагин делит heap/GC с ядром;
- **language lock-in**: один host runtime (Bun/Node или Python);
- **hot reload**: невозможен без рестарта всей системы;
- **sandbox per-component**: невозможен без отдельного PID;
- **API leakage**: внутренние interface'ы host'а доступны импортом.

У Tabula плагин — отдельный subprocess, общается с kernel по stdio
JSON-RPC. Мы платим IPC-латентностью; получаем изоляцию,
multi-language и реальные wire boundaries. Любая новая подсистема
должна **усиливать** эту границу, а не размывать её.

## Что стоит заимствовать (приоритезировано)

Порядок отражает приоритет реализации, не источник.

### 1. Skill/plugin manifest formalization

После `SKILL_PLUGIN_ARCHITECTURE.md` это **prio-0**.

- `plugin.toml` манифест с `id`, `version`, `entry`, `runtime`, `configSchema`,
  `tags` (OpenClaw `*.plugin.json`).
- `register(api)` API для long-lived плагинов (OpenClaw plugin SDK).
- `tools[].exec` в SKILL.md остаётся; `requires-kernel-tools` уходит.
- Validation на boot, не на runtime (OpenClaw manifest registry, OpenCode plugin
  loader).

### 2. Permissions как first-class subsystem

Базис уже есть: `base/hook-permissions` (kernel-level allow/deny) и
`coder-workspace/hook-approvals` (file-based pattern rules с
`allow_always`/`deny_always` по `tool`/`path`/`command`,
subagent allowed_tools enforcement, `add_rule` через `MSG_MESSAGE.meta.rule_add`).
Не переписывать — расширять и переводить в plugin-форму.

Что добавить (поверх существующего):

- мигрировать `hook-permissions` и `hook-approvals` в plugin'ы (long-lived,
  state в памяти; rules.json как persisted view);
- единый контракт outcome: `allow / deny / ask` (Claude Code
  `PermissionContext`, Codex risk levels) — сейчас есть только
  `allow_always`/`deny_always`;
- `ask` ветка с persisted approval (session-aware);
- gateway-agnostic approval UX (одна модель работает в TUI/CLI/API/Telegram);
- per-agent permissions tree (immune-harness `permission: {task: {...},
  edit: {...}, bash: {...}}`) — как расширение rules-формата;
- permission scope types: `tool`, `exec`, `network`, `external_directory`,
  `workspace_write` (Claude Code, Codex; первые два уже есть de facto через
  fnmatch на `tool`/`command`).

### 3. Session lifecycle и durability

Самая большая системная слабость Tabula по сравнению с OpenCode/Claude Code.

- Canonical entities: `Session`, `Turn`, `Task`, `Agent`, `Artifact`,
  `Approval`, `Project` (OpenCode).
- Session state machine: `busy/idle/running/cancelled/failed/completed`
  (OpenCode `SessionRunState`).
- Session-level turn serialization встроен в core, не в каждом gateway
  отдельно (OpenCode `SessionProcessor`).
- Idle TTL / eviction / cleanup для gateway sessions (Claude Code).
- Resume / fork / replay / export как control-plane операции
  (OpenCode session API).
- Append-only event log + materialized projector model (OpenCode `storage/`).
- Локальный durable store: SQLite или, для компактности, JSON append-only
  начально (OpenClaw `task-flow-registry.store.sqlite`).

### 4. Typed tools

- Tool descriptor: input schema, optional output schema, validation, activity
  description, progress semantics (Claude Code `Tool.ts`, OpenCode `tool/`).
- Schema-driven param validation в kernel до dispatch (OpenClaw plugin SDK).
- Structured tool metadata в результате (touched files, diff, exit code) для
  TUI rendering (Claude Code, opencode).

### 5. Subagent control surface

Самые конкретные improvements, переносимые из OpenClaw.

- Tool allowlist на spawn (`process_spawn --tools "read,grep"`); kernel
  фильтрует init.tools (OpenClaw `agent-scope`).
- `subagent_control` tool с `kill / steer / wait / list / info` (OpenClaw
  `subagent-control.ts`); throttling 2s на steer.
- Run registry с persisted state (started_at/finished_at/exit_reason/
  turn_count) — JSON append-only.
- Lifecycle hook events: `subagent_started/turn/finished/failed`.
- Orphan recovery: при exit subagent-процесса parent driver получает
  синтетическое `<error>subagent died</error>` за <1s, не ждёт TOOL_RESULT_TIMEOUT.

### 6. Tool orchestrator (centralized)

Сегодня tool dispatch размазан между `tool_service.go` и `hook_engine.go`.

- Один `ToolOrchestrator`, владеющий: approval flow, sandbox selection
  (когда появится), retry policy, escalation, telemetry decisions
  (Claude Code, OpenCode).
- Modifying-hook payload реально применяется к `tool_use.input` (это уже
  закрыто в P0 текущим backlog'ом, оставляю как контракт).

### 7. Operator tooling

Сегодня его почти нет.

- `tabula doctor`, `tabula health`, `tabula setup`, `tabula sessions`,
  `tabula inspect <session-id>` (OpenCode CLI, OpenClaw doctor).
- Health/readiness HTTP surface (OpenCode `/health` уже есть, расширить).
- Audit log как отдельный stream, не побочный продукт observer (OpenClaw
  `security/audit.ts`).

### 8. Plan mode

Решается через **выбор агента**, не отдельный режим. В `coder` distro уже
есть зачатки: агент `plan` (`coder/agents/plan.md` — "Focus on architecture,
tradeoffs, sequencing, and risks. Do not make code changes unless the user
explicitly asks") и `/agent` slash для переключения. Дальнейшая работа:

- довести permissions tree per-agent (см. §2), чтобы у `plan` была реально
  read-only капабилити-маска, а не просто prompt-instruction;
- расширить набор стандартных агентов (`plan`/`build`/`explore`) в base
  templates, чтобы каждая distro не описывала их с нуля.

Отдельный `/plan` mode из Claude Code НЕ нужен — agent selection это
покрывает.

### 9. Compaction

Уже частично есть, но не докручено.

- Hook event `before_compaction` — memory plugin может flush state до
  summarization (Claude Code).
- Auto-compaction trigger по token threshold (configurable).
- Compaction context windows синхронизированы с дефолтными моделями
  (см. ANALYSIS P1: gpt-5.4 не в карте).

### 10. Headless / non-interactive mode

**Уже есть.** `tabula run [-p PROMPT] [-t TIMEOUT] [--json]` — one-shot
exchange через `kernel.RunOneShot` (`cmd/tabula/main.go:259`,
`internal/kernel/oneshot.go:18`). Stdin поддерживается если `-p` не задан.

Что докрутить (опционально, не P0):

- структурированный stream (`--stream` для NDJSON событий) для
  CI-интеграций;
- `--session <id>` для resume в headless-режиме (после §3).

### 11. Tasks как first-class

Не путать с subagents.

- `Task` сущность с `id / lifecycle / progress / artifacts / notification /
  timeout / cleanup_policy` (OpenCode tasks, OpenClaw task-registry).
- Связь tasks ↔ session ↔ subagent ↔ process state.
- Background jobs / cron / detached work строятся **поверх** task layer, а
  не отдельными ad-hoc обходами (cron plugin использует tasks).

### 12. Multi-agent guardrails

- Live registry активных агентов и child sessions (OpenClaw).
- `max_depth`, `max_threads`, ownership (`session_owner`, `process_owner`,
  `task_owner`) — переезжают **внутрь** subagent plugin'а (см.
  SKILL_PLUGIN_ARCHITECTURE §4.5).
- Final-state reconciliation для зависших child sessions.

### 13. Snapshot / diff / revert

- Track file modifications за session; `/rewind` или `/undo` для отката
  (Claude Code, OpenCode `snapshot/`).
- Реализация: git snapshots или before/after copies на FS plugin уровне.

### 14. Project / workspace abstraction

- `project_root`, `session_owner`, `child_session`, `artifact_root`,
  `replay/export target` (OpenCode `project/`, `specs/project.md`).
- Workspace boundary как plugin (уже есть как `coder-workspace/hook-workspace-boundary`,
  расширить).

### 15. Custom slash commands

- User-defined slash как markdown в workspace skills с `user-invocable: true`.
  Уже поддерживается; нужно довести UX и documentation.

### 16. Distributed-readiness без раннего distributed rewrite

- Явные ownership поля (`node_id`, `session_owner`, `process_owner`).
- Routable correlation IDs для hooks/approvals/cancel.
- Process records отдельно от локального PID.
- Abstraction для local/remote executor (без реализации remote сейчас).

### 17. Observability

- Telemetry стрим отдельно от security/modifying hooks (observer
  не должен сидеть в critical path — это известный P1 из ANALYSIS).
- Structured event log для inspection (OpenCode events).
- Метрики: spawn alive/dead, tool errors, session health, approval rates.

### 18. Tool/skill/plugin marketplace primitives

**Только как отдельный тулинг, не внутри ядра.** Kernel ничего не знает
про install/discovery — это работа `tabula-distro` и подобных CLI.

- `tabula-distro install <git-url>` для skills/plugins (расширение
  существующего bundle install) — отдельный CLI surface.
- Trust/health/version/install metadata — поля в `plugin.toml` /
  `bundle.toml`, валидируются установщиком.
- Discovery / search / registry — отдельный сервис, не часть kernel.

Marketplace как продукт — anti-goal (см. секцию ниже).

## Anti-goals (что НЕ берём)

Эти паттерны привлекательны, но не подходят Tabula или активно конфликтуют с
её архитектурной ставкой.

- **In-process plugin host**. OpenClaw, OpenCode и Claude Code держат
  плагины внутри одного host runtime. У нас плагины — отдельные процессы,
  это base bet.
- **Монолитный TS-host runtime** (OpenCode/Claude Code). Tabula — Go-kernel,
  не in-process platform.
- **Несколько параллельных policy systems**. Только одна (см. §2).
- **Desktop / mobile companion apps как раннее направление**. Возможно
  позже, не сейчас.
- **Skill marketplace как продукт**. Только primitives (§18).
- **Voice / Discord / browser automation как core**. Plug-in territory,
  не roadmap-уровень.
- **AI guardian как обязательный path** (Codex). Опциональный plugin,
  если кому-то нужно.
- **Богатый remote/IDE/web surface** до стабилизации kernel.
- **Distributed multi-node now**. Только distributed-readiness (§16).
- **DeepAgents-style in-process delegation**. У нас subagent — процесс.
  Возможно как опциональный bounded specialist plugin (§5.1 в старом
  backlog'е), но без замены core.
- **Decepticon-style kind/capabilities в манифесте**. Сознательно не делаем.

## Источники

- Claude Code, OpenCode, OpenClaw — основной вклад в permissions, sessions,
  manifests, operator tooling, tasks.
- Codex — risk levels, sandbox layers, AI guardian (как опция).
- DeepAgents — bounded specialist в качестве опции.
- Decepticon — отрицательный пример kind-based dispatch (избегаем).
- ANALYSIS — конкретные операционные баги Tabula (большая часть закрыта в
  P0 backlog, оставшееся переехало в этот документ).
