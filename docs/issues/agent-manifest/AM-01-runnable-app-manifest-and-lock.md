# AM-01 — Runnable app manifest and lockfile

Status: proposed
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/cli, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`
Plan: `docs/plans/PROJECT_AGENT_MANIFEST.md`

## What to build

Add installer-owned support for a runnable agent application manifest. A project
can commit the manifest; a user can run one installer command; the installer has
enough non-sensitive topology to apply the distro, create the app tenant, start
or reuse kernel/runtime processes, and launch the agent.

Preferred target command surface:

```bash
tabula-install app run ./tabula.app.toml
tabula-install app apply ./tabula.app.toml
tabula-install app lock ./tabula.app.toml
tabula-install app audit ./tabula.app.toml
```

Acceptable first slice if the installer binary is not renamed yet:

```bash
tabula-distro app apply ./tabula.app.toml
tabula-distro app lock ./tabula.app.toml
tabula-distro app audit ./tabula.app.toml
```

Manifest shape:

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

[values]
# distro-owned arbitrary TOML tree
```

Do not include platform-level `scope`, `instructions`, `features`, or
`system_prompt` fields. Do include launch topology sections such as `[kernel]`,
`[[runtimes]]`, and `[bindings.*]`.

Implementation components:

- New app manifest parser in installer tooling.
- New app lockfile format, likely `tabula.app.lock` next to the manifest for
  repo-local apps and `app.lock.json` under the materialized tenant/app root.
- Parse topology sections:
  - `[kernel]` with `mode = "managed" | "external"`, `id`, and `url`.
  - `[[runtimes]]` with `mode = "managed" | "external" | "embedded"`, `id`,
    `tenants`, and `[runtimes.exec]` backend config.
  - `[bindings.default]` and `[[bindings.directory]]`.
- Reuse existing `tabula-distro` source resolution, cache, compatibility, and
  lock data for the distro source.
- Add variable expansion for a deliberately small installer context:
  - `${project_root}`
  - `${application_id}`
  - `${tabula_home}`
- Preserve raw `values` as distro-owned data; do not interpret workspace or
  prompt semantics in the installer.
- Treat kernel/runtime topology as installer/deployment data. Secrets and
  machine-specific values may reference `.tabula/local.toml`, environment, or
  secret stores.
- Add `audit` output showing:
  - app id/name
  - manifest path
  - distro source and resolved ref/sha/path
  - lock status
  - materialization target
  - requested binding action, if any

## Acceptance criteria

- [ ] `app lock` resolves the distro source and writes a deterministic app
      lockfile without materializing runtime state.
- [ ] `app audit` displays kernel mode/url, runtime modes, execution backends,
      bindings, app id, distro source, and missing local substitutions.
- [ ] `app apply --frozen` requires an app lockfile and does not update it.
- [ ] `app apply --update` refreshes resolved distro source data and rewrites
      the app lockfile.
- [ ] `values` round-trip as distro-owned TOML data with only approved template
      variables expanded.
- [ ] Secret values are not required and are not written into lockfiles.
- [ ] Invalid app ids fail using the existing tenant id grammar or a stricter
      compatible grammar.
- [ ] Existing `tabula-distro install/update/rollback/list/lock/gc` behavior is
      unchanged.

## Blocked by

- None for parser/lock/audit.
- AM-02 for binding registry integration.
- AM-03 for kernel/runtime launch orchestration.
- AM-05 for distro contract validation/materialization.

## Notes

- Keep this in installer tooling. The `tabula` binary should not grow `agent` or
  `app apply` commands.
- The first slice can materialize only metadata/lock files. Do not fake
  app-scoped runtime support before AM-07.
