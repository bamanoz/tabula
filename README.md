# Tabula

**The Neovim of AI agents.**

A small Go kernel, a plain-files home directory, and skills you can read, fork,
or have the agent write for itself. If you've ever configured Neovim or lived
inside Emacs, you already know how Tabula feels — except the thing being
configured is an agent.

## What Tabula is

Tabula is not a chatbot app and not a framework. It is an **environment** for
building and living with an agent.

- A small Go kernel owns routing, sessions, process supervision, and hooks.
- **Skills** are separate processes that plug into the kernel over WebSocket.
  They can be written by you, installed from a bundle, or written by the agent
  itself.
- Your agent's utilities live as plain files under
  `$TABULA_HOME` (default `~/.tabula`). Like dotfiles, for an agent.
- **Distros** package a kernel + a set of skills + a personality into a
  product. Three ship today: `claw` (general-purpose), `guardian`
  (sandboxed code execution), and `ouroboros` (self-hosting/evolving). You can
  build your own.

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

- **Self-modification is a normal operation.** A skill is whatever the active
  distro knows how to discover, describe, and optionally execute. The agent has
  file-writing and shell tools, so it can create, edit, and install skills for
  itself using the same mechanisms a human extender would use.
- **Process isolation is real.** A skill is a real OS process. Crashes don't
  take down the kernel. Subagents are supervised by userland plugins as real
  child processes with their own session, not fake threads.
- **State is inspectable.** Everything lives in plain files under
  `$TABULA_HOME`. You can `cat`, `diff`, `grep`, and put it in git.
- **The kernel stays small.** Features live in skills, not in the core. Same
  reason `grep` is not in `bash`.

## Status

Tabula is useful today, but it is in the "strong core, maturing extension
surface" phase.

- **Solid:** kernel, process-based skills, distro model, official Anthropic /
  OpenAI SDK drivers, OpenAI-compatible HTTP gateway, plugin-owned subagent
  processes, local memory via MemPalace.
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

Switch distro later:

```bash
tabula-install-distro <local-path-or-github-tree-url>
```

<details>
<summary>Windows</summary>

```powershell
irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1 | iex
```

Windows support exists, but the main development and test flow is
macOS/Linux-first.
</details>

### Install from source

```bash
git clone https://github.com/bamanoz/tabula.git
cd tabula
bash scripts/install-dev.sh                                        # kernel only

# then install a distro (pick one):
tabula-distro install ../tabula-distrib/claw                       # local checkout
tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'
```

Requires Go 1.26+ and Python 3.11+.

## Quick start

```bash
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> "$TABULA_HOME/.env"
tabula-server
tabula-cli
```

Use OpenAI instead:

```bash
cat >> "$TABULA_HOME/.env" <<'EOF'
TABULA_PROVIDER=openai
OPENAI_API_KEY=sk-...
EOF
tabula-server
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
- **Boot** — command from `TABULA_BOOT` that emits one JSON config for the
  kernel. In the built-in distros this is currently implemented in Python.
- **Drivers** — one provider loop per process (Anthropic, OpenAI).
- **Gateways** — the mouths and ears: CLI, HTTP API, Telegram.
- **Skills** — everything else. Tools, hooks, integrations, memory. Their
  exact format is defined by the active distro, not by the kernel.
- **Subagents** — real child processes running their own driver in their own
  session.

### `$TABULA_HOME` — the "dotfiles" of your agent

```text
$TABULA_HOME/
├── distrib/
│   ├── claw/current/
│   ├── guardian/current/
│   ├── ouroboros/current/
│   └── active -> claw
├── boot.py         -> distrib/active/current/boot.py
├── templates/      -> distrib/active/current/templates
├── skills/         # distro skills + bundle skills
├── plugins/        # distro plugins + bundle plugins
├── config/global.toml
├── secrets.json
├── .env
├── data/
├── logs/
├── bin/
└── .venv/
```

The active distro plus the bundles it declares fan out into `boot.py`,
`templates/`, `skills/`, and `plugins/`. Shared SDK/support packages are
installed as normal package artifacts (for example into `.venv` or bundle
`_lib` payloads). During the library-relocation migration, source installs may
still stage temporary legacy support directories for compatibility; don't treat
those as the long-term authoring surface.
Files like `IDENTITY.md`, `SOUL.md`, `AGENTS.md` under `templates/` are the
agent's personality — edit them, or let the agent edit them.

## Distros

Distros are how you package a kernel + skills + personality into a product.
They live in
[`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) and declare
bundle dependencies against
[`tabula-bundles`](https://github.com/bamanoz/tabula-bundles).

### `claw`

Default general-purpose agent.

- providers: Anthropic and OpenAI (official SDKs, from the `drivers` bundle)
- gateways: CLI, OpenAI-compatible HTTP API, Telegram
- tools: `files` (`read`, `list_dir`, `glob`, `grep`, `write`, `edit`, `multiedit`, `apply_patch`), `sessions`, `pair`, `mcp`,
  `timer`, `cron`
- hooks: `hook-logger`, `hook-permissions`
- observability: `observer`
- memory: `memory-save`, `memory-search`, `memory-admin` (backed by MemPalace)
- subagents: provider-matched, real processes

### `guardian`

Focused runtime for sandboxed Python execution.

- single tool: `execute_code` (Python 3 in a Docker sandbox)
- minimal CLI gateway
- builds its sandbox image during install
- no files / mcp / sessions / memory / telegram / hooks

### `ouroboros`

Self-hosting distro for a long-running agent with persistent identity,
scratchpad, task list, and knowledge base. Ships its own driver variants plus
consciousness / control / evolve / review skills.

More about what each distro contains lives in the
[`tabula-distrib`](https://github.com/bamanoz/tabula-distrib) repo.

## Documentation

- [`docs/PHILOSOPHY.md`](docs/PHILOSOPHY.md) — why Tabula is shaped like Linux / Neovim
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — how kernel, boot, distros, and skills fit together
- [`docs/SKILL_AUTHORING.md`](docs/SKILL_AUTHORING.md) — practical guide to writing skills
- [`docs/DISTROS.md`](docs/DISTROS.md) — what distros are and what `claw` / `guardian` / `ouroboros` mean
- [`docs/distro-config.md`](docs/distro-config.md) — `distro.toml` reference

## Common commands

| Command                                             | What it does                                 |
| --------------------------------------------------- | -------------------------------------------- |
| `tabula-server`                                     | Start the kernel with sane defaults          |
| `tabula-cli`                                        | Local terminal gateway                       |
| `TABULA_API_PORT=8090 tabula-api`                   | OpenAI-compatible HTTP gateway               |
| `tabula-install-distro <path-or-github-tree-url>`   | Install or switch the active distro          |
| `tabula serve`                                      | Low-level kernel entrypoint                  |
| `tabula run --prompt "..."`                         | One-shot prompt → response                   |

Direct `tabula serve` and `tabula run` need `TABULA_BOOT`:

```bash
TABULA_BOOT='"$TABULA_HOME/.venv/bin/python3" "$TABULA_HOME/boot.py"' tabula serve
```

For CI-style minimal runs, `boot-cicd.py` is a driver-only boot without the
full shell.

## API gateway

Start a kernel, then start the API gateway:

```bash
tabula-server
TABULA_API_PORT=8090 tabula-api
```

```bash
curl http://localhost:8090/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"tabula","messages":[{"role":"user","content":"hello"}]}'
```

The gateway is OpenAI-compatible on purpose: any OpenAI SDK client can talk to
Tabula without a custom client.

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

Plugins use a standard precedence:

1. code defaults
2. `config/global.toml` under `[plugins.<plugin-id>]`
3. `config/plugins/<plugin-id>/config.toml`
4. plugin-declared environment variables
5. explicit CLI/runtime arguments

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

External skills are instruction-only by default. Their `tools[].exec` entries
are ignored unless those tools are also present in Tabula's active tool surface.

Useful environment variables:

| Variable                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ |
| `TABULA_HOME`                              | Home directory, default `~/.tabula`        |
| `TABULA_WORKSPACE`                         | Assistant workspace, default `~/.agents`   |
| `TABULA_PROVIDER`                          | Active provider                            |
| `TABULA_BOOT`                              | Boot command for `tabula serve / run`      |
| `TABULA_URL`                               | Kernel WebSocket URL                       |
| `TABULA_API_PORT`                          | HTTP port for `tabula-api`                 |
| `TABULA_MAX_SPAWN_DEPTH`                   | Max nested subagent depth                  |
| `TABULA_MAX_CHILDREN_PER_SESSION`          | Max child subagents per session            |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`     | Provider API keys                          |

## Writing skills

A common built-in convention, used by the `claw` distro, is a skill
directory like this:

```text
my-skill/
├── SKILL.md     # frontmatter contract + human-readable docs
└── run.py       # one possible entrypoint used by many built-in skills
```

In `claw`, `SKILL.md` frontmatter declares tools, commands, and
compatibility metadata. Claw boot discovers skills, parses frontmatter,
assembles the system prompt, and exposes tools to the active driver. Some
built-in claw paths also default to `run.py`, but that is a convention of
the current distro, not a platform rule.

This is the same mechanism the agent uses when it writes a new skill for
itself — there is no separate "agent-authored skills" path.

See the
[`skill-contract`](https://github.com/bamanoz/tabula-bundles/tree/main/base/skill-contract)
skill in `tabula-bundles` for the current claw skill convention. Contract
versioning across the wire protocol, boot output, claw skill manifests,
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
