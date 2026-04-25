# Coder Distro Plan

Статус: draft / план в работе
Цель: сделать на базе `tabula` coding-agent distro `coder`, по функциональности не уступающий `claude-code`, `codex`, `opencode`, `claw-code`, `openclaw`, сохранив минимальность ядра.

---

## 1. Принципы

- Минимальное ядро. Менять `tabula` kernel только там, где иначе не получается.
- Все, что можно сделать skill/hook/gateway — делаем skill/hook/gateway.
- Language-neutral. Python остается, TypeScript становится first-class наравне с ним.
- Skills композируются через bundles, bundles — через distro.toml.
- Coder — это не один skill и не кастомный kernel. Это набор bundles + gateway-tui + distro.

---

## 2. Архитектура `coder`

### 2.1 Компоненты

- `coder` distro (`tabula-distrib/coder`)
- TUI gateway на TypeScript (`gateway-tui`)
- опциональный API gateway (`gateway-api`) — позже
- набор bundles:
  - `coder-git`
  - `coder-tasks`
  - `coder-subagents`
  - `coder-workspace` (approvals + boundary policy)
  - `coder-review`
  - улучшенный `mcp` в `base` (bridge → first-class tools)
  - использование существующих `files`, `drivers`, `memory`, `base`
- TypeScript SDK (`skills/_tslib` в kernel repo или отдельный npm-пакет)

### 2.2 Поток данных

Пользователь → `gateway-tui` → kernel → driver → tools / subagents / hooks.
Structured tool metadata (diff, touched files, cmd summary) пробрасывается обратно в TUI.

### 2.3 Что меняем в ядре

Минимально:
1. Опциональный `ask_user` / approval primitive (runtime request/reply), если hook-only подход окажется слишком кривым для UX.
2. Session-level `project_root` / `cwd` concept, доступный всем skills.
3. Расширение `tool_result` — опциональные structured поля: `diff`, `files_touched`, `summary`, `attachments`.
4. Все остальное — skills.

---

## 3. Стек

- Kernel: Go, без изменений кроме пунктов выше.
- `skills/_pylib` (Python) — остается.
- `skills/_tslib` (TypeScript) — новый, first-class SDK.
- `coder-tui` — TypeScript на **Ink + React** (как `claude-code` / `openclaw`). Bun runtime для скорости старта.
- Tool skills:
  - `coder-workspace`, `coder-git`, `coder-tasks` → **Python** (консистентно с `base`/`files`/`memory`, меньше boilerplate для subprocess/fs, быстрее MVP).
  - `gateway-tui` → **TypeScript** (Ink + React, первый TS-потребитель tslib).
  - `coder-review`, `coder-subagents` — решим по факту; TS становится первым вариантом, если потребуется переиспользовать парсеры/рендер из TUI.

---

## 4. Ответы на вопросы пользователя

- 5 `workspace`: выделяется в bundle `coder-workspace`. Не специфично для gateway. В нем живут и boundary-hook, и approvals-UX-contract, и `project_root` helpers.
- 6 `approvals`: кладем рядом с `hook-permissions`, в тот же bundle `coder-workspace` (или, если hook-permissions уехал в `base`, формируем поверх него policy-pipeline как отдельный bundle). Главное — approvals и boundary должны шариться между distros.
- 8 `mcp`: дорабатываем прямо в `base`, превращая текущий `familiar/skills/mcp` bridge в полноценный bundle `base/mcp` с first-class tool registration через driver.
- Subagents: текущая реализация (familiar) слаба. Выделяем в отдельный bundle `coder-subagents` (возможно позже промоутнем в `base`). См. раздел 6.

---

## 5. Bundles

### 5.1 `coder-git`
Skills:
- `git` (один skill, много tools): `git_status`, `git_diff`, `git_staged_diff`, `git_log`, `git_show`, `git_add`, `git_commit`, `git_branch`, `git_checkout`, `git_stash`, `git_blame`.
- tools возвращают structured output (hunks, files touched).
- нет авто-push. Нет авто-force. Никогда.

### 5.2 `coder-tasks`
Skills:
- `todo`: `todowrite`, `todoread` — session-scoped todo list, persisted.
- `tasks`: более долгоживущий task queue (для batch/background операций), если понадобится отдельно от todo.

### 5.3 `coder-subagents`
См. раздел 6. Отдельный bundle.

### 5.4 `coder-workspace`
Skills:
- `workspace`: tools для project_root/cwd introspection (`workspace_info`, `workspace_set_root`).
- hook `hook-workspace-boundary`: enforces read/write/bash границы по repo root.
- hook `hook-approvals`: interactive approval flow поверх существующего `hook-permissions`.
  - действия: allow once / allow always / deny once / deny always.
  - persistence: `~/.tabula/config/skills/hook-approvals/rules.json`.
  - matching: tool + pattern (path, command prefix, host).

### 5.5 `coder-review`
Skills:
- `review`: `diff_preview`, `review_plan`, `review_patch`.
- ориентирован на workflow "показать diff до apply", "сгенерить review comments".
- использует `files.apply_patch` под капотом, но добавляет UX/метаданные для TUI.

### 5.6 Улучшения в `base/mcp`
- Перевод familiar MCP bridge в `base/mcp` bundle.
- MCP tools регистрируются как обычные `tools` в boot output.
- Driver видит их в одном `tools` списке, без prompt/EXEC хардкода.
- Optional: OAuth flow для MCP как в `opencode`/`codex`.

---

## 6. Subagents: redesign

Текущее состояние (familiar): driver спавнит subagent через `process_spawn`, дочерний процесс гоняет свой driver_runtime и возвращает итог текстом. Это слабо по сравнению с codex/claude/opencode.

### 6.1 Новый bundle `coder-subagents`

Tools:
- `subagent_spawn(type, prompt, inputs?, model?, cwd?, allowed_tools?, timeout?, mode: "sync"|"async", session?)`
- `subagent_send(id, message)`
- `subagent_wait(id, timeout?)`
- `subagent_list()`
- `subagent_kill(id)`
- `subagent_steer(id, instruction)` — вставить новое user message в активную сессию субагента

Design:
- Registry субагентов: `$TABULA_HOME/state/subagents/*.json` (id, parent session, status, cmd, allowed tools, timestamps).
- Lifecycle events: `subagent_spawning`, `subagent_spawned`, `subagent_ended` (как hooks).
- Subagent “types”: preset — prompt + allowed_tools + model. Определяются в `coder-subagents/types/*.toml` или `.json`.
  - стартовый набор: `explore`, `plan`, `review`, `fix`, `general`.
- Subagent isolation:
  - собственная kernel session,
  - собственный cwd (может быть отдельный git worktree — phase 2),
  - allowed_tools prepopulated и enforced через `hook-approvals` или policy pipeline.
- Async mode: parent driver может продолжать работу, получая результаты через `subagent_wait` или streaming.
- Steering: первый-класс, не только "снова spawn".

### 6.2 Kernel hooks нужные для этого
- уже есть `before_spawn` / `after_spawn`. Достаточно.
- добавить (если нужно) механизм доставки `subagent_ended` — можно через existing session messages.

### 6.3 Что убираем из familiar
- familiar продолжает работать на существующем простом subagent-стеке (обратная совместимость).
- coder использует новый `coder-subagents`.
- позже можно мигрировать familiar на него же.

---

## 7. TUI (gateway-tui)

TypeScript, отдельный repo/package (решим: сначала под `tabula-distrib/coder/skills/gateway-tui/`).

Компоненты:
- thread view (messages, tool calls, tool results).
- live streaming deltas.
- tool activity pane (запущенные tool calls, их status).
- approvals modal (interactive allow once/always).
- todo pane.
- subagents pane (list, steer, kill).
- diff preview modal (использует structured tool metadata).
- session list / resume.
- slash commands: `/resume`, `/compact`, `/model`, `/agents`, `/todo`, `/diff`, `/approvals`, `/sessions`.
- keybindings: vim-like опционально.

Framework: **Ink + React** (решено). Runtime: Bun. Размещение: внутри `tabula-distrib/coder/skills/gateway-tui/` (вынесем в отдельный репо позже, если не будет coder-specific логики).

Готовые блоки из экосистемы Ink, которые планируем использовать:
- `ink-spinner`, `ink-text-input`, `ink-select-input`
- `ink-table`, `ink-link`, `ink-big-text`
- `ink-syntax-highlight` для diff-рендера
- `ink-gradient` для брендинга
- собственные компоненты для approval modal, tool activity, subagent pane

---

## 8. TypeScript SDK

Цели:
- писать skills на Node/Bun/Deno без boilerplate.
- строить gateway-tui на нормальном стеке.

Поверхность:
- `connect({ name, sends, receives, hooks, token })`
- `join(sessionId)`
- `on("message" | "tool_use" | "init" | "hook" | ...)`
- `send({ type, ... })`
- `tool(name, handler)` — для tool subprocess skills (JSON stdin → JSON stdout).
- `hook(event, handler)`
- types: protocol messages, init payload, tool_use/tool_result.

Реализация:
- либо `skills/_tslib` внутри `tabula` (параллельно `_pylib`),
- либо отдельный npm-пакет `@tabula/skill-sdk`.
- Решение: начинаем в репозитории `tabula`, в `skills/_tslib/`, публикация в npm — позже.

---

## 9. Kernel changes (минимум)

1. `project_root` в session:
   - опциональное поле при `join` / `init`.
   - доступно skills через `init.context.project_root`.
2. Structured `tool_result` метаданные:
   - optional `metadata.diff`, `metadata.files`, `metadata.summary`.
   - kernel их не интерпретирует, только передает.
3. Approval primitive (только если hook-only flow окажется неудобным):
   - `ask` message: skill → kernel → gateway.
   - `ask_result` message: gateway → kernel → skill.
   - Сначала пробуем обойтись hooks. Решение по итогам phase 1.
4. TS SDK: `skills/_tslib/` — не меняет kernel, но в этом же repo.

---

## 10. Roadmap (этапы)

### Phase 0 — Preparation
- написать этот план (done).
- решить: Ink vs OpenTUI — **Ink + React** (done).
- решить: размещение TUI — **внутри `tabula-distrib/coder/`** (done, вынесем позже если общим окажется).
- Set up `skills/_tslib` skeleton.

### Phase 1 — Foundations
- `skills/_tslib` (TypeScript SDK, minimal: connect/join/send/recv, tool helper).
- kernel: добавить `project_root` в init/context.
- kernel: расширить tool_result metadata (optional structured fields).
- tests для обоих.

### Phase 2 — Core coder bundles  — **done (Python, в `~/src/tabula-bundles/`)**
- `coder-workspace` bundle (`~/src/tabula-bundles/coder-workspace/`):
  - `workspace` skill: `workspace_info`, `workspace_set_root`.
  - `hook-workspace-boundary`: блокирует file-tool вызовы с путями вне `TABULA_PROJECT_ROOT`, парсит `apply_patch`.
  - `hook-approvals`: file-based allow/deny правила по `tool`/`path`/`command` со specificity + deny-wins; интерактив (allow-once) — в Phase 5 вместе с TUI.
- `coder-git` bundle (`~/src/tabula-bundles/coder-git/`):
  - `git` skill: `git_status`, `git_diff`, `git_staged_diff`, `git_log`, `git_show`, `git_add`, `git_commit`, `git_branch`, `git_checkout`, `git_stash`, `git_blame`.
  - diff возвращается в structured JSON (files → hunks → lines, + stats).
  - нет `git_push` и force-операций; `git_checkout` отказывается переключаться на грязном дереве.
- `coder-tasks` bundle (`~/src/tabula-bundles/coder-tasks/`):
  - `todo` skill: `todoread`, `todowrite`, persist в `$TABULA_HOME/state/todo/<session>.json`, инвариант "≤1 in_progress".

### Phase 3 — Subagents redesign — **done (Python, в `~/src/tabula-bundles/coder-subagents/`)**
- `coder-subagents` bundle:
  - registry: `$TABULA_HOME/state/subagents/<id>.json` + `.result.txt` + логи.
  - `subagent_spawn/send/wait/list/kill/steer`: spawn — detached subprocess поверх `drivers/subagent-{openai,anthropic}`; pid-based reconciliation; `send`/`steer` пушит MSG_MESSAGE в `subagent-<id>` сессию через KernelConnection.
  - types: `explore`, `plan`, `review`, `fix`, `general` в `types/*.toml` (provider, model, max_turns, allowed_tools, system_suffix).
  - `mode=sync|async`; sync блокирует через `subagent_wait`.
- TODO: подключение к distro `coder` (Phase 6); enforcement `allowed_tools` через hook-approvals (сейчас записывается только в registry).
- Формат types: TOML (вопрос из раздела 13 закрыт — не MD; frontmatter избыточен для скалярных пресетов, тело прогноза кладём в `system_suffix`).

### Phase 4 — MCP уровня выше — **done (в `~/src/tabula-bundles/base/mcp/`)**
- Перенос `familiar/skills/mcp` → `base/mcp` bundle: `client.py`, `pool.py`, `daemon.py`, `run.py`, `register.py`, `SKILL.md`, `SKILL.config.json`. Код фреймворк-нейтральный; familiar/distrib позже переключится на общий bundle (MVP: одноразовый параллельный импорт, потом выпиливание).
- First-class tool registration: `mcp.register.mcp_tool_entries(venv_python=...)` отдает kernel-format записи `mcp__<server>__<tool>` с `exec` на `skills/mcp/run.py tool mcp__...`. Distro `boot.py` зовет хелпер и расширяет `tools[]`. LLM видит MCP-тулзы в tool-list наравне с нативными скиллами.
- `run.py` раздваивается: (a) legacy CLI (`discover/list/call/pool`) — сохранен; (b) skill-tool mode (`tool <name>` + JSON stdin) с хендлерами `mcp_list_servers/mcp_discover/mcp_list_tools/mcp_call` плюс динамический dispatch `mcp__<server>__<tool>`.
- Результаты MCP flat-ятся в `{text, attachments?, isError?}` для driver'а. Небезопасные имена (вне `[A-Za-z0-9_]` или содержат `__`) пропускаются с warning'ом на stderr.
- `TABULA_SKIP_MCP=1` отключает MCP на boot (offline/тесты).
- Не делали: OAuth flow — отложен, как и планировалось. Resources/prompts тоже отложены (нужны только когда появится реальный use case).

### Phase 5 — TUI MVP — **done (TypeScript/Ink, в `~/src/tabula-distrib/coder/skills/gateway-tui/`)**
- `gateway-tui` skill:
  - `src/session.ts`: Gateway class (WebSocket → kernel), MSG_CONNECT/JOIN/MESSAGE/CANCEL, streaming state machine, ThreadEntry events.
  - `src/tools.ts`: `callSkillTool` helper (spawns `run.py tool <name>` with JSON stdin).
  - `src/slash.ts`: SLASH_COMMANDS registry (`/help`, `/quit`, `/exit`, `/clear`, `/cancel`, `/todo`, `/agents`, `/diff`, `/approvals`, `/sessions`, `/model`).
  - `src/App.tsx`: top-level Ink component, wires Gateway events → React state, panel routing, approval modal.
  - `src/index.tsx`: entry point (`bun run src/index.tsx --session X --provider Y`).
  - `src/components/Thread.tsx`: renders user/assistant/tool_use/tool_result/status/error entries.
  - `src/components/Input.tsx`: ink-text-input with slash-command autocomplete.
  - `src/components/StatusBar.tsx`: session id, provider, turn state.
  - `src/components/Spinner.tsx`: ink-spinner wrapper.
  - `src/components/Approval.tsx`: modal (a=allow-once, A=allow-always, d/D/x).
  - `src/components/panels/Todo.tsx`: invokes `todoread` tool.
  - `src/components/panels/Agents.tsx`: invokes `subagent_list` tool.
  - `src/components/panels/Diff.tsx`: invokes `git_diff`+`git_staged_diff` tools.
- TODO: integrate with distro boot (Phase 6); hook-approvals ask primitive wiring (Phase 7).

### Phase 6 — Distro `coder` — **done**
- `tabula-distrib/coder/distro.toml` — bundles (git+): base, files, drivers, memory, coder-workspace, coder-git, coder-tasks, coder-subagents. `coder-review` отложен в Phase 7.
- `tabula-distrib/coder/distro.override.toml` — те же bundles, source=`local:/Users/mak/src/tabula-bundles/*` для dev-инсталла.
- `tabula-distrib/coder/boot.py` — вариант familiar/boot.py без system-prompt сборки (роль prompt-builder берёт на себя driver). Добавлен `discover_mcp_first_class_tools()` → `mcp.register.mcp_tool_entries(...)`. Spawn исключает `gateway-tui` (её поднимает пользователь вручную из TTY).
- `scripts/install-coder.sh` — зеркало `install-familiar.sh`: ставит kernel через `install-dev.sh` затем `tabula-distro install $REPO/../tabula-distrib/coder` (fallback на git+).
- Smoke: `tabula-distro --home /tmp/tabula-coder-smoke install .../tabula-distrib/coder` → 42 skill-tools, 4 kernel-tools, spawns sessions/hook-logger/hook-workspace-boundary; ошибок нет.
- TODO: enforcement `allowed_tools` subagents через hook-approvals; написать e2e-тест `gateway-tui ⇄ kernel ⇄ driver`.

### Phase 7 — Review / Diff UX — **done**
- `coder-review` bundle (`~/src/tabula-bundles/coder-review/`):
  - `review` skill с тулзами `diff_preview`, `review_plan`, `review_patch` (все read-only; реальный apply остаётся за `files.apply_patch`).
  - `diff_preview` возвращает структурированный JSON (unstaged + staged + summary/by_file) — тот же формат, что `git_diff`.
  - `review_plan` сканирует added-строки на TODO/FIXME/XXX/HACK/print(/console.log/dbg!/breakpoint(/debugger, бакетирует файлы (small ≤20, medium ≤200, large), перечисляет untracked и строит conventional-commit prefix (docs/test/ci/feat) + scaffold.
  - `review_patch` различает envelope-форму (`*** Begin Patch`) и unified diff; для unified пускает `git apply --check` + `--stat` через временный файл; для envelope парсит Add/Update/Delete/Move File: строки.
- `distro.toml` / `distro.override.toml` coder расширены записью `coder-review`.
- `gateway-tui`:
  - добавлен `src/components/panels/Review.tsx` — modal с summary (files, +/−), per-file glyphами (+/-/→/M), checklist, suggested commit.
  - slash-команда `/review` и `PanelId="review"` в `slash.ts`/`App.tsx`.
  - `bunx tsc --noEmit` чист.
- Smoke (`/tmp/tabula-coder-phase7`): 9 bundles, 45 skill-tools (+3 review-tools), boot чист.
- TODO (Phase 8): approval-UX интеграция с `apply_patch` через kernel ask-primitive; пока что approval modal в TUI остаётся заглушкой.

### Phase 8 — Productization

**8a — Approval-UX через before_tool_call hook (done)**
- Kernel: `HookSubscription.TimeoutMs *int` (`internal/kernel/hooks.go`).
  Семантика: `nil` → дефолт 5s; `0` → ждать до disconnect клиента; `>0` →
  переопределение в мс.
- `internal/kernel/hook_engine.go` `sendAndWait` теперь принимает `hookEntry`,
  селект на `<-ch / <-c.Done() / <-time.After(...)`. Для `HookSecurity` любой
  silent-исход (timeout/disconnect) ⇒ block (fail-closed). Это и есть нужное
  поведение, когда TUI отвалился до ответа.
- `Client.Done()` (`internal/kernel/client.go`) — закрывается в `closeSend`/
  `MarkClosed`, чтобы интерактивный хук не висел вечно.
- `coder-workspace/hook-approvals/run.py`: на ALLOW-правило теперь шлёт
  HOOK_MODIFY с payload, в котором `input.approved=true` (был просто PASS).
  Это сигнал нижестоящему интерактивному хуку «не спрашивай — уже одобрено».
- `gateway-tui` сам становится `before_tool_call` хук-подписчиком с `priority=10,
  timeout_ms=0`. На fire: если `input.approved===true` или tool не в
  `APPROVAL_TOOLS` (`apply_patch`, `write`, `edit`, `multiedit`, `shell_exec`,
  `process_spawn`) → авто-PASS; иначе пробрасывает `onApproval`. App.tsx
  `handleApproval` теперь шлёт `MSG_HOOK_RESULT pass|block` через
  `Gateway.replyApproval(hookId, choice)`. `abort` дополнительно зовёт
  `gateway.cancel()`.
- Тесты: `TestBeforeToolCallHook_InfiniteTimeout_RepliesAfterDelay` (хук висит
  6 секунд, потом отвечает — tool выполняется), `..._DisconnectBlocks`
  (subscriber отваливается без ответа — tool блокируется с
  `blocked by hook`). `bunx tsc --noEmit` чист.
- TODO: persistence allow-always/deny-always (CRUD над `rules.json` из TUI);
  `apply_patch`-специфичный prompt (диф/файлы) вместо плоского `Approve write?`.

**8b — Productization (далее, не сделано)**
- `gateway-api` (OpenCode-style local HTTP API).
- optional sandbox mode (docker backend как в guardian, но generalized).
- worktree-based subagents.
- session compaction/summarization improvements.
- editor integration (VS Code) — далеко.

---

## 11. Что НЕ делаем

- Не переписываем kernel.
- Не делаем собственный model provider stack (используем драйверы).
- Не делаем собственный MCP сервер/клиент с нуля — обертываем существующий.
- Не делаем web UI в MVP.
- Не ломаем familiar/guardian/ouroboros.

---

## 12. Риски

- TUI performance: Ink на сильно динамичных сценах может тормозить. Mitigation: профилировать, при необходимости мигрировать критичные панели.
- Сandbox story: чтобы не отставать от codex, рано или поздно нужен нормальный sandbox; это отдельный большой проект. На MVP живем с approvals + boundary.
- MCP integration: может потянуть за собой расширение protocol (resources/prompts). Держим в уме.
- TS + Python параллельно: риск двойной поддержки. Решение: tslib покрывает только то, что реально нужно coder distro; мы не дублируем Python бездумно.

---

## 13. Открытые вопросы

- Нужен ли kernel-level approval primitive или hooks хватит.
- Формат subagent types: TOML/JSON/MD (скорее MD + frontmatter, как SKILL.md).
- Формат diff metadata в tool_result: унифицированный JSON schema, надо зафиксировать на старте phase 1.

---

## 14. Немедленные следующие действия

1. Phase 0: done.
2. Phase 1 kickoff:
   - создать `skills/_tslib/` skeleton.
   - реализовать TS connect/join/send/recv.
   - добавить `project_root` в kernel init.
   - добавить optional structured tool_result metadata в protocol.
   - написать тесты.

Поехали с Phase 1.
