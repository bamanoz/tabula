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
└── distro.toml      # declares bundle dependencies
```

Optional additions (`install.sh`, docs) are fine. A distro never embeds
bundle source — it declares dependencies in `distro.toml` and the installer
fetches them.

At install time, the distro is copied into:

```text
~/.tabula/distrib/<name>/<generation>/
```

and `~/.tabula/distrib/<name>/current` points at the latest generation. The
active distro is selected via `~/.tabula/distrib/active`, and Tabula fans out
the flat runtime surface:

```text
~/.tabula/boot.py    -> distrib/active/current/boot.py
~/.tabula/templates/ -> distrib/active/current/templates/*
~/.tabula/skills/    -> distrib/active/current/skills/* + bundle skills
```

`skills/lib/` is the one important exception: it is the kernel-side runtime
contract copied from the `tabula` repo, not owned by any single distro.

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

- `familiar`
- `guardian`
- `ouroboros`

They share the same kernel and protocol, but they are different products.

### `familiar`

The default general-purpose distro.

What it is for:

- daily driver personal agent
- terminal-first interaction
- OpenAI-compatible local API
- Telegram bot usage
- tool use across files, memory, sessions, MCP, and scheduling
- multi-step work using real subagents

What it includes:

- provider drivers: Anthropic and OpenAI (from the `drivers` bundle)
- gateways: CLI, HTTP API, Telegram (distro-specific)
- tool and support skills via bundles: `files`, `base` (sessions, pair,
  clawhub, timer, cron, hook-logger, hook-permissions, observer, skill-contract,
  tabula-guide), `memory` (save/search/admin)
- MCP bridge (distro-specific)
- provider-matched subagents (from the `drivers` bundle)

How it works:

- `familiar/boot.py` scans the active `skills/` tree
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

- `guardian/boot.py` builds a fixed runtime
- does not depend on broad dynamic skill discovery the way familiar does
- exposes one tool and a small prompt surface

Philosophy:

- narrow capability surface
- strong operational clarity
- sandbox first
- optimized for one job, not general companionship

### `ouroboros`

Self-hosting / self-evolving distro for an agent that maintains its own
identity, scratchpad, task list, and knowledge base.

What it is for:

- long-running agent with a persistent self-model
- background research and self-improvement loops
- experimentation with consciousness/control/evolve skills

What it includes:

- its own driver-anthropic / driver-openai (custom variants)
- subagents from the `drivers` bundle
- distro-specific skills: `identity`, `scratchpad`, `tasks`, `knowledge`,
  `consciousness`, `control`, `evolve`, `review`, `multi-model-review`,
  `status`, `bg`, `git`, `hook-ouroboros-context`, `hook-ouroboros-log`
- shared base skills via the `base` bundle

Philosophy:

- the agent should be able to write things down about itself
- the agent should be able to act on its own task list
- the kernel stays out of the way; identity lives in files

## Distros vs bundles vs skills

These three concepts are related but different.

### Skill

Smallest extension unit.

- one capability or process
- in familiar today, commonly represented as `SKILL.md` plus some executable
  entrypoint
- examples: `files`, `hook-logger`, `gateway-cli`

### Bundle

Reusable collection of skills, kept in
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles).

- referenced from a distro via `distro.toml`
- materialized into the flat runtime surface at install time
- examples: `base`, `files`, `drivers`, `memory`, `caveman`

Bundles are capability packs.

### Distro

Whole product assembly.

- defines boot behavior
- defines prompt templates
- defines the default skill universe
- can include its own install hook and packaging assumptions

Distros are not just bigger bundles. They decide what the runtime *is*.

## Installing and switching distros

From a local path, `local:` URI, or `git+...` URI:

```bash
tabula-distro install ./path/to/my-distro
tabula-distro install local:./path/to/my-distro
tabula-distro install "git+https://github.com/bamanoz/tabula-distrib.git@main#path=familiar"
```

What this does:

1. resolve the distro source (clone+checkout for git+, copy for local)
2. validate that it provides `boot.py`, `templates/`, `skills/`,
   and `distro.toml`
3. resolve every bundle declared in `distro.toml` (git+ or local: sources;
   pinned via lockfile)
4. lay everything out under `~/.tabula/distrib/<name>/<generation>/`
5. update `~/.tabula/distrib/<name>/current` and (if requested)
   `~/.tabula/distrib/active`
6. rebuild the flat `boot.py`, `templates/`, and `skills/` surface

For development, clone `tabula-distrib` next to this repo and run
`bash scripts/install-dev.sh` (kernel only), then
`tabula-distro install ../tabula-distrib/<name>`.

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

- Familiar is broad.
- Guardian is narrow.
- Ouroboros is moderate but very opinionated about self-state.

That is a feature, not inconsistency.

### 3. Should boot be dynamic or fixed?

Familiar-style boot:

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

Today there are three built-in distros (`familiar`, `guardian`, `ouroboros`).
That is enough to demonstrate the model — broad / narrow / self-modifying — but
not enough to call the ecosystem mature yet.
