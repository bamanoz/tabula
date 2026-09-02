# Install UX Report

Date: 2026-07-21

This report compares Tabula's current installation path with several popular
agent CLIs and recommends an implementation path toward a one-liner install that
still preserves distro choice.

## Executive Summary

Tabula currently installs like a runtime platform. Most competing agents install
like a product command.

The core installer successfully downloads release artifacts, installs the local
runtime layer, creates a Python virtual environment, links commands, prepares
`$TABULA_HOME`, and can install a service. However, the user still has to know
what to do next: choose or install a distro, configure provider credentials,
start or attach to the runtime, and then connect with a client. That is too much
conceptual load for a first run.

Target outcome:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro code
```

or, once a stable short URL/domain exists:

```bash
curl -fsSL https://tabula.ai/install | bash -s -- --distro code
```

The command should leave the user at a working agent prompt or print exactly one
next command that opens the prompt.

The implementation should not move product policy into the kernel. The simplest
safe shape is a thin onboarding/product layer in the installer and
`tabula-install`, backed by distro-owned metadata in `tabula-distrib`.

## Current Tabula Path

The documented binary installer is:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

The installer requires Python 3.11+, installs to `$TABULA_HOME` by default, and
links these main commands:

- `tabula`
- `tabula-runtime`
- `tabula-agent`
- `tabula-install`

After core install, an agent is not necessarily ready. The user must configure a
provider key and install a project-scoped tenant:

```bash
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> "$TABULA_HOME/.env"
tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
tabula-agent
```

Optional `tabula.agent.toml` stores distro source plus distro-owned values.
`tabula-agent apply` installs or rematerializes its host-local backing tenant.

### Current Friction Points

- The first-run path exposes too many platform concepts: `$TABULA_HOME`, distro,
  tenant, runtime, service, and frontend.
- Provider setup is manual `.env` editing rather than an onboarding flow.
- Distro installation is separate from core installation.
- The beginner path has several valid commands, but no single obvious product
  command.
- Runtime requirements are distro-specific and can fail late. For example,
  `tabula.code` requires `npx` and `uvx`; `tabula.claw` requires `uvx`.
- There is no single `doctor` command that confirms the full installed path:
  core binary, selected tenant, credentials, service, runtime, plugins, and a
  test completion.

## Competitor Install UX

### Codex

Typical path:

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
codex
```

Alternatives include `npm install -g @openai/codex` and
`brew install --cask codex`. First-run authentication is interactive through the
CLI. There is no separate platform assembly stage before first prompt.

Complexity: 2/10.

### Claude Code

Typical path:

```bash
npm install -g @anthropic-ai/claude-code
claude
```

Authentication is available through `claude auth login` or first-run flows. The
main product command is `claude`. Native install/migration exists, but it does
not complicate the beginner path.

Complexity: 2/10.

### OpenCode

Typical path:

```bash
curl -fsSL https://opencode.ai/install | bash
cd /path/to/project
opencode
```

The TUI offers `/connect`; CLI auth is also available with
`opencode auth login`. Package manager options are broad, but the primary path
is still one installer plus one command.

Complexity: 3/10.

### Hermes Agent

Typical path:

```bash
curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash
hermes setup
```

The installer provisions much of the runtime stack, including Python, Node,
`uv`, `ripgrep`, `ffmpeg`, and Playwright/browser dependencies where applicable.
The setup command centralizes model, tool, gateway, and portal configuration.

Complexity: 4/10.

### OpenClaw

Typical path:

```bash
npm install -g openclaw@latest
openclaw onboard --install-daemon
openclaw gateway status
```

This is close to Tabula's product category because it has a gateway daemon,
channels, models, plugins, and background service. The important UX difference
is that complexity is concentrated in `openclaw onboard`.

Complexity: 5/10.

### Tabula

Typical path today:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
echo 'ANTHROPIC_API_KEY=sk-ant-...' >> "$TABULA_HOME/.env"
tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
tabula-agent
```

Complexity: 7/10.

## Desired User Experience

### Default One-Liner

The installer should support a product-level default distro while preserving
explicit distro choice:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Default behavior should be equivalent to:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro code
```

The installer should:

1. Install or upgrade Tabula core.
2. Resolve the selected distro.
3. Check distro runtime requirements before materialization where possible.
4. Offer clear install hints for missing required executables.
5. Configure provider credentials interactively or from environment variables.
6. Materialize a tenant for current workspace or default binding.
7. Start or reload the managed service/runtime.
8. Open the CLI, or print a single command such as `tabula`.

### Explicit Distro Choice

Supported forms should include short names and source URIs:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro code
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro claw
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=code'
```

Short names should be resolved by the installer or `tabula-install` to pinned or
version-compatible distro source URIs. The kernel should not know these names.

### Non-Interactive CI Path

The same flow should work without prompts:

```bash
ANTHROPIC_API_KEY=sk-ant-... \
TABULA_DISTRO=code \
TABULA_ONBOARD_NONINTERACTIVE=1 \
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

or:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | \
  bash -s -- --distro code --provider anthropic --non-interactive
```

If required credentials or executables are missing, non-interactive mode should
fail early with a short actionable message.

## Implementation Recommendations

### 1. Add `tabula-install onboard`

Implement a product-level onboarding command in `tools/tabula-distro`, not in
the kernel:

```bash
tabula-install onboard [--distro code] [--workspace .] [--global] [--provider anthropic] [--non-interactive]
```

Responsibilities:

- Resolve a distro short name or source URI.
- Install or reinstall the distro with version checks.
- Read the distro's runtime requirements.
- Check required executables and print distro-owned install hints.
- Configure provider credentials in `$TABULA_HOME/.env` or the existing runtime
  config surface.
- Materialize a project-scoped tenant using the distro's tenant contract.
- Bind the current workspace unless `--global` or `--no-bind` is selected.
- Start or reload the managed service when requested.
- Optionally launch `tabula-agent` after setup.

This command can delegate to existing tenant install/materialize and managed
service code paths. Value is orchestration and UX, not a second installer.

### 2. Add Installer Flags That Forward To Onboarding

Extend `scripts/install.sh` to parse product-level flags:

```bash
--distro <name-or-uri>
--provider <id>
--workspace <path>
--global
--non-interactive
--no-start
--no-launch
```

After installing core, the script should run:

```bash
tabula-install --home "$TABULA_HOME" onboard ...
```

Keep the existing generic post-install forwarding for advanced users, but make
`onboard` the default path when no advanced subcommand is supplied.

### 3. Add A Thin `tabula` Product Command

Users should not need to distinguish managed service internals from frontend
launch. Product command should do expected thing:

```bash
tabula
```

Desired behavior:

- If no bound tenant is installed, run or suggest agent install.
- If managed runtime is not running, start it or explain exact command.
- Resolve tenant from explicit selection or longest project binding.
- Start and verify selected tenant runtime without assuming any client component.

This wrapper can live outside the kernel. It should not add distro semantics to
kernel code.

### 4. Add Distro Catalog Metadata Outside The Kernel

Short names require a catalog. Keep it in installer/distro layers, not kernel.

Possible shapes:

```toml
[[distros]]
name = "code"
id = "tabula.code"
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=code"
default = true

[[distros]]
name = "claw"
id = "tabula.claw"
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"
```

The catalog can initially be embedded in `tabula-install` or shipped in the
release payload. Later it can be fetched from a signed URL or released artifact.
Do not put this catalog into kernel routing/runtime code.

### 5. Make Provider Setup First-Class

Add a small credential setup flow:

```bash
tabula-install auth setup --provider anthropic
tabula-install auth test
```

`onboard` should call it. Minimal first version:

- Detect `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` in the environment.
- If interactive and missing, prompt securely when possible.
- Write only missing keys to `$TABULA_HOME/.env`.
- Never overwrite existing non-empty keys without confirmation.
- Run a cheap provider validation if a suitable distro/plugin path exists; if
  not, at least validate presence and format.

### 6. Add `tabula-install doctor`

A one-liner is only good if failure recovery is obvious. Add:

```bash
tabula-install doctor
```

Checks:

- `TABULA_HOME` exists and has expected layout.
- `tabula`, `tabula-runtime`, `tabula-agent`, and `tabula-install` are reachable.
- Selected tenant and its version 2 install lock points to an installed distro tree.
- Distro runtime requirements are satisfied.
- Provider credentials are present.
- Managed service status is known.
- Runtime can load plugin catalog.
- Optional smoke test can complete one prompt.

Installer failures should end with:

```text
Run: tabula-install doctor
```

### 7. Keep Advanced Paths Available

Power-user commands remain explicit:

```bash
tabula-install distro install <path-or-uri>
tabula-install tenant install <source> --id <tenant> --root <project>
tabula-agent --tenant <tenant>
tabula serve --runtime-mode external
```

Beginner path stays short without hiding platform controls.

## Suggested Phasing

### Phase 1: Installer Default To Onboard

- Add product-level onboarding that wraps tenant installation.
- Add `--distro` support to `scripts/install.sh`.
- Make no-argument install print or run the onboarding path.
- Support `code`, `claw`, and full source URI.
- Do not add auto-install of Node/uv yet; only detect and provide hints.

Acceptance criteria:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro claw
```

installs core, installs `claw`, checks `uvx`, configures credentials or gives an
actionable prompt, starts runtime or prints one command, and leaves no conceptual
fork in the output.

### Phase 2: Product Command And Doctor

- Add product launcher behavior for project tenant selection.
- Add `tabula-install doctor`.
- Update README quick start to one primary path.
- Keep advanced docs in `docs/TENANT_MATERIALIZER_CONTRACT.md`, `docs/DISTROS.md`, and
  `tools/tabula-distro/README.md`.

Acceptance criteria:

```bash
tabula
```

either opens an agent session or tells the user one exact setup/doctor command.

### Phase 3: Better Dependency Provisioning

- Optionally auto-install `uv`/`uvx` on platforms where this is safe and
  predictable.
- Optionally detect Node and guide to supported install methods.
- Add `--install-missing-deps` for explicit opt-in.
- Keep non-interactive mode fail-fast by default.

### Phase 4: Stable Public Installer URL And Package Managers

- Publish a short stable installer URL.
- Add Homebrew or npm-style install if it matches release operations.
- Keep GitHub release artifacts as source of truth.

## Risks And Boundaries

- Do not move distro selection into the kernel. Distro choice is product policy.
- Do not make installer overwrite user-owned config or existing credentials.
- Do not make `TABULA_HOME` look like a workspace. It remains runtime/config/state
  root.
- Do not hardcode `~/.tabula` in user-facing behavior except as the default value
  of `TABULA_HOME`.
- Do not make automatic dependency installation mandatory; users need a dry-run
  and non-interactive failure mode.
- Keep release payload and installed `tabula-guide` current when onboarding,
  runtime layout, config, or troubleshooting behavior changes.

## Recommended Final UX Copy

Beginner README should eventually reduce to:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
```

Then:

```bash
tabula
```

Advanced selection remains one flag:

```bash
curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- --distro claw
```

Power users still get the platform API:

```bash
tabula-install distro install <path-or-uri>
tabula-agent
```
