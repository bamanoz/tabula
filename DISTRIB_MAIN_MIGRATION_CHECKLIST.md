# Distrib Assistant Migration Checklist

## Goal

Bring the repository to this model:

- Core: `cmd/`, `internal/`, `skills/lib/`
- Official distro: `distrib/assistant/`
- Bundles: `bundles/`
- Test/dev runtime skills: `testing/skills/`
- No compatibility layer for old root-level runtime paths

Installed layout target:

```text
~/.tabula/
  bin/
  service/
  distrib/
    assistant/
      boot.py
      templates/
      skills/
  bundles/
  skills/
    lib/
  .venv/
  config/
  data/
  state/
  run/
  logs/
```

## 1. Layout Migration

### 1.1 Create new structure

- [x] Create `distrib/assistant/`
- [x] Create `distrib/assistant/skills/`
- [x] Create `distrib/assistant/templates/`
- [x] Create `testing/skills/`

### 1.2 Move official assistant distro assets

- [x] Move `boot.py` to `distrib/assistant/boot.py`
- [x] Move `templates/*` to `distrib/assistant/templates/*`
- [x] Move official assistant skills from `skills/*` to `distrib/assistant/skills/*`
- [ ] Leave only `skills/lib/` in root `skills/`

### 1.3 Move test/dev-only executable skills

- [x] Move `skills/driver-mock` to `testing/skills/driver-mock`
- [x] Move `skills/subagent-mock` to `testing/skills/subagent-mock`
- [x] Move `skills/gateway-test` to `testing/skills/gateway-test`

### 1.4 Express bundle composition in git

- [ ] Add real symlinks in `distrib/assistant/skills/` to selected bundle skills from `bundles/`
- [ ] Verify symlinks resolve correctly in the working tree

### 1.5 Lock the assistant distro skill set

- [x] Keep `cron` in assistant
- [x] Keep `driver-anthropic` in assistant
- [x] Keep `driver-openai` in assistant
- [x] Keep `files` in assistant
- [x] Keep `gateway-api` in assistant
- [x] Keep `gateway-cli` in assistant
- [x] Keep `gateway-telegram` in assistant
- [x] Keep `hook-logger` in assistant
- [x] Keep `hook-permissions` in assistant
- [x] Keep `mcp` in assistant
- [x] Keep `memory` in assistant
- [x] Keep `observer` in assistant
- [x] Keep `pair` in assistant
- [x] Keep `sessions` in assistant
- [x] Keep `skill-contract` in assistant
- [x] Keep `subagent-anthropic` in assistant
- [x] Keep `subagent-openai` in assistant
- [x] Keep `tabula-guide` in assistant
- [x] Keep `timer` in assistant
- [x] Decide whether `clawhub` remains part of assistant

## 2. Path Model Unification

### 2.1 Add a single helper layer for `distrib/assistant`

- [x] Update `skills/lib/paths.py`
- [x] Add helper for `distrib/assistant`
- [x] Add helper for `distrib/assistant/boot.py`
- [x] Add helper for `distrib/assistant/templates/`
- [x] Add helper for `distrib/assistant/skills/`
- [x] Add helper for `bundles/`
- [x] Add helper for `testing/skills/` if needed

### 2.2 Make `distrib/main/boot.py` self-rooted

- [ ] Make `boot.py` resolve `skills/` relative to `__file__`
- [ ] Make `boot.py` resolve `templates/` relative to `__file__`
- [x] Keep `config/data/state/run/logs` under `TABULA_HOME`

### 2.3 Switch prompt/discovery paths

- [x] Update `skills/lib/prompt_builder.py` to use flat runtime `TABULA_HOME/templates`
- [x] Update `skills/lib/prompt_builder.py` to use flat runtime `TABULA_HOME/skills`
- [x] Update skill discovery to scan the flat runtime skill surface

### 2.4 Switch runtime command paths

- [x] Normalize runtime command paths to flat `python3 skills/.../run.py`
- [x] Keep skill inspection commands on flat `skills/...` paths
- [x] Keep skill discovery commands on flat `skills/` paths

### 2.5 Switch gateway/driver/subagent path constructors

- [x] Update `gateway-api` path logic
- [x] Update `gateway-telegram` path logic
- [x] Update driver/subagent spawn command construction
- [x] Update any helper that assembles runtime command strings

## 3. Installer / Launcher / Service Migration

### 3.1 Source install scripts

- [x] Update `scripts/install-dev.sh`
- [x] Update `scripts/install-dev.ps1`

### 3.2 Release installer and packaging

- [x] Update `scripts/install.sh`
- [x] Update `scripts/package-skills.sh`
- [x] Update `scripts/uninstall.sh`

### 3.3 Installed layout

- [x] Installer installs `distrib/assistant/` and activates it through `~/.tabula/distrib/active`
- [x] Distro installer copies required bundle roots to `~/.tabula/bundles/` when referenced by the selected distro
- [x] Installer copies `skills/lib/` to `~/.tabula/skills/lib/`
- [x] `install-dev` copies `testing/skills/`
- [x] Release install does not include `testing/skills/`

### 3.4 Launchers

- [x] Update `bin/tabula-server`
- [x] Update `bin/tabula-cli`
- [x] Update `bin/tabula-api`

### 3.5 Service defaults

- [x] Update `service/com.tabula.kernel.plist`
- [x] Update systemd unit templates
- [x] Switch default `TABULA_BOOT` to `~/.tabula/boot.py`

## 4. Docs and Examples Migration

### 4.1 README

- [x] Update the installed tree
- [x] Update the boot path
- [x] Update the architecture section
- [x] Update command examples
- [x] Update the bundles section
- [x] Update the kernel tools section

### 4.2 Architecture docs

- [x] Update `tabula-guide`
- [x] Update `skill-contract`

### 4.3 Skill docs

- [x] Update assistant `SKILL.md` files to keep agent-facing paths on the flat runtime contract
- [ ] Update bundle `SKILL.md` files if they mention old paths
- [x] Update `memory` docs
- [x] Update `mcp` docs
- [x] Update `sessions` docs
- [x] Update `subagent-*` docs
- [x] Update `timer` docs
- [x] Update `tabula-guide` descriptions/examples

### 4.4 Prompt templates

- [x] Update `distrib/assistant/templates/AGENTS.md`
- [ ] Update `distrib/main/templates/GUIDELINES.md`
- [x] Update `distrib/assistant/templates/TOOLS.md`

## 5. Tests Migration

### 5.1 Make test homes mirror the installed layout

- [x] Update `tests/runtime_harness.py`
- [x] Ensure test homes contain `distrib/assistant/...`
- [x] Ensure test homes contain `skills/lib`
- [x] Ensure test homes contain `bundles/` when needed
- [x] Ensure test homes contain `testing/skills/` when needed

### 5.2 Update source-level path expectations

- [x] Update tests that read root `skills/`
- [x] Update tests that read root `templates/`
- [x] Update tests that expect root `boot.py`

### 5.3 Update e2e / smoke / fixture setups

- [x] Update `tests/test_runtime_smoke.py`
- [x] Update `tests/test_mock_driver_e2e.py`
- [x] Update `tests/test_subagent_e2e.py`
- [x] Update `tests/test_openai_subagent_e2e.py`
- [x] Update `tests/test_hooks_e2e.py`
- [x] Update `tests/test_observer.py`
- [x] Update `tests/test_mcp_e2e.py`
- [x] Update `tests/test_gateway_*`
- [x] Update `tests/test_boot.py`
- [x] Update `tests/test_system_prompt.py`
- [x] Update `tests/test_skill_config.py`
- [x] Update `tests/test_sessions.py`

### 5.4 Add new layout tests

- [ ] Add a test for the new installed layout
- [ ] Add a test that validates symlinks in `distrib/main/skills/`
- [ ] Add a test that validates `TABULA_BOOT -> distrib/main/boot.py`

## 6. Built-in Tools Policy

### 6.1 Extend the boot contract

- [x] Add `kernel_tools` to `distrib/assistant/boot.py` output

### 6.2 Filter built-ins in the kernel

- [x] Update `cmd/tabula/main.go`
- [x] Filter embedded built-ins by `kernel_tools`
- [ ] Validate unknown built-in names
- [x] Add tests for filtering

### 6.3 Verify exposure

- [x] Verify `init.tools` contains only the allowed built-in subset

## 7. Prompt Coherence for Hidden Built-ins

### 7.1 Make the built-in tool section dynamic

- [x] Update `skills/lib/prompt_builder.py`
- [ ] Pass the actual visible tools into the prompt builder
- [ ] Render built-in tool docs from `init.tools`

### 7.2 Remove unconditional capability assumptions

- [x] Remove unconditional `EXEC` guidance from `AGENTS.md`
- [ ] Remove unconditional `EXEC` guidance from `GUIDELINES.md`
- [ ] Remove the canonical built-in list from `TOOLS.md` as a single source of truth

### 7.3 Verify prompt coherence

- [x] If a built-in tool is hidden, it does not appear in the prompt
- [x] If a built-in tool is visible, it is described in the prompt
- [x] Prompt content does not contradict `init.tools`

## 8. Skill Compatibility with Built-in Policy

### 8.1 Add metadata

- [ ] Add `requires-kernel-tools` to the skill metadata contract

### 8.2 Mark known dependent skills

- [ ] Mark `memory`
- [ ] Mark `mcp`
- [ ] Mark `sessions`
- [ ] Mark `subagent-anthropic`
- [ ] Mark `subagent-openai`
- [ ] Mark `timer`
- [ ] Mark relevant bundle skills

### 8.3 Filter discovery

- [ ] Hide incompatible skills from `## Available skills`
- [ ] Hide incompatible slash/discovery surfaces if needed

### 8.4 Verify compatibility filtering

- [ ] If a built-in tool is hidden, incompatible skills do not appear in prompt/discovery

## 9. Final Verification

### 9.1 Go

- [x] Run the impacted Go test suite

### 9.2 Python

- [x] Run the impacted Python test suite

### 9.3 Install / service

- [x] Perform a local reinstall
- [x] Restart the kernel service
- [ ] Check `/sessions`
- [ ] Check connect/join/init
- [ ] Check at least one smoke scenario on the live install

### 9.4 Acceptance

- [x] Official distro source lives only in `distrib/assistant`
- [ ] Root `skills` contains only `lib`
- [x] Test-only executable skills live in `testing/skills`
- [x] Installed runtime paths are distro-based
- [x] Built-in tools are controlled by boot policy
- [x] Prompt and discovery are coherent with visible built-ins

## Recommended Execution Waves

### Wave 1

- Layout migration
- Path migration
- Installers, services, launchers
- Docs and tests

### Wave 2

- `kernel_tools` in boot output
- Built-in filtering in kernel

### Wave 3

- Dynamic prompt tool rendering
- `requires-kernel-tools`
- Discovery filtering

### Wave 4

- Final regression
- Local reinstall
- Live kernel restart and smoke verification
