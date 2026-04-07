# Tabula: Competitive Analysis & Feature Roadmap

## Context

Tabula — микроядерный AI-агент с архитектурой Go-ядро + WebSocket pub/sub + Python-скиллы. Нужно понять, где мы стоим относительно конкурентов (Claude Code, OpenClaw, Codex CLI, OpenCode, Cursor) и составить приоритизированный список фичей.

---

## Что Tabula умеет сейчас

| Категория | Возможности |
|-----------|-------------|
| **Ядро** | Go + gorilla/websocket, hub pattern, сессии, pub/sub, async tool execution |
| **Тулы** | EXEC (sync cmd, 16KB limit), SPAWN, KILL, LIST |
| **LLM** | Anthropic Claude (streaming, tool use, batching tool results) |
| **Субагенты** | Параллельный запуск через SPAWN, собственная сессия, debounce результатов |
| **CLI** | Rich TUI: markdown рендеринг, spinner, стриминг, Ctrl+C cancel |
| **Память** | Персистентная (MEMORY.md), категории, keyword search, опционально embeddings |
| **Протокол** | JSON over WebSocket, connect/join/message/stream_*/tool_use/tool_result/cancel/error |
| **Тесты** | 28 тестов с -race: handshake, routing, tools, concurrency, leaks |

---

## Сравнительная матрица

| Фича | Tabula | Claude Code | OpenClaw | Codex CLI | OpenCode | Cursor |
|------|--------|-------------|----------|-----------|----------|--------|
| **Чтение файлов** | EXEC cat | Dedicated Read tool | Boundary file read | read_file | read | IDE native |
| **Редактирование файлов** | EXEC sed/echo | Edit (string replace) + Write | apply_patch (unified diff) | apply_patch | edit + multiedit + apply_patch | IDE + Cmd+K |
| **Поиск по коду** | EXEC grep/find | Glob + Grep (ripgrep) | Environment tools | list_dir | glob + grep + codesearch | Semantic indexing |
| **LSP** | - | goToDefinition, references, hover, symbols, callHierarchy | Bundle LSP servers | - | Full LSP: definition, references, hover, symbols, callHierarchy | VS Code native |
| **Git** | EXEC git | Worktree isolation, diff, status, commit helpers | Commit tracking | - | - | Built-in |
| **Мульти-модель** | Только Anthropic | Anthropic + Bedrock + Vertex | 40+ провайдеров | OpenAI + OpenRouter + 10+ | Provider-agnostic | Claude + GPT + Gemini |
| **MCP** | - | Full (servers, resources, tools, OAuth) | Native MCP + channel tools | Full MCP handler | MCP + OAuth | MCP |
| **Sandbox** | - | Sandbox mode toggle | Exec approvals + allowlist | Apple Seatbelt + Docker | - | - |
| **Permissions** | - | 5+ modes (default/acceptEdits/bypass/plan/auto) + rules + hooks | Multi-layer (session/host/user) + allowlists | 3 modes (suggest/autoEdit/fullAuto) | 2 agents (build/plan) | Diff review + YOLO |
| **Субагенты** | SPAWN + debounce | AgentTool + Coordinator mode + workers | ACP spawn + sessions | Multi-agent v2 (spawn/wait/resume) | @general + batch | Background agents (beta) |
| **Стриминг** | stream_start/delta/end | Tool-level + progress streaming | Provider-specific transport | Chain-of-thought summaries | Yes | Yes |
| **Память** | MEMORY.md + daily + embeddings | MEMORY.md + CLAUDE.md hierarchy + auto-memory | QMD + Honcho + dreaming | AGENTS.md | AGENTS.md + Skills | .cursor/rules + Notepad |
| **Веб** | - | WebFetch + WebSearch | web_fetch + web_search | - | webfetch + websearch | @web |
| **Cost tracking** | - | Per-model USD, tokens, duration | Per-session tokens + estimated cost | - | - | Fast/slow request quotas |
| **Hooks** | - | 14+ events (PreToolUse, PostToolUse, FileChanged...) | Internal + message + delivery hooks | - | - | - |
| **Расширения** | Skills (SKILL.md) | Skills + Plugins + Marketplace | Plugins + Skills + channels | MCP | Skills + MCP | VS Code extensions + MCP |
| **Image/мультимодал** | - | Images + PDFs + Notebooks | Images + Audio + Video | - | Images | IDE native |
| **CI/headless** | - | Yes (--print, quiet mode) | - | Yes (quiet/non-interactive) | Yes (client/server) | - |
| **Notebook** | - | Jupyter cell editing | - | - | - | IDE native |
| **Plan mode** | - | EnterPlanMode + task tracking | - | - | Plan agent (read-only) | - |

---

## Фичи к реализации по уровню критичности

### P0 — Must Have (без этого агент не конкурентоспособен)

| # | Фича | Описание | Effort |
|---|------|----------|--------|
| 1 | **File Read tool** | Dedicated tool для чтения файлов: поддержка line ranges, limit, offset. Не через `EXEC cat` — нужна обработка больших файлов, бинарных, images. | S |
| 2 | **File Write tool** | Атомарная запись файла с контентом. Создание новых файлов. | S |
| 3 | **File Edit tool** | String replacement в файлах (как Claude Code Edit) или apply_patch (как Codex/OpenClaw). Позволяет точечное редактирование без переписывания целого файла. | M |
| 4 | **Glob tool** | Поиск файлов по паттернам (*.go, src/**/*.ts). Сейчас через EXEC find — медленно и неудобно. | S |
| 5 | **Grep tool** | Поиск по содержимому файлов (ripgrep). Ключевой инструмент навигации по кодобазе. | S |
| 6 | **Permission model** | Минимум: approve/deny для EXEC и SPAWN. LLM не должен без спроса rm -rf или push --force. Хотя бы whitelist безопасных команд. | M |
| 7 | **Conversation history persistence** | Сейчас история живёт в Python-драйвере и теряется при рестарте. Нужно хранить в ядре или на диске. | M |

### P1 — Should Have (значительно улучшает качество)

| # | Фича | Описание | Effort |
|---|------|----------|--------|
| 8 | **Multi-model support** | Хотя бы OpenAI + Anthropic. В идеале — абстракция provider + config. | M |
| 9 | **Web fetch/search** | Возможность ходить в интернет: fetch URL, search. Критично для research-задач. | M |
| 10 | **Git integration tools** | Dedicated git tools: status, diff, log, commit. Не через EXEC — для правильной интеграции с permissions и context. | M |
| 11 | **Cost/token tracking** | Подсчёт потраченных токенов и стоимости за сессию. Показывать в CLI. | S |
| 12 | **MCP support** | Подключение MCP-серверов для расширения тулов. Стандарт де-факто, поддерживают все конкуренты. | L |
| 13 | **Hooks system** | Pre/post tool execution хуки. Позволяют кастомизировать поведение без изменения кода. | M |
| 14 | **Plan mode** | Read-only режим для исследования кодобазы перед написанием кода. С task tracking. | M |

### P2 — Nice to Have (конкурентные преимущества)

| # | Фича | Описание | Effort |
|---|------|----------|--------|
| 15 | **LSP integration** | goToDefinition, findReferences, hover. Только OpenCode и Claude Code имеют. Мощный differentiator. | L |
| 16 | **Image/PDF support** | Чтение изображений и PDF в контекст. Мультимодальность. | M |
| 17 | **Sandbox mode** | Изоляция EXEC: ограничение доступа к FS, сети. Apple Seatbelt на macOS, Docker на Linux. | L |
| 18 | **CI/headless mode** | Non-interactive запуск: принять prompt, выполнить, вывести результат, выйти. Для автоматизации. | S |
| 19 | **Web gateway** | Веб-интерфейс в дополнение к CLI. | L |
| 20 | **Coordinator mode** | Оркестрация нескольких воркеров на сложных задачах. | L |

### P3 — Future (долгосрочная vision)

| # | Фича | Описание |
|---|------|----------|
| 21 | Plugin marketplace | Установка скиллов из реестра |
| 22 | Semantic codebase indexing | Векторный индекс кодобазы для контекста |
| 23 | Voice mode | Голосовой ввод |
| 24 | IDE extensions | VS Code / JetBrains интеграция через bridge |
| 25 | Notebook support | Jupyter cell editing |
| 26 | Remote execution | Облачные сессии |

---

## Effort Legend

- **S** = Small (1-2 дня, 1 файл/скилл)
- **M** = Medium (3-5 дней, несколько файлов)
- **L** = Large (1-2 недели, архитектурные изменения)

---

## Рекомендация по порядку

Начать с P0 #1-5 (file tools + glob + grep) — они дают максимальный impact при минимальном effort. Это превращает агента из "запускалки команд" в полноценный coding assistant. Каждый тул реализуется как Go-handler в `tools.go` + добавляется в `kernel.tools.json`.

Затем P0 #6 (permissions) — без этого опасно давать агенту в руки реальные проекты.

Далее P1 в порядке: cost tracking (#11, S) → web (#9, M) → multi-model (#8, M) → MCP (#12, L).
