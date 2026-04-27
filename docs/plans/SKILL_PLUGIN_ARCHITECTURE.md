# Skill / Plugin Architecture

Status: design (approved 2026-04-26), implementation not started.
Scope: kernel boundary, manifest formats, bundle layout, migration plan.
Out of scope: sandbox, opencode integration, harness distro (отдельные документы).

## 1. Motivation

Текущая абстракция «skill» в Tabula перегружена. Один и тот же манифест `SKILL.md` сегодня обслуживает четыре фундаментально разные роли:

1. **Tool provider** — короткоживущий subprocess per call
   (`coder-git`, `coder-tasks`, `files`, generic `mcp_*` tools).
2. **Hook handler** — долгоживущий обработчик bus-событий
   (`hook-permissions`, `hook-approvals`, `hook-workspace-boundary`, `caveman`).
3. **Gateway / driver / runtime** — долгоживущий процесс с собственным lifecycle и
   внешним transport'ом (`gateway-tui`, `gateway-cli`, `gateway-api`,
   `gateway-telegram`, `drivers/driver`, `drivers/subagent`, `consciousness`).
4. **Library** — shared-код без LLM-tool surface, опознаётся по `_` префиксу
   (`_pylib`, `_tslib`, `_drivers`, `_subagent_types`).

Симптомы: `skills = []` allowlist hack в distro для скрытия libs из каталога;
`requires-kernel-tools` в frontmatter как способ повлиять на kernel; `_`
префикс как convention; ad-hoc launchers под TUI/gateway; параллельные
hook-skill контракты per-call. Каталог LLM-tools размазан между kernel
(`shell_exec`, `process_*`) и skills, что усложняет distro tailoring.

Цель архитектуры — развести две принципиально разные сущности:

- **skill** — декларативный per-call tool provider;
- **plugin** — программный long-lived компонент с собственным lifecycle;

и привести kernel к минимальному инвариантному ядру (message bus + hook
engine + supervisor двух уровней), отдав весь catalog LLM-tools на откуп
distro composition'у.

## 2. Two entities

|                       | `SKILL.md`                          | `plugin.toml`                                |
|-----------------------|-------------------------------------|----------------------------------------------|
| Format                | Markdown + YAML frontmatter         | TOML; опциональный `README.md` рядом         |
| Lifecycle             | per-call subprocess                 | long-lived процесс, `register(api)` at start |
| State                 | stateless                           | может держать state, child processes         |
| Tools registration    | объявлены в frontmatter             | `api.registerTool(spec, handler)`            |
| Bus subscription      | нет                                 | `api.on(event, handler, opts)`               |
| Spawn children        | нет (kernel сам вызывает skill)     | да, под собственной process group            |
| Identification        | имя файла `SKILL.md`                | имя файла `plugin.toml`                      |
| `kind` / `capabilities` field | **отсутствует**             | **отсутствует**                              |

Тип сущности определяется именем манифеста. Никаких `kind` enum'ов и никакого
`capabilities`-dispatch в kernel. Если distro/UI хочет тегировать плагины
(например, отфильтровать «все gateways»), используется свободное поле
`tags: [...]` в `plugin.toml` — kernel его игнорирует.

## 3. Manifest specs

### 3.1 `SKILL.md`

Формат остаётся **полностью совместимым с тем, что есть сейчас** — это
Anthropic-style skill manifest c YAML frontmatter, который Tabula разделяет
с другими агентскими экосистемами. Ломать совместимость нельзя.

Базовые поля frontmatter, которые остаются как есть:

- `name` — обязательное.
- `description` — обязательное.
- `tools` — массив объектов `{name, description, params, required, exec}`,
  Tabula-специфичная фишка, объявляет LLM-видимые tools, dispatch'имые
  через kernel `SkillExec`.
  - `exec` в каждом tool entry — команда, которой kernel запускает tool;
    это и определяет runtime скилла (python/node/shell/binary). Имя файла
    скрипта произвольное (не обязан называться `run.py`).
- `user-invocable: true` — опциональное поле; если выставлено, skill
  экспонируется как `/name` slash command в gateway. Остаётся как есть.

Что **убирается** в рамках миграции:

- `requires-kernel-tools` — больше не имеет смысла, у kernel пустой LLM
  catalog (см. §4.1).
- `_` префикс / `skills = []` allowlist hack — libs переезжают из skills
  в обычные python/ts packages (см. §8.3).

Пример (как сегодня в `coder-git/git/SKILL.md`, добавлен `exec`):

```yaml
---
name: git
description: "Structured git operations for coding agents..."
tools:
  - name: git_status
    description: "Porcelain status of the working tree..."
    params: { cwd: { type: string, description: "..." } }
    required: []
    exec: "<venv_python> skills/git/run.py tool git_status"
  - name: git_diff
    ...
    exec: "<venv_python> skills/git/run.py tool git_diff"
---

# Git skill

(human-readable docs)
```

Slash-skill пример:

```yaml
---
name: caveman
description: "Switch response style"
user-invocable: true
---
```

Никаких `id`, `version`, `runtime`, `entry`, `kind` полей не вводим — это
ломало бы совместимость и не нужно: skill = всегда per-call subprocess,
runtime определяется командой в `tools[].exec`, а entry-script может
называться как угодно.

### 3.2 `plugin.toml`

```toml
id = "mcp"
name = "MCP bridge"
version = "0.3.0"
runtime = "python"          # python | node
entry = "run.py"            # путь относительно plugin dir
tags = ["mcp_bridge"]       # опционально, свободные строки

[config.schema]
# JSON Schema для config (как у openclaw)

[config.defaults]
# default values

[ui_hints]
# опционально, для distro/TUI
```

Никаких других обязательных полей. README.md рядом — опционально, не парсится.

## 4. Kernel changes

### 4.1 LLM tool catalog — пустой

Удаляются из `internal/kernel/protocol.go::KernelTool`:

- `shell_exec`
- `process_spawn`
- `process_kill`
- `process_list`

Соответствующие handler'ы (`handleExec`, `handleSpawn`, `handleKill`,
`handleList`) убираются из `tool_service.go`. ProcessManager.RunCommand /
Spawn / Kill / List остаются как internal Go API (нужны for skill exec
internals и plugin supervision).

### 4.2 `RunSkillTool` → internal `SkillExec`

`ProcessManager.RunSkillTool` переименовывается в `SkillExec.Run(skillID,
payload) → result`, не публикуется как LLM-tool, доступен только через
`handleDynamicTool` dispatch для skill-зарегистрированных tools.

### 4.3 Two-tier supervision

```
kernel
  ├── skill subprocess (per-call, kernel-managed lifecycle)
  ├── plugin process (long-lived, kernel-managed lifecycle)
  │     └── plugin's own children (under plugin's process group)
  └── plugin process
        └── child
```

- Kernel spawns skills и plugins, владеет их PID/PG, делает `killpg` при
  shutdown / supervisor restart.
- Plugin владеет своими детьми сам через PG leader pattern: ставит себя
  PG leader'ом при старте, spawn'ит детей в свою PG, ловит SIGTERM и
  делает `killpg(0, SIGTERM)`.
- Kernel не пытается видеть «внуков». Никаких kernel-level invariants на
  global child count / depth.

### 4.4 New `PluginRuntime` interface

Новый internal Go interface для long-lived plugin'ов:

```go
type PluginRuntime interface {
    Start(ctx context.Context, plugin PluginManifest) (PluginHandle, error)
}

type PluginHandle interface {
    ID() string
    Send(msg Envelope) error      // bus event → plugin
    CallTool(req ToolCall) (ToolResult, error) // tool dispatch → plugin
    Shutdown(ctx context.Context) error
}
```

Реализация: spawn subprocess, держит stdio JSON-RPC канал, маршрутизирует
сообщения по subscriptions из `register`-фазы.

### 4.5 MaxChildren / depth / spawn token

Эти инварианты переезжают **внутрь** subagent plugin'а. Kernel о них не
знает. Subagent plugin сам считает depth по приходящему `parent_token` и
сам отказывает в spawn'е при превышении.

## 5. Plugin API

Стартовый набор. Расширяется по мере необходимости (state store, scheduler
helpers, child supervision helpers — добавляются по факту запроса).

### 5.1 Python signature

```python
def register(api):
    api.config            # dict, merged plugin defaults + user override
    api.log               # structured logger (info/warn/error/debug)

    @api.on("before_tool_call", priority=80)
    def handler(event):
        ...
        return {"params": {...}}   # optional rewrite
        # или return {"deny": "reason"}

    api.registerTool({
        "name": "cron_schedule",
        "description": "...",
        "schema": {...},
    }, handler=lambda call: {...})

    api.send({"channel": "bus", "type": "...", "payload": {...}})
    api.spawn(["my-helper", "--flag"])   # child под PG плагина
```

### 5.2 TypeScript signature

```typescript
export default function register(api: PluginAPI) {
  const cfg = api.config;
  api.on("before_tool_call", (event) => { ... }, { priority: 80 });
  api.registerTool({ name, description, schema }, async (call) => { ... });
  api.send({ channel: "bus", type, payload });
  api.spawn(["my-helper", "--flag"]);
}
```

### 5.3 События

Минимум на старте: `before_tool_call`, `after_tool_call`, `before_send`,
`after_receive`. Остальные добавляются по необходимости.

## 6. Plugin ↔ kernel protocol

Stdio JSON-RPC, language-agnostic. Sequence:

```
plugin → kernel : { "method": "register", "params": {
                      "id": "mcp",
                      "tools": [ { name, description, schema } ],
                      "subscriptions": [ { event, priority } ]
                  } }

kernel → plugin : { "method": "tool_call", "params": { name, args, callId } }
plugin → kernel : { "method": "tool_result", "params": { callId, result } }

kernel → plugin : { "method": "event",     "params": { event, data, callId } }
plugin → kernel : { "method": "event_reply",
                    "params": { callId, action: "ok"|"rewrite"|"deny", ... } }

plugin → kernel : { "method": "send",      "params": { channel, type, payload } }
plugin → kernel : { "method": "log",       "params": { level, msg, fields } }

kernel → plugin : { "method": "shutdown" }
```

`config` доставляется в первом `register`-reply от kernel или в env var
при spawn'е (детали — на этапе implementation).

## 7. Bundles

- Bundle = декларативный TOML список путей к skills и plugins.
- Skills и plugins лежат на одном уровне в репо: `tabula-bundles/<area>/<name>/`,
  внутри `SKILL.md` или `plugin.toml`.
- Bundle.toml перечисляет включённые компоненты:

  ```toml
  [bundle]
  id = "base"
  version = "0.3.0"

  [[components]]
  path = "shell"           # → tabula-bundles/base/shell/SKILL.md

  [[components]]
  path = "mcp"             # → tabula-bundles/base/mcp/plugin.toml

  [[components]]
  path = "hook-permissions"

  [[components]]
  path = "cron"
  ```

- `base` bundle включает и skills (`shell`), и plugins (`mcp`, `cron`,
  `hook-permissions`).

## 8. Migration plan

### 8.1 Components → plugins (~10)

| Path                                              | Reason                              |
|---------------------------------------------------|-------------------------------------|
| `base/mcp`                                        | tools + daemon + hot pool           |
| `base/hook-permissions`                           | bus subscription                    |
| `coder-workspace/hook-approvals`                  | bus subscription, state             |
| `coder-workspace/hook-workspace-boundary`         | bus subscription                    |
| `coder-workspace/caveman` (или `caveman/`)        | bus subscription                    |
| `drivers/driver`                                  | long-lived LLM driver               |
| `drivers/subagent`                                | tool registrar + supervisor         |
| `coder/skills/gateway-tui`                        | long-lived TTY frontend             |
| `familiar/skills/gateway-cli`                     | long-lived transport                |
| `familiar/skills/gateway-api`                     | long-lived transport                |
| `familiar/skills/gateway-telegram`                | long-lived transport                |

### 8.2 Components остаются skills

| Path                                  | Reason                       |
|---------------------------------------|------------------------------|
| `base/shell` (новый, после выноса)    | per-call shell exec          |
| `coder-git`, `coder-tasks`, `coder-review` | per-call tool sets       |
| `files`                                | per-call FS ops              |
| `memory`                               | per-call read/write          |
| `pty-tools` (новый)                   | per-call pty interactions    |

### 8.3 Libs больше не skills

`_pylib`, `_tslib`, `_drivers`, `_subagent_types` уезжают из `skills/`
конвенции:

- общий Python код становится обычным python package в `tabula-bundles/_lib/python/`
  или внутри плагина, который его потребляет;
- общий TS код — `tabula-bundles/_lib/ts/` или внутри плагина;
- `_drivers` мигрирует внутрь `drivers/driver` plugin'а как его собственный
  internal module;
- `_subagent_types` — внутрь `drivers/subagent` plugin'а.

`_` префикс упраздняется. `skills = []` allowlist hack удаляется.

### 8.4 Hook-skills — инвазивная миграция

Per-call hook-skill контракт (запуск `run.py` per event) полностью
удаляется. `base/hook-permissions`, `coder-workspace/hook-approvals`,
`coder-workspace/hook-workspace-boundary`, `caveman` переписываются на
long-lived `register(api) + api.on(...)`. Никаких compat shim'ов.

### 8.5 Order of work

1. **Kernel cleanup**: убрать `shell_exec` / `process_*` из enum;
   переименовать `RunSkillTool` → `SkillExec`.
2. **`base/shell` skill**: вынести `shell_exec` как обычный SKILL.md.
3. **`PluginRuntime` interface**: реализовать stdio JSON-RPC, начать с
   Python; написать reference `examples/plugin-hello`.
4. **Migrate hooks** в порядке: `hook-permissions` → `hook-approvals` →
   `hook-workspace-boundary` → `caveman`.
5. **Migrate `mcp`** (наиболее сложный, daemon + tools + first-class
   `mcp__*` registration).
6. **Migrate drivers**: `drivers/driver`, затем `drivers/subagent`
   (subagent забирает MaxChildren/depth/spawn-token).
7. **Migrate gateways**: TUI, CLI, API, Telegram.
8. **TypeScript runtime support** для plugin'ов (gateway-tui переходит на
   plugin protocol).
9. **Bundle format update**: новый `bundle.toml` со списком components.
10. **Distro install update**: `tabula-distro` понимает оба манифеста, libs
    больше не существуют как skills.
11. **Docs**: обновить `SKILL_AUTHORING.md`, добавить `PLUGIN_AUTHORING.md`,
    обновить `ARCHITECTURE.md`, `DISTROS.md`.

### 8.6 Distro deletion

`tabula-distrib/ouroboros` удаляется на старте миграции (снижает
поверхность). При необходимости восстановим позже на новой архитектуре.

## 9. Out of scope

- **Sandbox / pluggable launcher** — отдельный документ (`SANDBOX.md`).
- **opencode integration** — отдельный документ (`OPENCODE_INTEGRATION.md`).
- **harness distro** (immune-harness аналог) — отдельный документ
  (`HARNESS.md`), завязанный на готовую skill/plugin модель.
- **capabilities-based dispatch** в kernel — намеренно НЕ делается.

## 10. Open questions

Решаются по ходу implementation, не блокируют design:

- финальный набор bus events (расширение по запросу плагинов);
- нужен ли plugin'ам shared state store API (k/v) или каждый держит сам;
- scheduler helpers vs plugin сам крутит loop;
- child supervision helpers (например, готовый `api.spawn_supervised(cmd,
  restart=True)`).

Решения добавляются в этот документ инкрементально по мере реализации.
