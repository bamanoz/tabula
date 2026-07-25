# Architecture

This document describes how Tabula is put together today.

If `README.md` explains *what Tabula is*, this file explains *how it actually
works*.

## Top-level model

Tabula has five main concepts:

1. **Kernel** — small Go runtime that owns sessions, routing, hooks, and
   process supervision.
2. **Kernel config** — installer-written `$TABULA_HOME/config/kernel.toml`
   containing only kernel transport settings.
3. **Skills** — prompt/instruction artifacts. They do not publish executable
   tools. Manifest: `SKILL.md` (Markdown + frontmatter).
4. **Plugins** — runtime worker processes that subscribe to bus events,
   publish executable tools, and own their own lifecycle. Manifest:
   `plugin.toml`.
5. **Distro** — a packaged runtime surface: templates, runtime config, and a set
   of bundles (which themselves are mixed collections of skills and plugins).

Message flow usually looks like this:

```text
user -> gateway -> kernel -> driver -> tools / hooks / subagents
```

## Kernel

The kernel lives in `cmd/tabula/` and `internal/kernel/`.

Responsibilities:

- run the WebSocket server
- read `$TABULA_HOME/config/kernel.toml`
- accept runtime attachments and route tool calls to runtime-hosted workers
- route messages between session members
- expose the runtime-attached skill/plugin tool catalog
- enforce hook ordering and own level-one process supervision

The kernel intentionally does **not** know about Anthropic, OpenAI, Telegram,
memory, MCP, or any other product feature. Those are all userland components.

### Built-in kernel tools

The kernel publishes **no** LLM-visible tools by default. The native tool
catalog is empty; everything (including `shell_exec`-style commands) is
delivered by plugins from the active distro.

This is intentional: distros decide their tool surface, and the kernel does not
impose command execution or process-management tools as a baseline.

Tool execution happens through attached `tabula-runtime` processes. For the
standard local topology, `tabula serve --runtime-mode managed` starts and owns
the local runtime child.

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
`tabula_plugin_sdk.kernel_client`. Plugins should use the runtime worker
protocol through `tabula-runtime` instead of opening kernel WebSockets directly.

## Boot

Tabula does not hardcode its runtime in Go. Installs write kernel-owned startup
settings to `$TABULA_HOME/config/kernel.toml`.

The preferred kernel config is `$TABULA_HOME/config/kernel.toml`:

```toml
[kernel]
url = "ws://127.0.0.1:8089/ws"

[runtime_wss]
enabled = false
```

`tabula serve --runtime-mode managed` is the standard local stack entrypoint: it
reads `kernel.toml`, serves the kernel endpoints, starts `tabula-runtime` as a
managed child, and shuts that child down with the kernel. Foreground and user
service launches use this same process topology. Use `--runtime-mode external`
when a separately managed runtime attaches to the kernel.

### `.env` loading

The kernel is the **single canonical loader** of `$TABULA_HOME/.env`. During
startup, `tabula serve` reads the file with `loadEnvFile` and populates
`os.Environ()` for every key that is not already set in the shell environment.
Downstream workers inherit those values through normal process inheritance.

Precedence is fixed:

1. Shell environment (highest — never overridden by the file).
2. `$TABULA_HOME/.env` (file values, applied with `setdefault` semantics).

### Plugin discovery

`$TABULA_HOME/config/runtime.toml` defines runtime layout. Plugin loading goes
through `runtime.toml` only.

- `tabula-install <distro>` writes `plugin_dirs` (plus `skill_dirs`,
  `[[kernel]]`, `[pool]`) into `runtime.toml` via the `tomlkit`-based writer.
  User comments and unknown keys in the file are preserved.
- `tabula serve` reads `kernel.toml` for kernel transport settings, then
  verifies `runtime.toml` exists. It fails fast with an explicit
  "run `tabula-install <distro>` first" message when the file is missing.
- `tabula-runtime` reads `runtime.toml` and loads plugins from `plugin_dirs`.
- Optional `[plugin_kinds.<kind>] depends_on = [...]` entries in
  `runtime.toml` express runtime composition ordering between plugin `[kind]`
  classes. Plugin manifests declare their own kind; runtime config wires kinds
  together for the installed deployment.
- After `tabula-install --update` the installer atomically updates
  `$TABULA_HOME/run/reload.touch`. The kernel polls that file and triggers a
  plugin reload through `runtime.toml`.

To add or remove a plugin without going through the installer, edit
`runtime.toml` directly and `touch run/reload.touch`. The kernel picks up the
new layout on the next reload tick.

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

At the kernel level, a distro is an installed runtime layout. The built-in
`tabula-distro` installer expects a directory
with:

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
$TABULA_HOME/templates/*                -> distrib/active/current/templates/*
$TABULA_HOME/skills/*                   -> distrib/active/current/skills/* + bundle skills
$TABULA_HOME/plugins/*                  -> distrib/active/current/plugins/* + bundle plugins
$TABULA_HOME/tenants/<id>/templates/*   -> distrib/<distro>/generations/<generation>/templates/*
$TABULA_HOME/tenants/<id>/skills/*      -> distrib/<distro>/generations/<generation>/skills/*
$TABULA_HOME/tenants/<id>/plugins/*     -> distrib/<distro>/generations/<generation>/plugins/*
$TABULA_HOME/tenants/<id>/apps/*        -> distrib/<distro>/generations/<generation>/apps/*
$TABULA_HOME/tenants/<id>/packages/*    -> distrib/<distro>/generations/<generation>/packages/*
```

Shared SDK packages such as `tabula_plugin_sdk` are installed into
`$TABULA_HOME/.venv` by the installer from bundled package artifacts. They are not
materialized as special legacy support directories in the runtime surface.

The root runtime surface remains a flat compatibility view of the active distro.
Each materialized tenant instead pins one exact distro generation in its install
lock and links its component surfaces directly to that immutable tree. Installing
or selecting another distro does not rewrite existing tenant surfaces. Bundle
code is shared through links; tenant config, sessions, state, cache, logs, and
workers remain isolated.

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

A runtime worker process. Plugins:

- subscribe to bus events through worker `event` frames,
- publish tools with `init_ack` and `tools_updated`,
- spawn and supervise their own children under their own process group,
- hold state for as long as they run.

Manifest is TOML; an optional `README.md` provides human docs (not parsed).

```toml
id = "mcp"
name = "MCP bridge"
version = "0.3.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"
scope = "tenant"      # default; "runtime" shares one warm worker across tenants

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

### Introspection

`tabula config inspect` is the read-only static view of the installed runtime
surface. It reads `$TABULA_HOME/config/runtime.toml`, resolved path locations,
tenant plugin catalogs, plugin manifests, and optional plugin config overlays.
It does not execute distro boot. JSON output is available with
`--format=json`; the schema is beta until Tabula v1.0.

`tabula config inspect --plugin <id>` includes the effective plugin config from:

1. `$TABULA_HOME/config/global.toml` under `[plugins.<plugin-id>]`
2. `$TABULA_HOME/config/plugins/<plugin-id>/config.toml`
3. `$TABULA_HOME/tenants/<tenant>/config/plugins/<plugin-id>/defaults.toml`
4. `$TABULA_HOME/tenants/<tenant>/config/plugins/<plugin-id>/config.toml`
5. `$TABULA_HOME/tenants/<tenant>/config/plugins/<plugin-id>/overrides.toml`

Secret-like keys such as `token`, `password`, `api_key`, and `client_secret` are
printed as `<redacted>`.

`tabula health` walks the same runtime plugin catalog. Plugins that expose a
zero-argument `health` tool are called through the runtime worker protocol;
plugins without that tool are reported as `skipped`. It does not start a kernel
session or run an LLM driver.

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

The release installers can also install and bind one project-scoped agent:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | \
  bash -s -- --distro 'git+https://github.com/owner/distros.git@main#path=my-distro'
```

If GitHub Actions is unavailable, publish the same release artifacts locally
from a tagged checkout:

```bash
make release-local-dry-run   # build assets only
make release-local           # build assets and create/update the GitHub release
```

`make release-local` requires `gh` authentication and a tag matching `VERSION`
on `HEAD`, for example `v0.9.3` when `VERSION` is `0.9.3`.

### Source install

`scripts/install-dev.sh` / `scripts/install-dev.ps1`:

- build the `tabula` and `tabula-runtime` binaries from source
- install shared SDK packages into `$TABULA_HOME/.venv`
- copy service files
- create a venv with dev dependencies
- install `tabula-distro` (editable) and expose `tabula-install` on `$TABULA_HOME/bin`

Same follow-up: install a distro with `tabula-install distro install <path-or-uri>`.

Both paths intentionally keep distro composition outside the kernel.
`tabula-install distro install` builds a global distro surface; `tabula-agent
install/apply` creates a project-scoped tenant pinned to one immutable distro
generation.

## Runtime surfaces

There are three related but different layouts to keep in mind.

### Repository layout

The source tree is split across three repos:

- `tabula/` — kernel, examples, installation scripts, and the `tabula-distro`
  installer.
- `tabula-bundles/` — reusable collections of skills, plugins, and clients
  (`base/`, `workspace/`, `drivers/`, `memory/`, `caveman/`, `code/`).
- `tabula-distrib/` — product distros, each with `distro.toml`, templates,
  and optional in-tree `skills/`/`plugins/`.

### Installed active layout

The running agent sees a flat tree under `$TABULA_HOME/`:

- one active `config/kernel.toml`
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

- `extensions/` — plugin SDK, skills plugin, and `tabula-guide`
- `security/` — permission and approval hooks plus security guide
- `async/` — deferred tool calls and tool result artifacts
- `collaboration/` — sessions and pairing
- `integrations/` — external integrations such as MCP
- `interaction/` — human interaction tools such as `question`
- `observability/` — runtime logging hooks
- `productivity/` — `cron`, `todo`, and `wait`
- `workspace/` — `fs` and `exec` plugins
- `drivers/` — `driver` plus driver SDKs and shared support code
- `mempalace/`, `caveman/`, `codegraph/`, `openspec/`, `subagents/` — domain-specific capabilities

A distro lists bundles in `distro.toml`:

```toml
[sources.tabula-bundles]
source = "git+https://github.com/bamanoz/tabula-bundles.git@main"

[[bundles]]
name = "extensions"
source = "source:tabula-bundles#path=extensions"

[[bundles]]
name = "drivers"
source = "source:tabula-bundles#path=drivers"
```

For dev work you can override these locally with a `distro.override.toml`
that changes `[sources.tabula-bundles]` to a single `local:` checkout.

At install time, bundle components are linked into the flat runtime surface:
skill components under `skills/`, plugin components under `plugins/`, and
app components under `apps/`.

## Current boundaries

If you are extending Tabula, the important seams are:

- **kernel <-> runtime** — Runtime API attachment, tenant-scoped catalogs, tool
  calls, events, reload, and status.
- **runtime <-> plugin** — long-lived stdio NDJSON worker protocol (`init`,
  `call`, `result`, `event`, `event_reply`, `tools_updated`, `send`, `log`,
  `shutdown`).
- **kernel config <-> kernel** — `$TABULA_HOME/config/kernel.toml`.
- **distro <-> install** — `tabula-install` expects `distro.toml`, templates,
  and optional in-tree components.
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
