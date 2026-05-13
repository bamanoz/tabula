# AM-05 — Claw application contract and materializer

Status: proposed
Type: AFK
Repo: tabula-distrib
Labels: needs-triage, area/distro, area/installer, area/claw

## Parent

Track: `tabula/docs/issues/agent-manifest/README.md`
Plan: `tabula/docs/plans/PROJECT_AGENT_MANIFEST.md`

## What to build

Add a `claw`-owned application contract that tells the installer how `claw`
wants to be applied as a named app instance. This belongs in `tabula-distrib`,
not in the kernel.

Example `claw/distro.toml` addition:

```toml
[application_contract]
version = 1
instance = "tenant"
multi_instance = true
allowed_bindings = ["directory", "default", "manual"]
default_binding = "directory"
values_schema = "application.schema.json"
materializer = "python3 application/apply.py"
prompt_builder = "claw_prompt.builder:build_main_prompt"
subagent_prompt_builder = "claw_prompt.builder:build_subagent_prompt"

[application_contract.runtime]
requires_app_scoped_surface = true
join_uses_tenant_id = true

[application_contract.config]
values_file = "values.toml"
```

Example repo-local `tabula.app.toml` for `claw`:

```toml
[application]
id = "claw-tabula"
name = "Claw for Tabula development"

[distro]
source = "git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw"

[kernel]
mode = "managed"
id = "claw-tabula"
url = "ws://127.0.0.1:8089/ws"

[[runtimes]]
id = "local"
mode = "managed"
tenants = ["claw-tabula"]

[runtimes.exec]
backend = "bare"

[[bindings.directory]]
root = "${project_root}"
app = "claw-tabula"
kernel = "claw-tabula"

[values.workspace]
path = "${project_root}"
external_skill_roots = [
  "${project_root}/skills",
  "${project_root}/.agents/skills",
]

[values.prompt]
project_files = ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]
create_missing_project_files = true
include_external_skills = true

[values.tools]
fs_roots = ["${project_root}", "${project_root}/.agents"]
exec_cwd = "${project_root}"

[values.provider]
name = "anthropic"
model = "claude-sonnet-4-6"

[values.memory]
mode = "project"

[[values.permissions.rules]]
tool = "exec_run"
command = "git push *--force*"
effect = "ask"
```

The materializer maps `values` into tenant/app config. Example outputs for app
`claw-tabula`:

```toml
# $TABULA_HOME/tenants/claw-tabula/config/tenant.toml
[application]
id = "claw-tabula"
distro = "claw"

[claw.workspace]
path = "/Users/mak/src/tabula"
external_skill_roots = ["/Users/mak/src/tabula/skills", "/Users/mak/src/tabula/.agents/skills"]

[claw.prompt]
project_files = ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]
create_missing_project_files = true
include_external_skills = true

[workspace]
project_root = "/Users/mak/src/tabula"
```

```toml
# $TABULA_HOME/tenants/claw-tabula/config/plugins/fs/config.toml
roots = ["/Users/mak/src/tabula", "/Users/mak/src/tabula/.agents"]
follow_symlinks = false
deny_globs = ["**/.git/**", "**/node_modules/**", "**/.env"]
```

```toml
# $TABULA_HOME/tenants/claw-tabula/config/plugins/exec/config.toml
cwd_default = "/Users/mak/src/tabula"
timeout_default_seconds = 60
timeout_max_seconds = 600
env_passthrough = ["PATH", "HOME", "LANG", "TERM"]
deny_commands = ["rm -rf /", "shutdown", "reboot"]
```

```toml
# $TABULA_HOME/tenants/claw-tabula/config/plugins/memory/config.toml
store_path = "$TABULA_HOME/tenants/claw-tabula/state/plugins/memory"
```

Shared memory is an explicit `claw`/memory-plugin extension point, not a Tabula
core concept:

```toml
[values.memory]
mode = "shared"
path = "${local.memory.personal_path}"
```

```toml
# .tabula/local.toml
[local.memory]
personal_path = "/Users/mak/.tabula/shared-memory/personal"
```

## Acceptance criteria

- [ ] `claw` declares an application contract in distro-owned metadata.
- [ ] `claw` values schema validates workspace, prompt, tools, provider, memory,
      and permissions sections without making them platform-wide concepts.
- [ ] `claw` materializer writes tenant-local config files only for the selected
      app id.
- [ ] Materializer never writes secret values from the app manifest into lock
      files.
- [ ] Memory defaults to tenant-local state; shared memory requires explicit
      `values.memory.mode = "shared"` and a resolved local path/reference.
- [ ] Existing legacy default `claw` install keeps working while app apply support is
      introduced.
- [ ] Distro README documents the `claw` application values contract.

## Blocked by

- AM-01 for app manifest parser.
- AM-02 for binding support.
- AM-07 for full concurrent different-distro runtime support.

## Notes

- The names under `[values.*]` are `claw`-owned. They are not a Tabula platform
  schema.
- Keep prompt semantics in `claw`; do not add a generic manifest
  `[instructions]` section.
