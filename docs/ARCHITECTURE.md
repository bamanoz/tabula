# Architecture

This document describes how Tabula is put together today.

If `README.md` explains *what Tabula is*, this file explains *how it actually
works*.

## Top-level model

Tabula has five main concepts:

1. **Kernel** — small Go runtime that owns sessions, routing, hooks, and
   process supervision.
2. **Boot** — command from `TABULA_BOOT` that inspects the active runtime and
   prints one JSON config object. In the built-in distros this is currently
   implemented in Python.
3. **Skills** — declarative per-call extensions. Each tool call spawns a fresh
   subprocess; skills are stateless. Manifest: `SKILL.md` (Markdown +
   frontmatter).
4. **Plugins** — long-lived processes with a `register(api)` entry point that
   subscribe to bus events, register tools dynamically, and own their own
   lifecycle. Manifest: `plugin.toml`. See
   [plans/SKILL_PLUGIN_ARCHITECTURE.md](plans/SKILL_PLUGIN_ARCHITECTURE.md).
5. **Distro** — a packaged runtime surface: boot script, templates, and a set
   of bundles (which themselves are mixed collections of skills and plugins).

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
- execute per-call skill subprocesses and long-lived plugin processes
- route messages between session members
- expose the distro-declared skill/plugin tool catalog
- enforce hook ordering and own level-one process supervision

The kernel intentionally does **not** know about Anthropic, OpenAI, Telegram,
memory, MCP, or any other product feature. Those are all userland skills.

### Built-in kernel tools

The kernel publishes **no** LLM-visible tools by default. The native tool
catalog is empty; everything (including `shell_exec`-style commands) is
delivered by skills or plugins from the active distro.

This is intentional: distros decide their tool surface, and the kernel does not
impose command execution or process-management tools as a baseline.

Internally, the kernel still owns process spawning and skill execution
(`SkillExec`, `PluginRuntime`), but those are not LLM tools.

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
- Python side: `tabula_plugin_sdk.protocol`
- current version: `1`
- Clients **must** declare `version` on `connect`. Mismatched or missing
  versions are rejected with an `error` message — there is no legacy fallback.

## Versioning

The kernel binary has its own SemVer. Source of truth: `<repo>/VERSION` (synced
by release tooling into Go ldflags). Installers also write that version to
`$TABULA_HOME/VERSION` so `tabula-distro` can verify compatibility before
composing a distro.

Wire compatibility is tracked separately:

- kernel client protocol: `ProtocolVersion` in `internal/kernel/protocol.go`
  and `tabula_plugin_sdk.protocol` for WebSocket clients;
- plugin protocol: `PluginProtocolVersion` in `internal/kernel/protocol.go`,
  negotiated during the stdio `register_request` / `register` handshake;
- Python/TypeScript SDK packages: independent SemVer artifacts, pinned by the
  bundle/distro release that ships them.

This avoids coupling kernel releases to SDK package patch releases while still
making incompatible protocol changes explicit at connect/register time.

Distros and bundles declare their requirement against this number:

- `distro.toml` → `[requires] kernel = ">=0.8.0,<1.0.0"`
- `bundle.toml` → `[requires] kernel = ">=0.8.0,<1.0.0"`

Mismatches are hard errors at install time. Bundles without a `bundle.toml` are
treated as legacy and skip the check.

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

The shared Python wrapper for this is `tabula_plugin_sdk.kernel_client`.

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
- `skills` — static per-call skill tool descriptors; each entry includes an
  `exec` command used by the kernel for dispatch
- `plugins` — plugin manifest paths for long-lived runtime components
- `meta` — opaque client-facing metadata forwarded on `init.meta`

Claw and guardian use the same contract, but generate different payloads.

### Claw boot

`claw/boot.py` (in the [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib)
repo) is the more dynamic boot implementation.

It does the following:

- loads `.env`
- scans the flat `skills/` and `plugins/` runtime surfaces recursively
- reads `SKILL.md` frontmatter for skills and `plugin.toml` for plugins
- discovers skill tools for `skills[]`
- builds the system prompt from templates and project files
- selects the active provider through the unified `drivers/driver` plugin
- declares long-lived plugins for kernel-managed lifecycle
- writes subagent prompt state under `~/.tabula/state/subagent/`

This is where most of the claw distro behavior is assembled.

### Guardian boot

`guardian/boot.py` (in `tabula-distrib`) is intentionally much simpler.

It does not scan a flexible skill tree. It builds a fixed runtime:

- one tool: `execute_code`
- a system prompt composed from four fixed templates

Guardian is a good example of a distro with the same kernel contract but a
completely different runtime philosophy.

## Repository layout

Tabula is split across three repositories:

- [`tabula`](https://github.com/bamanoz/tabula) — the kernel, the
  `tabula-distro` installer, reference examples, and installation scripts.
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  bundles. Each bundle is a directory containing skills (`SKILL.md`) and/or
  plugins (`plugin.toml`) on the same level. Bundles are referenced from
  distros via `distro.toml`.
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — the
  ready-to-use distros: `coder/`, `claw/`, `guardian/`. Each declares
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
- `plugins/` (optional distro-specific plugins)
- `distro.toml` (declares which bundles to pull in)

`tabula-distro install <source>` resolves the distro itself from a local path,
`local:` URI, or `git+...@ref#path=...` URI; resolves the bundles declared in
`distro.toml`; and lays the result out under
`~/.tabula/distrib/<name>/<generation>/` with `current` and `active` symlinks.

### Active distro layout

The selected distro is activated through symlinks/copies under
`~/.tabula/distrib/<name>/`:

```text
~/.tabula/distrib/claw/current       -> <generation>
~/.tabula/distrib/active             -> claw
~/.tabula/boot.py                    -> distrib/active/current/boot.py
~/.tabula/templates/*                -> distrib/active/current/templates/*
~/.tabula/skills/*                   -> distrib/active/current/skills/* + bundle skills
~/.tabula/plugins/*                  -> distrib/active/current/plugins/* + bundle plugins
```

Shared SDK packages such as `tabula_plugin_sdk` are installed into
`~/.tabula/.venv` by the installer from bundled package artifacts. They are not
materialized as special legacy support directories in the runtime surface.

This flat runtime surface is important: the active agent sees one `skills/`
tree and one `plugins/` tree, not a multi-distro layout.

## Skills and plugins

Tabula has two extension shapes, each identified by its manifest filename.

### Skill (`SKILL.md`)

A **per-call** tool provider. Each tool invocation spawns a fresh subprocess.
Skills are stateless. They do not subscribe to bus events.

Manifest is Markdown with YAML frontmatter (Anthropic-compatible). Required
fields: `name`, `description`, `tools`. Each entry in `tools[]` carries an
`exec` command — that is what kernel runs to handle the call. Optional
`user-invocable: true` exposes the skill as a `/name` slash command.

```yaml
---
name: git
description: "Structured git operations for coding agents..."
tools:
  - name: git_status
    description: "..."
    params: { cwd: { type: string } }
    required: []
    exec: "<venv_python> skills/git/run.py tool git_status"
---

# Git skill

(human-readable docs)
```

Kernel runs the `exec` command per tool call, pipes JSON params on stdin,
reads the result from stdout. Each call is process-isolated.

### Plugin (`plugin.toml`)

A **long-lived** process with a `register(api)` entry point. Plugins:

- subscribe to bus events (`api.on(...)`),
- register tools dynamically (`api.registerTool(...)`),
- spawn and supervise their own children under their own process group,
- hold state for as long as they run.

Manifest is TOML; an optional `README.md` provides human docs (not parsed).

```toml
id = "mcp"
name = "MCP bridge"
version = "0.3.0"
runtime = "python"      # python | node
entry = "run.py"
tags = ["mcp_bridge"]   # optional, free strings, kernel-ignored

[config.schema]
# JSON Schema for plugin config

[config.defaults]
# default values
```

Plugins talk to the kernel over stdio NDJSON JSON-RPC: `register`,
`tool_call`, `tool_result`, `event`, `event_reply`, `update_tools`, `send`,
`log`, and `shutdown`. See [PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md) for the
implemented authoring surface.

### What is a plugin and what is a skill

| Role today                         | Shape                       |
|------------------------------------|-----------------------------|
| Per-call tool sets (`coder-git`, `files`, `memory`, `pty-tools`) | skill |
| Hooks (`hook-permissions`, `hook-approvals`, `caveman`, ...)     | plugin |
| MCP bridge                          | plugin |
| LLM drivers (`drivers/driver`)     | plugin |
| Subagent runtime (`drivers/subagent`) | plugin |
| Gateways (TUI, CLI, API, Telegram) | plugin |

The migration from the old uniform "everything is a skill" model is tracked
in [plans/SKILL_PLUGIN_ARCHITECTURE.md §8](plans/SKILL_PLUGIN_ARCHITECTURE.md).

### Two-tier supervision

```
kernel
  ├── skill subprocess (per call)
  ├── plugin process (long-lived)
  │     └── plugin's own children (under plugin's process group)
  └── plugin process
        └── child
```

Kernel owns level-one processes. Plugins own their children through a
process-group leader pattern (`killpg` on shutdown). Kernel never reaches
into plugin children.

### Hook events

Available bus events plugins can subscribe to:

- `before_message`, `after_message`
- `before_tool_call`, `after_tool_call`
- `session_start`, `session_end`
- `before_spawn`, `after_spawn` (reserved legacy names; no longer emitted by
  kernel LLM-visible process tools)
- `cancel`

A hook can be void (observe only), modifying (rewrite/deny payload), or
claiming (first claimer wins).

## Shared SDK packages

`tabula_plugin_sdk` is the minimal shared Python package that skills and plugins
use to talk to the kernel. It is distributed as a normal package artifact and
installed into the Tabula venv by installer/bundle release tooling, rather than
copied into the runtime as a magic skill directory.

Important modules:

- `protocol` — protocol constants
- `kernel_client` — WebSocket wrapper for per-call skills and clients
- `api` — stdio NDJSON helper for long-lived plugins
- `paths` — runtime path helpers
- `config` — config and env parsing
- `filelock` — cross-process file locking

Driver and subagent runtime (`driver_runtime`, `subagent_runtime`,
`providers`, `provider_selection`, `prompt_builder`, `compaction`) lives in the
`drivers` bundle as bundle-internal support code.

This keeps the kernel-side contract tiny, and lets provider-specific code
evolve inside the `tabula-bundles` repo.

## Subagents

Subagents are one of Tabula's defining architectural choices.

They are:

- real child processes
- connected to the kernel like any other plugin client
- attached to their own session
- configured with `parent_session`, `agent_id`, `initial_task`, and limits

The parent driver requests a spawn through the subagent plugin, which owns the
child process group. Results are returned through `subagent_wait` or
`subagent_spawn` with `mode="sync"` as structured tool results.

Target ownership for the skill/plugin architecture is that `MaxSpawnDepth`,
`MaxChildren`, and child authentication live inside the subagent plugin itself —
the kernel should not enforce them as a global invariant. During migration,
removing the remaining kernel bridge is gated on external driver/subagent plugin
evidence for equivalent depth, child-count, auth, lifecycle, and cleanup tests.

The next operational layer (run registry, control surface, orphan recovery,
tool allowlist on spawn) is described in
[plans/COMPETITIVE_LESSONS.md §5](plans/COMPETITIVE_LESSONS.md).

## Prompts and personality

Prompt assembly is done in boot, not in the kernel.

Claw pulls from:

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
- download the runtime payload tarball (launchers, examples, service files,
  bundled SDK package artifacts, and the `tabula-distro` source)
- create `~/.tabula/.venv` and install Python runtime dependencies
- install `tabula-distro` from the bundled tools/ directory and expose it on
  `~/.tabula/bin`

After the kernel installer finishes, install a distro yourself:

```bash
tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
```

### Source install

`scripts/install-dev.sh` / `scripts/install-dev.ps1`:

- build the Go binary from source
- install shared SDK packages into `~/.tabula/.venv`
- copy service files
- create a venv with dev dependencies
- install `tabula-distro` (editable) and expose it on `~/.tabula/bin`

Same follow-up: install a distro with `tabula-distro install <path-or-uri>`.

Both paths intentionally stop at the kernel layer. Distro composition is
always done by `tabula-distro install`, which resolves a distro plus its
declared bundles into the active runtime surface.

## Runtime surfaces

There are three related but different layouts to keep in mind.

### Repository layout

The source tree is split across three repos:

- `tabula/` — kernel, examples, installation scripts, and the `tabula-distro`
  installer.
- `tabula-bundles/` — reusable collections of skills and plugins (`base/`,
  `files/`, `drivers/`, `memory/`, `caveman/`, `coder-*/`).
- `tabula-distrib/` — `coder/`, `claw/`, `guardian/` distros, each with
  its own `boot.py`, `templates/`, optional in-tree `skills/`/`plugins/`,
  and `distro.toml`.

### Installed active layout

The running agent sees a flat tree under `~/.tabula/`:

- one active `boot.py`
- one active `templates/`
- one active `skills/`
- one active `plugins/`
- shared SDK packages installed in `~/.tabula/.venv`

### Tool execution layout

Skills are invoked as subprocesses using their `tools[].exec` command. The
kernel does not import code from skills.

Plugins run as long-lived subprocesses and communicate via stdio NDJSON
JSON-RPC.
Tool calls dispatched to a plugin are sent on the same channel, not by
re-spawning a new process.

This keeps every execution boundary explicit and language-neutral.

## Bundles

Bundles are reusable collections of skills and plugins, kept in the
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) repo and pulled
into a distro at install time via `distro.toml`.

A bundle is a directory whose top level contains skill directories
(with `SKILL.md`) and plugin directories (with `plugin.toml`) on the same
level. The kernel doesn't distinguish between them at the bundle level — the
manifest filename does.

Current bundles:

- `base/` — clawhub, cron, hook-logger, hook-permissions, observer, pair,
  sessions, skill-contract, tabula-guide, timer, mcp
- `files/` — the `files` skill
- `drivers/` — `driver`, `subagent`, plus `_drivers/` shared support code
- `memory/` — memory-save, memory-search, memory-admin
- `caveman/` — minimal experimental skill set
- `coder-git/`, `coder-tasks/`, `coder-review/`, `subagents/`,
  `coder-workspace/` — components used by the `coder` distro

A distro lists bundles in `distro.toml`:

```toml
[sources.tabula-bundles]
source = "git+https://github.com/bamanoz/tabula-bundles.git@main"

[[bundles]]
name = "base"
source = "source:tabula-bundles#path=base"

[[bundles]]
name = "drivers"
source = "source:tabula-bundles#path=drivers"
```

For dev work you can override these locally with a `distro.override.toml`
that changes `[sources.tabula-bundles]` to a single `local:` checkout.

At install time, bundle components are linked into the flat runtime surface:
skill components under `skills/`, plugin components under `plugins/`, and
client components under `clients/`.

## Current boundaries

If you are extending Tabula, the important seams are:

- **kernel <-> skill** — `skills[].exec` command, JSON over stdin/stdout per
  call.
- **kernel <-> plugin** — long-lived stdio NDJSON JSON-RPC (`register`,
  `tool_call`, `tool_result`, `event`, `event_reply`, `update_tools`, `send`,
  `log`, `shutdown`).
- **boot <-> kernel** — one JSON config object on stdout.
- **distro <-> install** — `tabula-distro` expects `boot.py`, `templates/`,
  optional in-tree components, and `distro.toml`.
- **kernel runtime contract <-> components** — SDK packages such as
  `tabula_plugin_sdk`, installed into the Tabula venv.
- **driver/subagent runtime <-> drivers bundle** — bundle-internal support code.

Those are the places where contracts matter.

## Where to look next

- `README.md` — project positioning and quick start
- `docs/PHILOSOPHY.md` — why Tabula is shaped like this
- `docs/DISTROS.md` — how distros are composed
- `docs/SKILL_AUTHORING.md` — how to author skills and plugins
- `docs/distro-config.md` — `distro.toml` reference
- `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md` — skill/plugin design doc
- `docs/plans/COMPETITIVE_LESSONS.md` — actionable lessons from peer projects
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — `coder/`,
  `claw/`, `guardian/` distros
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  bundles
- `internal/kernel/protocol.go` — Go-side protocol constants
- `internal/kernel/plugin/protocol.go` — plugin stdio protocol messages
