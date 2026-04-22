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

The active `boot.py` is copied (or symlinked) from the installed distro under
`~/.tabula/distrib/<distro>/current/boot.py` by `tabula-distro`.

### Boot output

The boot script emits one JSON object with fields like:

- `url` — kernel WebSocket URL
- `spawn` — process commands to launch at startup
- `tools` — skill tools to expose to the active driver
- `commands` — slash commands for gateways
- `context` — assembled system prompt
- `kernel_tools` — subset of built-in kernel tools to expose

Familiar and guardian use the same contract, but generate different payloads.

### Familiar boot

`familiar/boot.py` (in the [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib)
repo) is the more dynamic boot implementation.

It does the following:

- loads `.env`
- scans the flat `skills/` runtime surface recursively
- reads `SKILL.md` frontmatter
- discovers tool skills and slash commands
- builds the system prompt from templates and project files
- filters provider-specific skills based on `TABULA_PROVIDER`
- selects exactly one driver and one matching subagent runtime
- writes subagent prompt state under `~/.tabula/state/subagent/`

This is where most of the familiar distro behavior is assembled.

### Guardian boot

`guardian/boot.py` (in `tabula-distrib`) is intentionally much simpler.

It does not scan a flexible skill tree. It builds a fixed runtime:

- empty spawn list
- one tool: `execute_code`
- no slash commands
- a system prompt composed from four fixed templates
- no kernel tools exposed

Guardian is a good example of a distro with the same kernel contract but a
completely different runtime philosophy.

## Repository layout

Tabula is split across three repositories:

- [`tabula`](https://github.com/bamanoz/tabula) — the kernel, the thin
  `skills/lib` runtime contract, the `tabula-distro` installer, and
  installation scripts.
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  skill bundles: `base/`, `files/`, `drivers/`, `memory/`, `caveman/`. These
  are referenced from distros via `distro.toml`.
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — the three
  ready-to-use distros: `familiar/`, `guardian/`, `ouroboros/`. Each declares
  its bundle dependencies in `distro.toml`.

A distro never embeds bundle source. It declares dependencies and the
`tabula-distro` installer composes the runtime surface in `~/.tabula/`.

## Distros

At the kernel level, a distro is just "whatever boot command and runtime layout
you choose to ship". The built-in `tabula-distro` installer expects a directory
with:

- `boot.py`
- `templates/`
- `skills/` (distro-specific skills only)
- `distro.toml` (declares which bundles to pull in)

`tabula-distro install <source>` resolves the distro itself from a local path,
`local:` URI, or `git+...@ref#path=...` URI; resolves the bundles declared in
`distro.toml`; and lays the result out under
`~/.tabula/distrib/<name>/<generation>/` with `current` and `active` symlinks.

### Active distro layout

The selected distro is activated through symlinks/copies under
`~/.tabula/distrib/<name>/`:

```text
~/.tabula/distrib/familiar/current   -> <generation>
~/.tabula/distrib/active             -> familiar
~/.tabula/boot.py                    -> distrib/active/current/boot.py
~/.tabula/templates/*                -> distrib/active/current/templates/*
~/.tabula/skills/*                   -> distrib/active/current/skills/* + bundle skills
```

The exception is `skills/lib/`, which is shared runtime code copied in
separately from the kernel repo and preserved when distros are switched.

This flat runtime surface is important: the active agent sees one `skills/`
tree, not a multi-distro layout.

## Skills

At the platform level, a skill is any external capability unit the active boot
logic can discover, describe to the kernel, and optionally execute.

In the built-in `familiar` distro, the current convention is usually a
directory that contains at least:

- `SKILL.md`
- some executable entry point

That convention is described in the `skill-contract` skill (shipped via the
`base` bundle in `tabula-bundles`).

### `SKILL.md`

In the `familiar` distro, `SKILL.md` serves two jobs at once:

1. frontmatter for machine-readable metadata
2. human-readable documentation for users and agents

Typical familiar frontmatter fields include:

- `name`
- `description`
- `user-invocable: true`
- `tools: [...]`

The familiar boot process uses this to:

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

Shared runtime: `skills._drivers.driver_runtime` (in the `drivers` bundle).

#### Gateways

User interfaces such as:

- `gateway-cli`
- `gateway-api`
- `gateway-telegram`

They send `message` and receive streaming events and completion markers.

#### Tool skills

In the built-in familiar convention, skills that expose tools through
`SKILL.md` frontmatter are usually invoked as separate subprocesses per call.
If no explicit `exec` is provided for a tool, familiar boot currently falls
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

Shared runtime: `skills._drivers.subagent_runtime` (in the `drivers` bundle).

## Shared runtime library

`skills/lib/` is the *minimal* shared Python code that every skill uses to
talk to the kernel. It lives in the kernel repo (`tabula`) and is shipped in
every install, regardless of which distro is active.

Important modules:

- `protocol.py` — protocol constants and kernel tool names
- `kernel_client.py` — WebSocket wrapper
- `paths.py` — runtime path helpers
- `config.py` — config and env parsing
- `filelock.py` — cross-process file locking

Driver and subagent runtime (`driver_runtime`, `subagent_runtime`,
`providers`, `provider_selection`, `prompt_builder`, `compaction`) now lives in
the `drivers` bundle under `skills/_drivers/` and is imported as
`skills._drivers.<module>`.

This keeps the kernel-side contract tiny, and lets provider-specific code
evolve inside the `tabula-bundles` repo.

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

Familiar pulls from:

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
- download the runtime payload tarball (kernel-side `skills/lib`, launchers,
  examples, service files, the `tabula-distro` source)
- create `~/.tabula/.venv` and install Python runtime dependencies
- install `tabula-distro` from the bundled tools/ directory
- install the requested distro from `tabula-distrib` (default: `familiar`,
  override with `TABULA_DISTRO`) using a `git+...` source — the installer
  pulls the bundles declared in `distro.toml` from `tabula-bundles`

### Source install

`scripts/install-dev.sh` / `scripts/install-dev.ps1`:

- build the Go binary from source
- install shared `skills/lib` from this repo
- copy service files
- create a venv with dev dependencies
- install `tabula-distro` (editable)
- install the selected distro from a sibling `tabula-distrib/` checkout
  (auto-detected, or pass `--distrib-root /path/to/tabula-distrib`)

Both paths converge on `tabula-distro install <source>` to materialize the
active runtime surface from a distro plus its declared bundles.

## Runtime surfaces

There are three related but different layouts to keep in mind.

### Repository layout

The source tree is split across three repos:

- `tabula/` — kernel, `skills/lib/` (kernel contract only), `tabula-distro`
  installer
- `tabula-bundles/` — reusable skill collections (`base/`, `files/`,
  `drivers/`, `memory/`, `caveman/`)
- `tabula-distrib/` — `familiar/`, `guardian/`, `ouroboros/` distros, each
  with its own `boot.py`, `templates/`, `skills/`, and `distro.toml`

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

Bundles are reusable collections of skills, kept in the
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) repo and pulled
into a distro at install time via `distro.toml`.

Current bundles:

- `base/` — clawhub, cron, hook-logger, hook-permissions, observer, pair,
  sessions, skill-contract, tabula-guide, timer
- `files/` — the `files` skill
- `drivers/` — `driver-anthropic`, `driver-openai`, `subagent-anthropic`,
  `subagent-openai`, plus `_drivers/` shared support code
- `memory/` — memory-save, memory-search, memory-admin
- `caveman/` — minimal experimental skill set

A distro lists bundles in `distro.toml`:

```toml
[bundles.base]
source = "git+https://github.com/bamanoz/tabula-bundles.git@main#path=base"

[bundles.drivers]
source = "git+https://github.com/bamanoz/tabula-bundles.git@main#path=drivers"
```

For dev work you can override these locally with a `distro.override.toml`
pointing at `local:` paths.

At install time, bundle skills are linked into the flat `skills/` surface so
boot sees them as ordinary skills.

## Current boundaries

If you are extending Tabula, the important seams are:

- **kernel <-> skill** — WebSocket protocol
- **boot <-> kernel** — one JSON config object on stdout
- **distro <-> install** — `tabula-distro` expects `boot.py`, `templates/`,
  `skills/`, and `distro.toml`
- **tool call <-> skill** — familiar currently defaults to `run.py tool <name>`
  via stdin/stdout when `exec` is not explicitly provided; the platform itself
  only needs an executable command
- **kernel runtime contract <-> skills** — `skills/lib/` (kernel)
- **driver/subagent runtime <-> drivers bundle** — `skills._drivers.*`

Those are the places where contracts matter.

## Where to look next

- `README.md` — project positioning and quick start
- `docs/PHILOSOPHY.md` — why Tabula is shaped like this
- `docs/DISTROS.md` — how distros are composed
- `docs/distro-config.md` — `distro.toml` reference
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — `familiar/`,
  `guardian/`, `ouroboros/` distros
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  skill bundles
- `skills/lib/protocol.py` — Python-side protocol constants
- `internal/kernel/protocol.go` — Go-side protocol constants
