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

In the built-in installer flow, a distro is expected to be a directory with
three required parts:

```text
my-distro/
├── boot.py
├── templates/
└── skills/
```

Optional additions are fine, but these three are the contract expected by
`scripts/install-distro.py`.

At install time, the distro is copied into:

```text
~/.tabula/distrib/<name>/
```

Then `~/.tabula/distrib/active` is updated to point at it, and Tabula rebuilds
the flat runtime surface:

```text
~/.tabula/boot.py    -> distrib/active/boot.py
~/.tabula/templates/ -> distrib/active/templates/*
~/.tabula/skills/    -> distrib/active/skills/*
```

`skills/lib/` is the one important exception: it is shared runtime code, not
owned by any single distro.

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

Tabula currently ships with two distros:

- `assistant`
- `guardian`

They share the same kernel and protocol, but they are different products.

### `assistant`

The default general-purpose distro.

What it is for:

- daily driver personal agent
- terminal-first interaction
- OpenAI-compatible local API
- Telegram bot usage
- tool use across files, memory, sessions, MCP, and scheduling
- multi-step work using real subagents

What it includes:

- provider drivers: Anthropic and OpenAI
- gateways: CLI, HTTP API, Telegram
- tool and support skills: files, sessions, pair, MCP, timer, cron, clawhub
- hooks: logging and permissions
- observer skill for metrics
- provider-matched subagents
- memory bundle: `memory-save`, `memory-search`, `memory-admin`

How it works:

- `distrib/assistant/boot.py` scans the active `skills/` tree
- reads `SKILL.md` recursively
- assembles the main system prompt from templates and project files
- selects the active provider via `TABULA_PROVIDER` / config
- exposes discovered tools and slash commands

Philosophy:

- broad capability surface
- composable skills
- agent can extend itself by writing more skills
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
- Anthropic and OpenAI drivers adapted for guardian
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

- `distrib/guardian/boot.py` builds a fixed runtime
- does not depend on broad dynamic skill discovery the way assistant does
- exposes one tool and a small prompt surface

Philosophy:

- narrow capability surface
- strong operational clarity
- sandbox first
- optimized for one job, not general companionship

## Distros vs bundles vs skills

These three concepts are related but different.

### Skill

Smallest extension unit.

- one capability or process
- in assistant today, commonly represented as `SKILL.md` plus some executable
  entrypoint
- examples: `files`, `hook-logger`, `gateway-cli`

### Bundle

Reusable collection of optional skills.

- lives under `bundles/<name>/`
- linked into the flat runtime surface when installed
- examples: the `memory` bundle

Bundles are capability packs.

### Distro

Whole product assembly.

- defines boot behavior
- defines prompt templates
- defines the default skill universe
- can include its own install hook and packaging assumptions

Distros are not just bigger bundles. They decide what the runtime *is*.

## Installing and switching distros

From a local path or GitHub tree URL:

```bash
```

What this does:

1. validate that the source matches the current installer expectation:
   `boot.py`, `templates/`, and `skills/`
2. copy it into `~/.tabula/distrib/<name>`
3. copy any required bundles referenced by symlinked skills
4. update `~/.tabula/distrib/active`
5. rebuild `boot.py`, `templates/`, and `skills/` symlink fan-out

On source installs, `scripts/install-dev.sh --distro <name>` does the same as
part of the install flow.

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

- Assistant is broad.
- Guardian is narrow.

That is a feature, not inconsistency.

### 3. Should boot be dynamic or fixed?

Assistant-style boot:

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
pile of skills.

Skills are how an agent grows.
Bundles are how capabilities are shared.
Distros are how complete agents become recognizable products.

Today there are only two built-in distros. That is enough to prove the model,
but not enough to call the ecosystem mature yet.
