# Philosophy

Tabula is shaped like Linux or Neovim, not like a chatbot or a framework.

This is not a marketing line. It is a design constraint that decides what
goes in and what stays out.

## The Neovim analogy

Neovim works because:

- The core is small, stable, and inspectable.
- Extension is **the** way to use it, not an advanced feature.
- Your config is plain files you put in git. It becomes yours.
- People stay for decades, because the tool grows with them.
- Convention beats magic. `:help`, `~/.config/nvim/`, `runtimepath`. Once
  learned, transferable.

Tabula is the same idea applied to AI agents.

| Neovim                 | Tabula                                |
| ---------------------- | ------------------------------------- |
| `nvim` core            | `bin/tabula` (Go kernel)              |
| Plugins                | Skills (separate processes)           |
| `runtimepath`          | `~/.tabula/skills/`                   |
| `~/.config/nvim/`      | `~/.tabula/`                          |
| `init.lua`             | `IDENTITY.md`, `SOUL.md`, `AGENTS.md` |
| Plugin manager         | `clawhub` skill                       |
| `:help`                | `SKILL.md` per skill                  |
| Distro (LazyVim, etc.) | Distro (`assistant`, `guardian`)      |

## What Tabula is not

- **Not a SaaS chatbot.** Closed UIs, vendor-owned memory, no extension below
  the prompt layer — that is the opposite end of this design.
- **Not a developer framework.** LangChain / CrewAI / AutoGen are libraries
  in one process, where every agent is another call site. Tabula is a
  runtime, not a library.
- **Not a single monolithic agent.** Open Interpreter, Devin, Claude Code
  are products. Useful, but you cannot reshape them into a different agent.

## Design principles

These are non-negotiable. They shape every decision.

### 1. Small kernel, large userland

Features live in skills, not in the core. `grep` is not in `bash`. Memory
is not in the kernel. Telegram is not in the kernel. Even drivers are
skills.

What stays in the kernel: routing, sessions, processes, hooks, the wire
protocol, and four built-in tools (`shell_exec`, `process_spawn`,
`process_kill`, `process_list`). That is the entire ABI.

If a feature can be a skill, it is a skill.

### 2. Process isolation is real

A skill is a real OS process. Crashes are contained. Skills can be written
in any language. Subagents are real child processes with their own
session, not coroutines.

This is the main reason Tabula is not a Python library.

### 3. Plain files as state

Everything user-facing lives as files under `~/.tabula/`. You can `cat`,
`diff`, `grep`, edit by hand, put in git. Nothing is hidden in a database.

This is what makes self-modification natural. The agent reads and writes
the same files you do. No special "agent storage" layer.

### 4. Self-modification is normal

A skill is not a kernel-defined file format. It is whatever the active distro
knows how to discover, describe, and optionally execute. The agent has file and
shell tools, so it can write skills for itself using the same mechanism a human
extender uses. There is no separate "agent-authored" path.

This is what we mean by *the agent grows with you*.

### 5. Stable contracts over rapid features

People will only invest in writing skills if those skills don't break next
month. The wire protocol is versioned. The `SKILL.md` contract will be
versioned. The `skills/_pylib/` public surface will be narrow and stable.

Features can move fast inside the kernel. Contracts cannot.

### 6. Convention over configuration

The kernel keeps its contracts small. Distros are free to define conventions
above them. In the built-in `assistant` distro, if a skill is in `skills/`, it
is discovered; if its `SKILL.md` declares a tool, the tool is exposed; if a
skill is named `driver-anthropic`, it is selected when
`TABULA_PROVIDER=anthropic`. No central registry, no manual wiring.

For assistant, the directory layout *is* the configuration.

### 7. Distros are products

A kernel + a curated skill set + a personality = a product. `assistant`
and `guardian` are two products on the same kernel. Anyone can build
their own.

This is how we expect the ecosystem to grow: not "everyone builds the
same agent", but "everyone builds their own distro".

## Trade-offs we accept

Being shaped this way means giving up some things on purpose.

- **Slower than a Python library.** Inter-process WebSocket calls are not
  free. We accept this for isolation and language freedom.
- **Higher floor for new users.** "Run a kernel and a gateway" is more
  than "import library, call function". We accept this for the long-term
  payoff: once installed, the agent is yours.
- **Smaller initial audience.** People who want plug-and-play won't pick
  Tabula. People who want to live with their agent will.
- **More moving parts visible.** We don't hide the kernel, the skills,
  the boot script. They are part of the model. Hiding them would defeat
  the design.

These are the same trade-offs Linux and Neovim make. They turned out fine.

## What this means in practice

When deciding whether something belongs in Tabula, ask:

- Can it be a skill instead? → Make it a skill.
- Does it require a database / hidden state? → Find a flat-file design.
- Does it require breaking the contract? → Postpone or version it.
- Does it make the kernel bigger? → Justify it.
- Does it make the agent harder to inspect or modify? → Probably no.

## Where we are

The architecture supports all of this today. The cultural shell around it
(stable contract, skill registry as a skill, cookbook-style docs, showcase
of community-built distros) is still being built.

This document is the reference point for "why is Tabula shaped this way".
If a future change conflicts with what is written here, the change should
be questioned, not the document.
