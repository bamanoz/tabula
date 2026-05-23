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
3. **Skills** — prompt/instruction artifacts. They do not publish executable
   tools. Manifest: `SKILL.md` (Markdown + frontmatter).
4. **Plugins** — long-lived processes with a `register(api)` entry point that
   subscribe to bus events, register tools dynamically, and own their own
   lifecycle. Manifest: `plugin.toml`.
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
- accept runtime attachments and route tool calls to runtime-hosted workers
- route messages between session members
- expose the distro-declared skill/plugin tool catalog
- enforce hook ordering and own level-one process supervision

The kernel intentionally does **not** know about Anthropic, OpenAI, Telegram,
memory, MCP, or any other product feature. Those are all userland components.

### Built-in kernel tools

The kernel publishes **no** LLM-visible tools by default. The native tool
catalog is empty; everything (including `shell_exec`-style commands) is
delivered by plugins from the active distro.

This is intentional: distros decide their tool surface, and the kernel does not
impose command execution or process-management tools as a baseline.

Tool execution happens through attached `tabula-runtime` processes. The local
product wrapper `tabula-runner` starts the kernel and runtime as sibling
processes.

### Sessions

The session is the main routing boundary.

- A gateway joins a session and sends user messages.
- A driver joins the same session and turns those into model calls.
- Tool results and stream events flow through that same session.
- Subagents run in their own session and send final results back to their
  parent session.

The kernel routes by session membership, not by direct skill-to-skill links.

## Protocols

Gateways, drivers, and low-level clients talk to the kernel over WebSocket using
JSON messages. Runtime workers do not connect to the kernel directly; they talk
to `tabula-runtime` over the worker protocol.

Protocol version:

- Go side: `internal/kernel/protocol.go`
- Python side: `tabula_plugin_sdk.protocol`
- current version: `3`
- Clients **must** declare `v: 3` on every client->kernel frame. The first
  frame is `hello`. Mismatched or missing versions are rejected with an
  `error` message — there is no legacy fallback.

## Versioning

The kernel binary has its own SemVer. Source of truth: `<repo>/VERSION` (synced
by release tooling into Go ldflags). Installers also write that version to
`$TABULA_HOME/VERSION` so `tabula-distro` can verify compatibility before
composing a distro.

Wire compatibility is tracked separately:

- kernel client protocol: `ProtocolVersion` in `internal/kernel/protocol.go`
  and `tabula_plugin_sdk.protocol` for WebSocket clients;
- runtime worker protocol: `internal/runtime/worker/wire` on the Go side and
  `tabula_plugin_sdk.protocol` on the Python side, exchanged as NDJSON `init`,
  `init_ack`, `call`, `result`, `event`, `event_reply`, `send`,
  `tools_updated`, and `shutdown` frames;
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

- client -> kernel: `hello`, `join`, `event`, `request`, `reply`, `hook_reply`
- common client topics: `message.user`, `tool.call`, `exchange.choose`,
  `exchange.approve`, `turn.cancel`
- common kernel topics: `session.init`, `session.member_joined`, `tool.result`,
  `usage.update`, `turn.done`, `hook`, `error`
- stream path: `stream.start`, `stream.delta`, `stream.end`

Every kernel client uses the same basic lifecycle:

1. open WebSocket to `TABULA_URL`
2. send `hello` with `data.auth_token`
3. receive `hello_ack`
4. send `join`
5. receive `joined`
6. optionally receive `event topic=session.init`
7. enter message loop

`tabula serve` writes the kernel client token to
`$TABULA_HOME/run/kernel-client-token` and exports it as `TABULA_KERNEL_TOKEN`
for first-party drivers and gateways. `hook_reply` messages are tied to the
subscriber identity that received the hook; another client cannot answer a hook
by reusing its id.

The shared Python wrapper for low-level clients is
`tabula_plugin_sdk.kernel_client`. Plugins should use the runtime-owned
`register(api)` worker API instead.

## Boot

Tabula does not hardcode its runtime in Go. Instead, the kernel runs a boot
command and expects a JSON object on stdout.

The boot command comes from `TABULA_BOOT`.

In a normal install, `tabula-runner` sets this to something like:

```text
"$TABULA_HOME/.venv/bin/python3" "$TABULA_HOME/boot.py"
```

The active `boot.py` is copied (or symlinked) from the installed distro under
`$TABULA_HOME/distrib/<distro>/current/boot.py` by `tabula-install`.

`tabula serve` itself is kernel-only: it reads `TABULA_BOOT`, serves the kernel
endpoints, and accepts runtime attachments. The local product wrapper
`tabula-runner` is what launches `tabula serve` plus a sibling `tabula-runtime`
process for the default local stack.

### Boot output

The boot script emits one JSON object with fields like:

- `url` — kernel WebSocket URL
- `skills` — reserved legacy field; current distros leave it empty
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
- builds the system prompt from templates and project files
- selects the active provider through the unified `drivers/driver` plugin
- declares long-lived plugins for runtime-managed worker lifecycle
- writes subagent prompt state under `$TABULA_HOME/state/subagent/`

This is where most of the claw distro behavior is assembled.

### Guardian boot

`guardian/boot.py` (in `tabula-distrib`) is intentionally much simpler.

It builds a fixed runtime around distro-owned prompts plus installed plugins.

Guardian is a good example of a distro with the same kernel contract but a
completely different runtime philosophy.

## Repository layout

Tabula is split across three repositories:

- [`tabula`](https://github.com/bamanoz/tabula) — the kernel, the
  `tabula-install`/`tabula-distro` installer, reference examples, and installation scripts.
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  bundles. Each bundle is a directory containing skills (`SKILL.md`) and/or
  plugins (`plugin.toml`) on the same level. Bundles are referenced from
  distros via `distro.toml`.
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — the
  ready-to-use distros: `code/`, `claw/`, `guardian/`. Each declares
  its bundle dependencies in `distro.toml`.

A distro never embeds bundle source. It declares dependencies and the
`tabula-install` composes the runtime surface in `$TABULA_HOME/`.

## Distros

At the kernel level, a distro is just "whatever boot command and runtime layout
you choose to ship". The built-in `tabula-distro` installer expects a directory
with:

- `boot.py`
- `templates/`
- `skills/` (distro-specific skills only)
- `plugins/` (optional distro-specific plugins)
- `distro.toml` (declares which bundles to pull in)

`tabula-install distro install <source>` resolves the distro itself from a local path,
`local:` URI, or `git+...@ref#path=...` URI; resolves the bundles declared in
`distro.toml`; and lays the result out under
`$TABULA_HOME/distrib/<name>/<generation>/` with `current` and `active` symlinks.

### Active distro layout

The selected distro is activated through symlinks/copies under
`$TABULA_HOME/distrib/<name>/`:

```text
$TABULA_HOME/distrib/claw/current       -> <generation>
$TABULA_HOME/distrib/active             -> claw
$TABULA_HOME/boot.py                    -> distrib/active/current/boot.py
$TABULA_HOME/templates/*                -> distrib/active/current/templates/*
$TABULA_HOME/skills/*                   -> distrib/active/current/skills/* + bundle skills
$TABULA_HOME/plugins/*                  -> distrib/active/current/plugins/* + bundle plugins
$TABULA_HOME/tenants/<id>/templates/*   -> distrib/active/current/templates/*
$TABULA_HOME/tenants/<id>/skills/*      -> distrib/active/current/skills/* + bundle skills
$TABULA_HOME/tenants/<id>/plugins/*     -> distrib/active/current/plugins/* + bundle plugins
$TABULA_HOME/tenants/<id>/_lib/*        -> distrib/active/current/_lib/*
```

Shared SDK packages such as `tabula_plugin_sdk` are installed into
`$TABULA_HOME/.venv` by the installer from bundled package artifacts. They are not
materialized as special legacy support directories in the runtime surface.

The root runtime surface remains flat for the active kernel/runtime, while each
tenant gets its own fan-out of the same active generation surface. Bundle code
is shared through symlinks; only tenant config/state/cache diverge.

## Skills and plugins

Tabula has two extension shapes, each identified by its manifest filename.

### Skill (`SKILL.md`)

A prompt/instruction artifact. Skills describe when to use a workflow, may
provide optional helper scripts/resources, and may expose slash-command docs via
`user-invocable: true`. Skills do not publish executable tools.

Manifest is Markdown with YAML frontmatter (Anthropic-compatible). Common
fields: `name`, `description`, and optional `user-invocable: true`.

```yaml
---
name: git
description: "Guidance for using structured git plugin tools."
user-invocable: true
---
```

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
```

Plugin runtime config is owned by the plugin, not by `plugin.toml`. Python
plugins should load it with `tabula_plugin_sdk.load_plugin_config(plugin_id, ...)`.
The standard precedence is:

1. code defaults passed by the plugin
2. `$TABULA_HOME/config/global.toml` under `[plugins.<plugin-id>]`
3. `$TABULA_HOME/config/plugins/<plugin-id>/config.toml`
4. tenant effective `config/plugins/<plugin-id>/config.toml`, when present
5. environment variables declared by the plugin
6. explicit runtime or CLI arguments

Example plugin-local config:

```toml
# $TABULA_HOME/config/plugins/mcp/config.toml
[servers.filesystem]
transport = "stdio"
command = ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
```

Equivalent global config:

```toml
# $TABULA_HOME/config/global.toml
[plugins.mcp.servers.filesystem]
transport = "stdio"
command = ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
```

Plugins talk to `tabula-runtime` over stdio NDJSON worker frames. The runtime
attaches to the kernel over the Runtime API and forwards `init`, tool calls,
events, `tools_updated`, logs, and shutdown. See [PLUGIN_AUTHORING.md](PLUGIN_AUTHORING.md)
for the implemented authoring surface.

### What is a plugin and what is a skill

| Role today                         | Shape                       |
|------------------------------------|-----------------------------|
| Workspace and integration tools (`fs`, `exec`, `memory-*`, `mcp`) | plugin |
| Hooks (`hook-permissions`, `hook-approvals`, `caveman`, ...)     | plugin |
| MCP bridge                          | plugin |
| LLM drivers (`drivers/driver`)     | plugin |
| Subagent runtime (`subagents`) | plugin/client support component |
| Gateways (CLI, Telegram) | client/plugin |

### Two-tier supervision

```
kernel
  └── tabula-runtime attachment
        ├── plugin worker
        │     └── plugin-owned child process
        └── plugin worker
```

`tabula-runtime` owns plugin worker processes. Plugins own their children through
a process-group leader pattern (`killpg` on shutdown). The kernel never reaches
into plugin workers or their children.

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
- `kernel_client` — WebSocket wrapper for low-level clients
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

Operational controls such as limits, orphan cleanup, and spawn policy live in
the subagent bundle rather than the kernel.

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

- download the `tabula` and `tabula-runtime` binaries from GitHub Releases
- download the runtime payload tarball (launchers, examples, service files,
  bundled SDK package artifacts, and the `tabula-distro` source)
- create `$TABULA_HOME/.venv` and install Python runtime dependencies
- install `tabula-distro` from the bundled tools/ directory and expose
  `tabula-install` on `$TABULA_HOME/bin`

After the kernel installer finishes, install a distro yourself:

```bash
tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
```

### Source install

`scripts/install-dev.sh` / `scripts/install-dev.ps1`:

- build the `tabula` and `tabula-runtime` binaries from source
- install shared SDK packages into `$TABULA_HOME/.venv`
- copy service files
- create a venv with dev dependencies
- install `tabula-distro` (editable) and expose `tabula-install` on `$TABULA_HOME/bin`

Same follow-up: install a distro with `tabula-install distro install <path-or-uri>`.

Both paths intentionally stop at the local runtime layer. Distro composition is
always done by `tabula-install distro install` or `tabula-install app run/apply`, which resolves a distro plus its
declared bundles into the active runtime surface.

## Runtime surfaces

There are three related but different layouts to keep in mind.

### Repository layout

The source tree is split across three repos:

- `tabula/` — kernel, examples, installation scripts, and the `tabula-distro`
  installer.
- `tabula-bundles/` — reusable collections of skills, plugins, and clients
  (`base/`, `workspace/`, `drivers/`, `memory/`, `caveman/`, `code/`).
- `tabula-distrib/` — `code/`, `claw/`, `guardian/` distros, each with
  its own `boot.py`, `templates/`, optional in-tree `skills/`/`plugins/`,
  and `distro.toml`.

### Installed active layout

The running agent sees a flat tree under `$TABULA_HOME/`:

- one active `boot.py`
- one active `templates/`
- one active `skills/`
- one active `plugins/`
- shared SDK packages installed in `$TABULA_HOME/.venv`

### Tool execution layout

Skills are prompt/instruction artifacts. The kernel does not import or invoke
skill code as part of tool execution.

Plugins run as long-lived subprocesses and communicate via stdio worker
protocol messages. Tool calls dispatched to a plugin are sent on the same
channel, not by re-spawning a new process.

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

- `base/` — cron, hook-logger, hook-permissions, observer, pair,
  sessions, skill-contract, tabula-guide, timer, mcp
- `workspace/` — `fs` and `exec` plugins
- `drivers/` — `driver`, `subagent`, plus `_drivers/` shared support code
- `memory/` — memory-save, memory-search, memory-admin
- `caveman/` — minimal experimental skill set
- `code/`, `subagents/` — coding and delegation components

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

- **kernel <-> runtime** — Runtime API attachment, tenant-scoped catalogs, tool
  calls, events, reload, and status.
- **runtime <-> plugin** — long-lived stdio NDJSON worker protocol (`init`,
  `call`, `result`, `event`, `event_reply`, `tools_updated`, `send`, `log`,
  `shutdown`).
- **boot <-> kernel** — one JSON config object on stdout.
- **distro <-> install** — `tabula-install` expects `boot.py`, `templates/`,
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
- [`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) — `code/`,
  `claw/`, `guardian/` distros
- [`tabula-bundles`](https://github.com/bamanoz/tabula-bundles) — reusable
  bundles
- `internal/kernel/protocol.go` — Go-side protocol constants
- `internal/runtime/worker/wire` — runtime-owned worker protocol messages
