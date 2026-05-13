# Distros

A distro is how Tabula packages an agent as a product.

In Tabula, the kernel is intentionally small. Almost everything user-facing
comes from the active distro:

- the boot script
- the prompt templates
- the available skills
- the default runtime philosophy

This is why distros matter so much. They are not presets. They are the main
unit of product design.

## What a distro contains

At the platform level, a distro is just a packaged runtime surface plus a boot
command that can describe itself to the kernel.

In the `tabula-distro` installer flow, a distro is expected to be a directory
with:

```text
my-distro/
├── boot.py
├── templates/
├── skills/          # distro-specific skills only
├── plugins/         # optional distro-specific plugins
└── distro.toml      # declares bundle dependencies
```

Optional additions (`install.sh`, docs) are fine. A distro never embeds
bundle source — it declares dependencies in `distro.toml` and the installer
fetches them.

At install time, the distro is copied into:

```text
$TABULA_HOME/distrib/<name>/<generation>/
```

and `$TABULA_HOME/distrib/<name>/current` points at the latest generation. The
active distro is selected via `$TABULA_HOME/distrib/active`, and Tabula fans out
the flat runtime surface:

```text
$TABULA_HOME/boot.py    -> distrib/active/current/boot.py
$TABULA_HOME/templates/ -> distrib/active/current/templates/*
$TABULA_HOME/skills/    -> distrib/active/current/skills/* + bundle skills
$TABULA_HOME/plugins/   -> distrib/active/current/plugins/* + bundle plugins
```

`$TABULA_HOME/skills` is distro-managed. Distros that want compatibility with
external Agent Skills installers should scan external roots such as an assistant
workspace's `skills/` and `.agents/skills/` directories or
`${XDG_CONFIG_HOME:-~/.config}/agents/skills` separately and treat them as
instruction-only unless explicitly trusted.

Shared SDK packages such as `tabula_plugin_sdk` are installed into the Tabula
venv by the installer from bundled package artifacts. They are runtime
contracts, but they are not owned by a single distro and are not special skill
directories.

## Why distros exist

Distros solve a specific problem: the same kernel should be able to power
different agents with different personalities, tools, and operating models.

That means:

- different prompts
- different skills
- different gateways
- different safety model
- different assumptions about what the agent is for

This is the same reason Linux has distributions and Neovim has opinionated
setups. The kernel is not the product. The assembled environment is.

## Built-in distros

Tabula currently ships three distros in the
[`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) repo:

- `coder`
- `claw`
- `guardian`

They share the same kernel and protocol, but they are different products.

### `coder`

Coding-agent distro with a TUI, structured tool metadata, and a coding-tuned
skill set.

What it is for:

- terminal-first coding agent (claude-code / opencode style)
- structured git, tasks, review, workspace boundary, approvals
- per-turn agent/model/effort selection
- subagents for parallel work

What it includes:

- distro-specific TUI gateway plugin (TypeScript/Ink) under `coder/`
- shared bundles: `base`, `files`, `drivers`, `memory`, `code`, `subagents`

### `claw`

The default general-purpose distro.

What it is for:

- daily driver personal agent
- terminal-first interaction
- OpenAI-compatible local API
- Telegram bot usage
- tool use across files, memory, sessions, MCP, and scheduling
- multi-step work using real subagents

What it includes:

- unified driver plugin (Anthropic and OpenAI selected per-turn)
- gateways: CLI, HTTP API, Telegram (distro-specific)
- tool and support components via bundles: `files`, `base` (sessions, pair,
  timer, cron, hook-logger, hook-permissions, observer,
  skill-contract, tabula-guide), `memory` (save/search/admin), `mcp`
- shared subagent runtime (from the `drivers` bundle)

How it works:

- `claw/boot.py` scans the active runtime tree
- reads `SKILL.md` and `plugin.toml`
- assembles the main system prompt from templates and project files
- exposes discovered tools and slash commands
- launches long-lived plugins (drivers, gateways, subagent, mcp)

Philosophy:

- broad capability surface
- composable skills and plugins
- the agent can extend itself by writing new components
- acts like a living personal environment, not a single-purpose tool

### `guardian`

Focused distro for sandboxed Python execution.

What it is for:

- controlled code execution
- data analysis inside a sandbox
- narrow, auditable runtime
- environments where tool sprawl is a liability

What it includes:

- one primary tool: `execute_code`
- minimal CLI gateway
- driver plugin tuned for guardian
- dedicated sandbox image build during install
- fixed templates for system / tools / guidelines / safety

What it does **not** include:

- files
- MCP
- memory
- Telegram
- sessions
- hooks / observer
- broad general-purpose tool surface

How it works:

- `guardian/boot.py` builds a fixed runtime
- does not depend on broad dynamic discovery the way claw does
- exposes one tool and a small prompt surface

Philosophy:

- narrow capability surface
- strong operational clarity
- sandbox first
- optimized for one job, not general companionship

## Distros vs bundles vs skills/plugins

These concepts are related but different.

### Skill / plugin

Smallest extension units.

- **Skill** (`SKILL.md`): prompt/instruction artifact;
  prompt/instruction artifact. Examples: `tabula-guide`, `skill-contract`.
- **Plugin** (`plugin.toml` + `register(api)`): long-lived process; subscribes
  to events; owns executable tools; has state. Examples: `mcp`,
  `hook-permissions`, `drivers/driver`, `timer`, `memory-save`, `code/git`.

See [SKILL_AUTHORING.md](SKILL_AUTHORING.md) and
[plans/SKILL_PLUGIN_ARCHITECTURE.md](plans/SKILL_PLUGIN_ARCHITECTURE.md).

### Bundle

Reusable collection of skills and plugins, kept in
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles).

- referenced from a distro via `distro.toml`
- materialized into the flat runtime surface at install time
- examples: `base`, `files`, `drivers`, `memory`, `caveman`, `coder-*`

Bundles are capability packs — they may contain a mix of skills and plugins
on the same level.

### Distro

Whole product assembly.

- defines boot behavior
- defines prompt templates
- defines the default capability universe
- can include its own install hook and packaging assumptions

Distros are not just bigger bundles. They decide what the runtime *is*.

## Installing and switching distros

From a local path, `local:` URI, or `git+...` URI:

```bash
tabula-distro install ./path/to/my-distro
tabula-distro install local:./path/to/my-distro
tabula-distro install "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"
```

What this does:

1. resolve the distro source (clone+checkout for git+, copy for local)
2. validate that it provides `boot.py`, `templates/`, `skills/`, optional
   `plugins/`, and `distro.toml`
3. resolve every bundle declared in `distro.toml` (`git+`, `local:`, or
   `source:<alias>` sources; pinned via lockfile)
4. lay everything out under `$TABULA_HOME/distrib/<name>/<generation>/`
5. update `$TABULA_HOME/distrib/<name>/current` and (if requested)
   `$TABULA_HOME/distrib/active`
6. rebuild the flat `boot.py`, `templates/`, `skills/`, and `plugins/` surface

For development, clone `tabula-distrib` next to this repo and run
`bash scripts/install-dev.sh` (installs `tabula` + `tabula-runtime`), then
`tabula-distro install ../tabula-distrib/<name>`.

For local development, keep `tabula.app.toml` in the repo root and use
`make agent prepare`, `make agent run`, and `make agent connect`.

`make agent run` starts the kernel in the foreground with `TABULA_LOG_LEVEL`
defaulting to `info` for that command.

Once a distro has been installed at least once, the installed CLI can reuse its
saved source directly: `tabula-install distro reinstall <name>`.

For a single entrypoint, use `tabula-install distro use <name>`. It prefers the
saved source of an already-installed distro; otherwise it looks for a local
checkout in a sibling `tabula-distrib/<name>` directory, or under an explicit
`--source-root`.

`tabula-install use <name>` is a top-level alias for the same flow.

## Designing a new distro

When making a new distro, decide these things explicitly.

### 1. What job is this distro for?

Bad answer:

- "a better agent"

Good answers:

- code-execution analyst
- writing companion
- ops copilot
- background home automation agent

### 2. How much capability surface should it have?

You do not need every skill in every distro.

- Claw is broad.
- Guardian is narrow.
- Ouroboros is moderate but very opinionated about self-state.

That is a feature, not inconsistency.

### 3. Should boot be dynamic or fixed?

Claw-style boot:

- scans a skill tree
- discovers tools and slash commands
- supports a broad extension environment

Guardian-style boot:

- emits a fixed runtime
- easier to audit
- easier to reason about operationally

Both are valid.

### 4. What should the personality files be?

Your templates define how the agent thinks about itself.

Examples:

- `SYSTEM.md`
- `TOOLS.md`
- `GUIDELINES.md`
- `SAFETY.md`
- `IDENTITY.md`
- `SOUL.md`
- `USER.md`
- `AGENTS.md`

Not every distro needs all of these.

### 5. What install-time assumptions does it make?

Some distros are pure Python skills.
Some need a post-install hook.
Guardian, for example, builds a Docker sandbox image.

That is fine. Distros are allowed to be opinionated.

## Current direction

The long-term idea is that Tabula grows an ecosystem of distros, not just a
pile of components.

Skills and plugins are how an agent grows.
Bundles are how capabilities are shared.
Distros are how complete agents become recognizable products.

Today there are three built-in distros (`coder`, `claw`, `guardian`).
That is enough to demonstrate the model — coding-tuned / general-purpose /
sandboxed — but not enough to call the ecosystem mature yet.
