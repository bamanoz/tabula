# Architecture

This document describes how Tabula is put together today.

If `README.md` explains *what Tabula is*, this file explains *how it actually
works*.

## Top-level model

Tabula has four main layers:

1. **Kernel** — small Go runtime that owns sessions, routing, hooks, and
   process management.
2. **Boot** — command from `TABULA_BOOT` that inspects the active runtime and
   prints one JSON config object. In the built-in distros this is currently
   implemented in Python.
3. **Skills** — external processes that connect to the kernel over WebSocket.
4. **Distro** — a packaged runtime surface: boot script, templates, and a set
   of skills.

Message flow usually looks like this:

```text
user -> gateway -> kernel -> driver -> tools / hooks / subagents
```

## Kernel

The kernel lives in `cmd/tabula/` and `internal/kernel/`.

Responsibilities:

- run the WebSocket server
- run the boot command from `TABULA_BOOT`
- parse the boot JSON config
- spawn configured skill processes
- route messages between session members
- expose built-in kernel tools
- enforce hook ordering and spawn policy

The kernel intentionally does **not** know about Anthropic, OpenAI, Telegram,
memory, MCP, or any other product feature. Those are all userland skills.

### Built-in kernel tools

Current built-ins are defined in `internal/kernel/protocol.go` and mirrored in
`skills/lib/protocol.py`:

- `shell_exec`
- `process_spawn`
- `process_kill`
- `process_list`

These are the only tools that are not provided by skills.

### Sessions

The session is the main routing boundary.

- A gateway joins a session and sends user messages.
- A driver joins the same session and turns those into model calls.
- Tool results and stream events flow through that same session.
- Subagents run in their own session and send final results back to their
  parent session.

The kernel routes by session membership, not by direct skill-to-skill links.

## Wire protocol

Skills talk to the kernel over WebSocket using JSON messages.

Protocol version:

- Go side: `internal/kernel/protocol.go`
- Python side: `skills/lib/protocol.py`
- current version: `1`

Important message types:

- client -> kernel: `connect`, `join`, `message`, `tool_use`, `hook_result`,
  `cancel`
- kernel -> client: `connected`, `joined`, `member_joined`, `init`,
  `tool_result`, `hook`, `error`
- stream path: `stream_start`, `stream_delta`, `stream_end`, `done`

Every skill uses the same basic lifecycle:

1. open WebSocket to `TABULA_URL`
2. send `connect`
3. receive `connected`
4. send `join`
5. receive `joined`
6. optionally receive `init`
7. enter message loop

The shared Python wrapper for this is `skills/lib/kernel_client.py`.

## Boot

Tabula does not hardcode its runtime in Go. Instead, the kernel runs a boot
command and expects a JSON object on stdout.

The boot command comes from `TABULA_BOOT`.

In a normal install, `tabula-server` sets this to something like:

```text
"$TABULA_HOME/.venv/bin/python3" "$TABULA_HOME/boot.py"
```

The active `boot.py` is a symlink to `distrib/active/boot.py`.

### Boot output

The boot script emits one JSON object with fields like:

- `url` — kernel WebSocket URL
- `spawn` — process commands to launch at startup
- `tools` — skill tools to expose to the active driver
- `commands` — slash commands for gateways
- `context` — assembled system prompt
- `kernel_tools` — subset of built-in kernel tools to expose

Assistant and guardian use the same contract, but generate different payloads.

### Assistant boot

`distrib/assistant/boot.py` is the more dynamic boot implementation.

It does the following:

- loads `.env`
- scans the flat `skills/` runtime surface recursively
- reads `SKILL.md` frontmatter
- discovers tool skills and slash commands
- builds the system prompt from templates and project files
- filters provider-specific skills based on `TABULA_PROVIDER`
- selects exactly one driver and one matching subagent runtime
- writes subagent prompt state under `~/.tabula/state/subagent/`

This is where most of the assistant distro behavior is assembled.

### Guardian boot

`distrib/guardian/boot.py` is intentionally much simpler.

It does not scan a flexible skill tree. It builds a fixed runtime:

- empty spawn list
- one tool: `execute_code`
- no slash commands
- a system prompt composed from four fixed templates
- no kernel tools exposed

Guardian is a good example of a distro with the same kernel contract but a
completely different runtime philosophy.

## Distros

At the kernel level, a distro is just "whatever boot command and runtime layout
you choose to ship". In the built-in installer flow, a distro is expected to
be a directory with three required parts:

- `boot.py`
- `templates/`
- `skills/`

Tabula currently ships two:

- `distrib/assistant/`
- `distrib/guardian/`

`scripts/install-distro.py` installs a distro into `~/.tabula/distrib/<name>`
and then updates the active runtime surface.

### Active distro fan-out

The selected distro is activated through symlinks:

```text
~/.tabula/distrib/active -> assistant
~/.tabula/boot.py -> distrib/active/boot.py
~/.tabula/templates/* -> distrib/active/templates/*
~/.tabula/skills/* -> distrib/active/skills/*
```

The exception is `skills/lib/`, which is shared runtime code copied in
separately and preserved when distros are switched.

This flat runtime surface is important: the active agent sees one `skills/`
tree, not a multi-distro layout.

## Skills

At the platform level, a skill is any external capability unit the active boot
logic can discover, describe to the kernel, and optionally execute.

In the built-in `assistant` distro, the current convention is usually a
directory that contains at least:

- `SKILL.md`
- some executable entry point

That current assistant convention is described in
`distrib/assistant/skills/skill-contract/SKILL.md`.

### `SKILL.md`

In the `assistant` distro, `SKILL.md` serves two jobs at once:

1. frontmatter for machine-readable metadata
2. human-readable documentation for users and agents

Typical assistant frontmatter fields include:

- `name`
- `description`
- `user-invocable: true`
- `tools: [...]`

The assistant boot process uses this to:

- decide what enters the system prompt
- discover tool definitions
- discover slash commands
- decide how some skills should be filtered or grouped

### Skill categories

Tabula does not enforce categories in the kernel, but in practice skills fall
into recognizable roles.

#### Drivers

LLM backends such as:

- `driver-anthropic`
- `driver-openai`

These receive `message`, `tool_result`, `init`, `cancel` and send stream
events, `tool_use`, and `done`.

Shared runtime: `skills/lib/driver_runtime.py`

#### Gateways

User interfaces such as:

- `gateway-cli`
- `gateway-api`
- `gateway-telegram`

They send `message` and receive streaming events and completion markers.

#### Tool skills

In the built-in assistant convention, skills that expose tools through
`SKILL.md` frontmatter are usually invoked as separate subprocesses per call.
If no explicit `exec` is provided for a tool, assistant boot currently falls
back to the `run.py tool <name>` convention.

Example shape:

```text
weather/
├── SKILL.md
└── run.py
```

Current assistant default tool call execution shape:

```text
python3 skills/weather/run.py tool get_weather
```

JSON input is piped on stdin; result is read from stdout.

#### Hook skills

Skills that subscribe to kernel lifecycle events by declaring `hooks` in their
`connect` message.

Examples:

- `hook-logger`
- `hook-permissions`

Current hook events include:

- `before_message`, `after_message`
- `before_tool_call`, `after_tool_call`
- `session_start`, `session_end`
- `before_spawn`, `after_spawn`
- `cancel`

#### Subagents

Provider-specific subagent runtimes:

- `subagent-anthropic`
- `subagent-openai`

These are not special-cased in the kernel beyond process spawning and spawn
policy. They are regular external processes that happen to run a different
driver loop.

Shared runtime: `skills/lib/subagent_runtime.py`

## Shared runtime library

`skills/lib/` is shared Python code used by many skills.

Important modules:

- `protocol.py` — protocol constants and kernel tool names
- `kernel_client.py` — WebSocket wrapper
- `driver_runtime.py` — common driver loop
- `subagent_runtime.py` — common subagent loop
- `providers.py` — Anthropic / OpenAI provider adapters
- `compaction.py` — context compaction
- `prompt_builder.py` — prompt assembly helpers
- `paths.py` — runtime path helpers
- `provider_selection.py` — provider resolution

This library is shared runtime infrastructure, but its public API is not yet
fully stabilized as a versioned external contract. That is one of the next
documentation and design tasks.

## Subagents

Subagents are one of Tabula's defining architectural choices.

They are:

- real child processes
- connected to the kernel like any other skill
- attached to their own session
- configured with `parent_session`, `agent_id`, `initial_task`, and limits

The parent driver uses `process_spawn` and later collects subagent results back
into the main turn.

Today the operational layer around subagents is still minimal compared to the
strength of the underlying process model. The current design already supports
parallel work and isolation; registry / control / recovery are the next layer.

## Prompts and personality

Prompt assembly is done in boot, not in the kernel.

Assistant pulls from:

- distro templates under `templates/`
- user-editable project files like `IDENTITY.md`, `SOUL.md`, `USER.md`,
  `AGENTS.md`
- discovered skill descriptions
- memory injections and provider-specific compatibility filtering

This is why personality in Tabula is best thought of as files, not config
flags.

Guardian uses a much smaller prompt surface with four fixed templates.

## Installation model

There are two main installation paths.

### Release install

`scripts/install.sh` / `scripts/install.ps1`:

- download the Go binary from GitHub Releases
- download the runtime payload tarball
- install `skills/lib`, distros, bundles, launchers, examples, service files
- create `~/.tabula/.venv`
- install Python runtime dependencies
- activate the default distro (`assistant` unless overridden)

### Source install

`scripts/install-dev.sh` / `scripts/install-dev.ps1`:

- build the Go binary from source
- install shared `skills/lib`
- install testing skills under `~/.tabula/testing`
- copy service files
- create a venv with dev dependencies
- activate the selected distro

Both paths rely on `scripts/install-distro.py` to materialize the active
runtime surface.

## Runtime surfaces

There are three related but different layouts to keep in mind.

### Repository layout

The source tree:

- keeps distros under `distrib/`
- keeps shared runtime code under `skills/lib/`
- keeps optional reusable skill collections under `bundles/`

### Installed active layout

The running agent sees a flat tree under `~/.tabula/`:

- one active `boot.py`
- one active `templates/`
- one active `skills/`
- shared `skills/lib/`

### Tool execution layout

Tool skills are invoked as subprocesses using their entrypoint directly. The
kernel does not import Python code from them.

This keeps the execution boundary explicit and language-neutral.

## Bundles

Bundles are optional reusable collections of skills under `bundles/<name>/`.

The assistant distro already uses this mechanism for memory:

- `memory-save`
- `memory-search`
- `memory-admin`

At install time or distro install time, bundle skills are linked into the flat
`skills/` surface so boot sees them as ordinary skills.

Bundles are the right place for optional capability packs. Distros are the
right place for fully opinionated products.

## Current boundaries

If you are extending Tabula, the important seams are:

- **kernel <-> skill** — WebSocket protocol
- **boot <-> kernel** — one JSON config object on stdout
- **distro <-> install** — current built-in installer expects `boot.py`,
  `templates/`, `skills/`
- **tool call <-> skill** — assistant currently defaults to `run.py tool <name>`
  via stdin/stdout when `exec` is not explicitly provided; the platform itself
  only needs an executable command
- **shared runtime <-> skills** — `skills/lib/`

Those are the places where contracts matter.

## Where to look next

- `README.md` — project positioning and quick start
- `docs/PHILOSOPHY.md` — why Tabula is shaped like this
- `distrib/assistant/boot.py` — dynamic boot logic
- `distrib/guardian/boot.py` — minimal fixed boot
- `distrib/assistant/skills/skill-contract/SKILL.md` — current skill contract
- `skills/lib/protocol.py` — Python-side protocol constants
- `internal/kernel/protocol.go` — Go-side protocol constants
