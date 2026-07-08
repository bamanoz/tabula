# Tabula

**The Neovim of AI agents.**

A small Go kernel, a plain-files home directory, and skills you can read, fork,
or have the agent write for itself. If you've ever configured Neovim or lived
inside Emacs, you already know how Tabula feels — except the thing being
configured is an agent.

## What Tabula is

Tabula is not a chatbot app and not a framework. It is an **environment** for
building and living with an agent.

- A small Go kernel owns routing, sessions, runtime attachments, and hooks.
- **Plugins** are long-lived runtime workers that publish executable tools.
- **Skills** are prompt/instruction artifacts with optional bundled resources.
- Your agent's utilities live as plain files under
  `$TABULA_HOME` (default `~/.tabula`). Like dotfiles, for an agent.
- **Distros** package a runtime surface, tool policy, and personality into a
  product. Current maintained distros are `claw` (general-purpose), `code`
  (coding-focused), and `guardian` (sandboxed code execution). You can build
  your own.

A Tabula agent is something you own, can inspect, can break, can fix, and can
grow over years. Not something you rent.

## Why it exists

Most AI-agent tooling today is one of three things:

- **A SaaS chatbot.** Closed, owned by the vendor, can't be extended below
  the "prompt" layer.
- **A developer framework** (LangChain, CrewAI, AutoGen, …). A library in one
  process; every agent looks like another call site.
- **A single monolithic app** (Open Interpreter, Claude Code, Devin).
  Opinionated, hard to repurpose.

Tabula is shaped like Linux or Neovim instead: a small, stable kernel with
clear extension contracts, and a userland you build yourself.

That design makes a few things natural:

- **Self-modification is a normal operation.** Skills and plugins are plain
  files in the active runtime surface. The agent has file and command tools, so
  it can create, edit, and install new components using the same mechanisms a
  human extender would use.
- **Process isolation is real.** Executable tools run in runtime-managed plugin
  workers. Crashes don't take down the kernel. Subagents are supervised by
  userland plugins as real child processes with their own session, not fake
  threads.
- **State is inspectable.** Everything lives in plain files under
  `$TABULA_HOME`. You can `cat`, `diff`, `grep`, and put it in git.
- **The kernel stays small.** Features live in skills, not in the core. Same
  reason `grep` is not in `bash`.

## Status

Tabula is useful today, but it is in the "strong core, maturing extension
surface" phase.

- **Solid:** kernel, runtime-hosted plugins, distro model, official Anthropic /
  OpenAI SDK drivers, CLI gateway, plugin-owned subagent processes, local memory
  via MemPalace.
- **Maturing:** stable claw skill-manifest versioning, subagent ops,
  packaged SDK distribution, self-edit safety (git-backed rollback), skill
  distribution story.

The project favors small, composable primitives over big features. It will
stay that way.

## Install

### Release install

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Requires Python 3.11+. Installs to `$TABULA_HOME` (default `~/.tabula`).

Install a specific version:

```bash
VERSION=v1.0.0 curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Install or switch distro later:

```bash
tabula-install distro install <local-path-or-github-tree-url>
```

<details>
<summary>Windows</summary>

```powershell
irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1 | iex

# one-shot install + run from a project with tabula.app.toml
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1))) app run
```

Windows support exists, but the main development and test flow is
macOS/Linux-first.
</details>

### Install from source

```bash
git clone https://github.com/bamanoz/tabula.git
cd tabula
bash scripts/install-dev.sh                                        # installs tabula + tabula-runtime

# then install a distro or run an app manifest:
tabula-install distro install ../tabula-distrib/claw                       # local checkout
tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'
tabula-install app install --workspace .
tabula-install app run

# local dev flow
make agent-prepare
make agent-run
make agent-connect

# installed CLI shortcuts
tabula-install use code
tabula-install distro reinstall code
tabula-install distro use code
tabula-install distro use claw --update
```

Requires Go 1.26+ and Python 3.11+.

## Quick start

```bash
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> "$TABULA_HOME/.env"
tabula-runner
tabula-cli
```

Use OpenAI instead:

```bash
cat >> "$TABULA_HOME/.env" <<'EOF'
TABULA_PROVIDER=openai
OPENAI_API_KEY=sk-...
EOF
tabula-runner
tabula-cli
```

If you installed from a release and the user service is already running,
`tabula-cli` alone may be enough.

## The mental model

Typical message flow:

```text
user ──▶ gateway ──▶ kernel ──▶ driver ──▶ tools / hooks / subagents
```

Pieces:

- **Kernel** — small Go server. Owns sessions, routing, process supervision,
  hooks, and plugin/skill dispatch. It publishes no LLM-visible tools by
  default; `shell_exec`-style tools and subagent operations are supplied by the
  active distro's skills/plugins.
- **Kernel config** — installer-written `config/kernel.toml` containing only
  kernel transport settings.
- **Drivers** — one provider loop per process (Anthropic, OpenAI).
- **Gateways** — the mouths and ears: CLI, HTTP API, Telegram.
- **Plugins** — executable tools, hooks, integrations, memory, MCP, and gateway
  daemons.
- **Skills** — prompt/instruction artifacts discovered by the active distro.
- **Subagents** — real child processes running their own driver in their own
  session.

### `$TABULA_HOME` — the "dotfiles" of your agent

```text
$TABULA_HOME/
├── distrib/
│   ├── claw/current/
│   ├── guardian/current/
│   └── active -> claw
├── templates/      -> distrib/active/current/templates
├── skills/         # distro skills + bundle skills
├── plugins/        # distro plugins + bundle plugins
├── config/
│   ├── global.toml
│   ├── kernel.toml  # kernel transport config — installer-owned
│   └── runtime.toml  # plugin_dirs, skill_dirs, [[kernel]], [pool] — installer-owned
├── secrets.json
├── .env            # loaded by the kernel; workers inherit via os.environ
├── data/
├── logs/
├── run/            # reload.touch, runtime sockets, tokens
├── bin/
└── .venv/
```

The active distro plus the bundles it declares fan out into `templates/`,
`skills/`, and `plugins/`. Shared SDK/support packages are
installed as normal package artifacts (for example into `.venv` or bundle
`_lib` payloads). During the library-relocation migration, source installs may
still stage temporary legacy support directories for compatibility; don't treat
those as the long-term authoring surface.
Files like `IDENTITY.md`, `SOUL.md`, `AGENTS.md` under `templates/` are the
agent's personality — edit them, or let the agent edit them.

Kernel transport lives in `config/kernel.toml`; plugin layout lives in
`config/runtime.toml`. The installer writes both during `tabula-install`; the
running kernel reads them directly and reloads plugins through
`run/reload.touch`. See
[`docs/DISTRO_CONFIG.md`](docs/DISTRO_CONFIG.md) for the contract.

Inspect the installed runtime surface:

```bash
tabula config inspect
tabula config inspect --plugin fs --format=json
tabula health
```

## Distros

Distros are how you package a kernel + skills + personality into a product.
They live in
[`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) and declare
bundle dependencies against
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles).

### `claw`

Default general-purpose agent.

- providers: Anthropic and OpenAI (official SDKs, from the `drivers` bundle)
- gateways: CLI and Telegram
- tools: workspace `fs`/`exec`, `sessions`, `pair`, `mcp`, `timer`, `cron`, `todo`
- hooks: `hook-logger`, `hook-permissions`
- observability: `observer`
- mempalace: `mempalace`, `mempalace-admin` (backed by MemPalace)
- subagents: provider-matched, real processes

### `code`

Focused coding-agent distro.

- providers: Anthropic and OpenAI through the shared driver
- gateway: CLI
- tools: workspace `fs`/`exec`, MCP defaults for Context7, Playwright, and DuckDuckGo,
  mempalace, codegraph, todo, and approval hooks
- runtime requirements: `npx` and `uvx` are required; `rg` is optional for faster grep

### `guardian`

Focused runtime for sandboxed Python execution.

- single tool: `execute_code` (Python 3 in a Docker sandbox)
- minimal CLI gateway
- builds its sandbox image during install
- no files / mcp / sessions / memory / telegram / hooks

More about what each distro contains lives in the
[`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) repo.

## Documentation

- [`docs/PHILOSOPHY.md`](docs/PHILOSOPHY.md) — why Tabula is shaped like Linux / Neovim
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — how kernel, boot, distros, and skills fit together
- [`docs/SKILL_AUTHORING.md`](docs/SKILL_AUTHORING.md) — practical guide to writing skills
- [`docs/DISTROS.md`](docs/DISTROS.md) — what distros are and how the installer composes them
- [`docs/distro-config.md`](docs/distro-config.md) — `distro.toml` reference

## Common commands

| Command                                             | What it does                                 |
| --------------------------------------------------- | -------------------------------------------- |
| `tabula-runner`                                     | Start the kernel plus local runtime wrapper  |
| `tabula-cli`                                        | Connect to a running kernel                  |
| `tabula-install distro install <path-or-uri>`       | Install or switch the active distro          |
| `tabula serve`                                      | Low-level kernel entrypoint                  |
| `tabula run --prompt "..."`                         | One-shot prompt → response                   |

`tabula-runner` is the product wrapper. `tabula serve` is the low-level kernel
entrypoint and does not supervise a local runtime process.

## Configuration

Core configuration uses these files:

- `.env` — local overrides and API keys
- `config/global.toml` — structured global config for providers, gateways,
  sessions, and plugin defaults
- `config/plugins/<plugin-id>/config.toml` — plugin-local config
- `secrets.json` — secret store entries referenced from config

Most installs start with only `.env`:

```bash
# $TABULA_HOME/.env
TABULA_PROVIDER=anthropic
ANTHROPIC_API_KEY=sk-ant-...
```

Structured example:

```toml
provider = "openai"

[openai]
model = "gpt-5.4"
base_url = "https://api.openai.com/v1"
api_key = { source = "store", id = "driver-openai.api_key" }
```

Plugins use a standard precedence. Without a tenant-local effective config,
global plugin config participates normally. When
`tenants/<tenant>/config/plugins/<plugin-id>/config.toml` exists, it is the
installer-compiled effective config and replaces the global plugin file for that
tenant:

1. code defaults
2. `config/global.toml` under `[plugins.<plugin-id>]`
3. `config/plugins/<plugin-id>/config.toml`
4. tenant effective `tenants/<tenant>/config/plugins/<plugin-id>/config.toml`, if present
5. plugin-declared environment variables
6. explicit CLI/runtime arguments

Example plugin-local config:

```toml
# $TABULA_HOME/config/plugins/observer/config.toml
host = "127.0.0.1"
port = 8091
snapshot_poll_sec = 0.5
```

## External Agent Skills

`$TABULA_HOME/skills` is managed by the active distro. Do not install
third-party Agent Skills there directly.

Claw has an assistant workspace separate from `$TABULA_HOME`, resolved as:

```text
TABULA_WORKSPACE -> $TABULA_HOME/config/global.toml [workspace].path -> ~/.agents
```

For compatibility with ecosystem installers, Claw discovers instruction-only
skills from:

- `$TABULA_WORKSPACE/skills/`
- `$TABULA_WORKSPACE/.agents/skills/`
- `~/.agents/skills/`
- `${XDG_CONFIG_HOME:-~/.config}/agents/skills/`
- OpenClaw roots: `~/.openclaw/skills/`, `~/.clawdbot/skills/`, `~/.moltbot/skills/`
- `TABULA_EXTERNAL_SKILLS_DIR`
- `TABULA_EXTERNAL_SKILLS_PATH` (`PATH`-style list)

The Vercel skills installer works through its universal target:

```bash
npx skills add vercel-labs/agent-skills -a universal --skill frontend-design
```

External skills are instruction-only. Executable tools must come from installed
plugins, not from skill frontmatter.

Useful environment variables:

| Variable                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ |
| `TABULA_HOME`                              | Home directory, default `~/.tabula`        |
| `TABULA_WORKSPACE`                         | Assistant workspace, default `~/.agents`   |
| `TABULA_PROVIDER`                          | Active provider                            |
| `TABULA_URL`                               | Kernel WebSocket URL                       |
| `TABULA_MAX_SPAWN_DEPTH`                   | Max nested subagent depth                  |
| `TABULA_MAX_CHILDREN_PER_SESSION`          | Max child subagents per session            |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`     | Provider API keys                          |

## Agent Applications

A runnable agent application manifest can live in a project as
`tabula.app.toml`. It applies a distro, creates a tenant whose id is
`application.id`, starts or reuses the configured kernel/runtime, and binds the
project directory to that app.

On a machine without Tabula installed yet, install Tabula and run the app in one
command from the project directory:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- app run
```

If Tabula is already installed:

```bash
tabula-install app install --workspace .
tabula-install app run
```

See `docs/AGENT_APPLICATIONS.md` for examples, local overrides, bindings, and
Claw memory modes.

## Writing skills

A common convention, used by the `claw` distro, is a prompt-only skill
directory like this:

```text
my-skill/
├── SKILL.md       # frontmatter + human-readable docs
├── references/
└── scripts/       # optional helper scripts/resources, not published as tools
```

In `claw`, `SKILL.md` frontmatter describes the skill and optional
`user-invocable` slash-command behavior. Boot discovers skills, parses
frontmatter, and assembles the system prompt. Executable tools are published by
installed plugins via `plugin.toml` and runtime capability registration.

This is the same mechanism the agent uses when it writes a new skill for
itself — there is no separate "agent-authored skills" path.

See the
[`skill-contract`](https://github.com/bamanoz/tabula-bundles/tree/main/base/skill-contract)
skill in `tabula-bundles` for the current claw skill convention. Contract
versioning across the wire protocol, runtime config, claw skill manifests,
and packaged SDK contracts is still being stabilized; don't rely on temporary
legacy support-dir internals yet.

## Testing

```bash
make test-unit       # fast logic-only
make test-smoke      # minimal runtime boot / connect / init
make test-e2e        # hooks, MCP, observer, mock driver, subagents
make test-contract   # protocol and extension contract checks
```

See [`tests/README.md`](tests/README.md).

## Development

```bash
make build
make install            # runs scripts/install-dev.sh
make test-unit
```

Python runtime deps: `scripts/requirements-runtime.txt`.
Source-install deps: `scripts/requirements-dev.txt`.

## License

MIT
