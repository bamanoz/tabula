# Distro configuration

A Tabula distribution is composed from a declarative config. This document
describes the schema, resolution rules, and on-disk layout managed by
`tabula-distro`.

Related docs: [tools/tabula-distro/README.md](../tools/tabula-distro/README.md).

## `distro.toml`

Lives at the root of a distro source tree (for example
`../tabula-distrib/claw/distro.toml`). The file is required for maintained
distros because `[distro].id` is required.

```toml
[distro]
id      = "tabula.claw"
name    = "claw"
version = "0.1.0"

[requires]
kernel = ">=0.8.0,<1.0.0"

[sources.tabula-bundles]
source = "git+https://github.com/bamanoz/tabula-bundles.git@main"

[[bundles]]
name   = "workspace"
source = "source:tabula-bundles#path=workspace"
# components = ["fs", "exec"]  # optional skill/plugin/app/host-service allowlist
# override = false              # must be true to replace existing

[[bundles]]
name   = "caveman"
source = "source:tabula-bundles#path=caveman"

[[skills]]
name   = "weather"
source = "git+https://github.com/foo/weather-skill.git@main#path=skill"

[[plugins]]
name   = "mcp"
source = "git+https://github.com/foo/mcp-plugin.git@main#path=plugin"

[tenant_contract]
materializer = "python3 tenant/apply.py"
values_schema = "values.schema.json"
values_defaults = "values.defaults.toml"

[[runtime_requirements.executables]]
name = "npx"
required = true
required_for = ["mcp.context7"]
install_hint = "Install Node.js LTS from https://nodejs.org/."
```

A bundle is a directory whose top level holds component directories identified
by `SKILL.md`, `plugin.toml`, `app.toml`, or `service.toml`. The
`components = [...]` allowlist on a bundle entry filters by component directory
name and applies to every shape. The legacy `skills = [...]` spelling is still parsed as an alias
for one compatibility cycle; new configs should use `components`.

### Fields

- `[distro].id` — required stable distro identifier such as `tabula.claw`.
- `[distro].name` — installed distro name. Defaults to the directory name.
- `[distro].version` — optional SemVer (`MAJOR.MINOR.PATCH`). Recorded in
  the lockfile; used by external tooling.
- `[requires].kernel` — optional SemVer constraint against the installed
  kernel version (read from `$TABULA_HOME/VERSION`). Hard-fails on mismatch
  before any work is done. Operators: `>=`, `>`, `<=`, `<`, `==`; clauses
  joined by commas (AND). Example: `">=0.8.0,<1.0.0"`.
- `[sources.<alias>]` — optional source roots for repo-level reuse. Entries can
  refer to them with `source:<alias>#path=<subdir>`, which keeps all bundles
  from the same repo on the same checkout/local root.
- `[[bundles]]` / `[[skills]]` / `[[plugins]]` — ordered arrays of external
  sources.
  - `name` — required. Target path under `skills/` for skills, `plugins/` for
    plugins, or bundle identity for bundles.
  - `source` — required. See URI grammar below.
  - `components` — bundles only. Optional allowlist of skill/plugin/app/host-service component
    subdirectories to include. `skills` is a deprecated alias.
  - `override` — required to replace a pre-existing target with the same name.
- `[tenant_contract]` — optional tenant materialization metadata.
  - `materializer` — shell-split command executed from stable installed distro
    tree. It receives only `TABULA_TENANT_*` context.
  - `values_schema` — distro-owned values schema.
  - `values_defaults` — distro-owned default values TOML.
  See [Tenant Materializer Contract](TENANT_MATERIALIZER_CONTRACT.md).
- `[[runtime_requirements.executables]]` — external commands the distro expects
  on `PATH`. Installer checks these before replacing installed tree.
  - `name` — executable name to resolve with `PATH`.
  - `required` — defaults to `true`. Missing required executables fail install
    or app apply/run; missing optional executables produce a warning.
  - `required_for` — optional list of capabilities such as `mcp.duckduckgo` or
    `fs.grep`.
  - `install_hint` — actionable text shown when missing.

### Bundle manifests

Each external bundle may include a `bundle.toml` at its root that mirrors the
distro schema:

```toml
[bundle]
name    = "drivers"
version = "0.1.0"
components = ["driver", "subagent", "mcp"]

[requires]
kernel = ">=0.8.0,<1.0.0"

[[exports.python_packages]]
name = "tabula_session_sdk"
path = "sessions/sdk/python/src/tabula_session_sdk"
public = true
owner = "sessions"

[[exports.typescript_packages]]
name = "@tabula/skill-sdk"
path = "skills/sdk/typescript"
public = true
owner = "skills"

[[dependencies]]
bundle = "extensions"
components = ["sessions"]
python_packages = ["tabula_session_sdk"]
typescript_packages = ["@tabula/skill-sdk"]
```

When present, `[requires].kernel` is enforced just like the distro-level
constraint. Bundles without a `bundle.toml` are accepted as legacy/unversioned
and skip the check (their entry in `distro.lock.json` will have no `version`).
If `[bundle].components` is omitted, `tabula-distro` discovers components by
walking immediate child directories and selecting those with `SKILL.md`,
`plugin.toml`, `app.toml`, or `service.toml`. If `components` is present, only those relative component paths
are installed; missing entries fail the install instead of being silently
skipped.

`[[exports.python_packages]]` and `[[exports.typescript_packages]]` declare
component-owned SDK/runtime packages. `name` is the import/package name, `path`
is a bundle-relative source path, `owner` identifies the owning component, and
`public` marks whether the package is a stable SDK surface (`true`) or an
internal shared package declared for installation/dependency validation
(`false`). `[[dependencies]]` entries can require runtime `components` and
packages exported through `python_packages` or `typescript_packages`.

Installer resolves dependency closure transitively. Unselected dependencies are
sibling bundles in same source tree. Git dependencies use exact resolved SHA of
requiring bundle; local dependencies use sibling directory in same checkout.
Auto-resolved bundles install union of required components. Package-only
dependencies install no runtime components but still export packages. Explicit
distro component allowlists remain authoritative and cannot be widened by a
bundle dependency. Missing siblings/components, cycles, export mismatches, and
component collisions fail before installed tree replacement.

A host-service component contains `service.toml` and an executable entry. Its
descriptor is generic: stable ID, entry, arguments, string environment values,
supported platforms, readiness probe, and shutdown timeout. Installer copies the
component without running bundle-controlled install scripts and records its
artifact hash. Trusted lifecycle commands then materialize immutable releases
outside the replaceable distro tree:

```text
$TABULA_HOME/host-services/<service-id>/releases/<sha256>/
$TABULA_HOME/host-services/<service-id>/current
$TABULA_HOME/host-services/<service-id>/previous
```

Use `tabula-install host-service reconcile` after installing/selecting a distro.
`list`, `status`, `start`, `stop`, `restart`, and `remove [--purge]` expose the
generic lifecycle. macOS defaults to a user launchd adapter. Explicit
`--adapter process` is for development and testbeds; unsupported persistent
platforms fail clearly. Failed readiness and interrupted activation restore the
previous release. Ordinary removal preserves service-owned state; `--purge`
removes it.

Declared Python exports are staged into `$TABULA_HOME/packages/python/src`, and
declared TypeScript exports are staged by package name under
`$TABULA_HOME/packages/typescript/<package-name>`, for example
`$TABULA_HOME/packages/typescript/@tabula/skill-sdk`. Bundle-local `_lib` roots
are ignored by the installer; SDK/shared packages must be declared with
`[[exports.python_packages]]` or `[[exports.typescript_packages]]`.
`requires.sdk` checks and `distro.lock.json` `sdk_versions` are read through the
installed SDK package-surface resolver, which supports Python `__version__` and
TypeScript `package.json` metadata without coupling callers to source layout.

### Source URI grammar

```
local:<path>
git+<url>@<ref>[#path=<subdir>]
source:<alias>[#path=<subdir>]
```

- `local:` paths are resolved relative to the containing `distro.toml`, or may
  be absolute. Always materialized as a **copy** (not a symlink) in transaction
  staging, so installed content does not depend on source checkout mutation.
- `git+` requires an explicit `@ref` (tag, branch, or full/short sha).
  A ref that looks like a hex sha is resolved directly; otherwise it is
  fetched and rev-parsed.
- `#path=<subdir>` narrows to a subdirectory of the repo — useful for
  mono-repos that expose several skills/bundles.
- `source:<alias>` expands through `[sources.<alias>]`. If both the alias and
  the entry include `#path=...`, paths are joined. Use this for multiple bundles
  from one repository so shared `_lib/` roots are installed once instead of from
  mixed local/git checkouts.

### Name resolution and conflicts

Resolution order within a distro (first writer wins):

1. In-tree `skills/<name>/`, `plugins/<name>/`, and `apps/<name>/` directories.
   Host services are bundle-only and compose under `host-services/<name>/`.
2. `[[skills]]` and `[[plugins]]` entries, in declaration order.
3. `[[bundles]]`, in declaration order — each provides skill components under
   `skills/`, plugin components under `plugins/`, app components under
   `apps/`, and host-service components under `host-services/`.

If a later entry collides with an earlier one, installation fails unless the
later entry is marked `override = true`. Silent overwrites are refused on
purpose.

### Overrides for development

A sibling `distro.override.toml` (conventionally gitignored) is merged on top
of `distro.toml`:

- `[distro]` fields are shallow-merged.
- `[sources]` aliases are merged by alias name.
- `[[bundles]]` / `[[skills]]` / `[[plugins]]` entries with the same `name`
  replace the base entry; new entries are appended.

Typical use: flip one repo alias from `git+` to a `local:` checkout:

```toml
[sources.tabula-bundles]
source = "local:/Users/me/src/tabula-bundles"
```

## Lockfile

After every install, `distro.lock.json` is written to the installed distro
root (`$TABULA_HOME/distrib/<name>/distro.lock.json`). Example:

```json
{
  "version": 6,
  "distro": "claw",
  "distro_version": "0.1.0",
  "kernel_version": "0.8.0",
  "generated_at": "2026-04-21T14:30:00Z",
  "bundles": {
    "memory": {
      "source":       "git+https://.../@main",
      "resolved_sha": "abc123…",
      "resolved_ref": "main",
      "version":      "0.1.0",
      "components":   ["memory"],
      "fetched_at":   "2026-04-21T14:30:00Z"
    }
  },
  "skills": {
    "weather": {
      "source":       "git+…@main",
      "resolved_sha": "def456…",
      "resolved_ref": "main",
      "subpath":      "skill"
    }
  },
  "plugins": {
    "mcp": {
      "source":       "git+…@main",
      "resolved_sha": "fedcba…",
      "resolved_ref": "main",
      "subpath":      "plugin"
    }
  },
  "host_services": {
    "evolution-supervisor": {
      "source":          "git+…@main",
      "resolved_sha":    "012345…",
      "artifact_sha256": "89abcdef…",
      "platforms":       ["darwin", "linux"]
    }
  }
}
```

Semantics:

- `distro.toml` expresses **intent** (`@main`, `@v0.2.0`).
- `distro.lock.json` expresses **reality** (pinned sha).
- Plain `tabula-install distro install` prefers pinned shas from the lock, so
  repeated installs are reproducible.
- `tabula-install distro install --update [--update-only NAME ...]` ignores pinned
  shas for the selected entries, re-resolves them, and writes a new lock.
- `tabula-install distro install --frozen` forbids any network access: the lock
  must fully describe the distro, otherwise the install fails.
- Lock v6 records host-service artifact hashes/platforms in addition to installed
  apps, plugin protocol, SDK versions, and selected bundle components. Older
  locks migrate on read and are rewritten after successful install.

Local sources (`local:…`) are not hashable by design; their lock entry only
records the resolved absolute path.

## Tenant Installation

Install one project-scoped tenant directly from a local path or full Git URI:

```sh
tabula-install tenant install \
  'git+https://github.com/example/distros.git@main#path=code' \
  --id code-my-project \
  --root /path/to/my-project \
  --values /path/to/values.toml
```

This writes `$TABULA_HOME/tenants/<id>/install.lock.json`, links tenant
components to stable installed distro tree, runs distro's optional
`[tenant_contract]` materializer, compiles tenant plugin config, registers
tenant-specific runtime directories, and binds `--root` to installed tenant.
Use `--replace-binding` to replace a conflicting directory binding.

For normal project-local installation, let `tabula-agent` generate the
host-local tenant identity:

```sh
tabula-agent install \
  --distro 'git+https://github.com/example/distros.git@main#path=code' \
  --bind /path/to/my-project \
  --values /path/to/values.toml
```

`tabula-agent install` reuses an exact directory binding only when its tenant
install lock records the same distro source. A different source or invalid lock
requires `--replace-binding`. Generated tenant IDs and bindings remain under
`$TABULA_HOME`; no identity file is written into the project repository.

## Transactional Installed Layout

Installed distro has one stable path:

```text
$TABULA_HOME/distrib/claw/
  apps/
  packages/
  plugins/
  skills/
  templates/
  distro.lock.json
  distro.toml
```

Installer uses private transaction scratch:

```text
$TABULA_HOME/run/install/claw/staging/
$TABULA_HOME/run/install/claw/previous/
$TABULA_HOME/run/install/claw/transaction.json
```

Under distro install lock, installer first recovers any interrupted transaction,
then composes and validates `staging`. Existing installed tree moves to
`previous` only while stable path is replaced and runtime/tenant surfaces are
refreshed. Failure restores `previous`; success removes scratch and touches
`run/reload.touch`.

Installer has no public candidate, promotion, history, pruning, or rollback
lifecycle. External supervisors own release candidates, health, known-good
selection, and rollback policy.

## Git source cache

Git sources are cached under `$TABULA_HOME/cache/git/<url-sha1>/`:

- `repo.git/` — a bare clone, fetched on demand.
- `worktrees/<commit-sha>/` — one worktree per pinned commit, shared across
  distro installs.

`tabula-install distro gc` removes worktrees not referenced by any installed
distro's lockfile.

## CLI summary

```
tabula-install distro install <source> [--frozen] [--update] [--update-only NAME]
tabula-install distro use <name> [--source-root DIR] [--update]
tabula-install distro reinstall [<name>]
tabula-install distro list
tabula-install distro lock [<name>]
tabula-install distro gc

```

`<source>` is the path to a distro source directory (the one containing
`distro.toml`).

## Plugin discovery contract

`$TABULA_HOME/config/kernel.toml` defines kernel transport settings.
`$TABULA_HOME/config/runtime.toml` defines runtime layout. Distro authors should
keep these two surfaces separate:

- `kernel.toml` is installer-owned and should contain only kernel-owned fields
  such as `[kernel].url` and optional `[runtime_wss]` listener settings. It must
  not contain workspace, provider, prompt, plugin, or skill policy.

- Installer writes `plugin_dirs` from installed distro's plugin surface and
  writes kernel transport settings to `kernel.toml`.
- `runtime.toml` may also declare runtime composition between plugin manifest
  kinds, for example `[plugin_kinds.gateway] depends_on = ["driver"]`.
- The running kernel reads `kernel.toml` for its own transport settings and
  `runtime.toml` for runtime layout, and fails fast if either is missing.
- Hot reload happens through `$TABULA_HOME/run/reload.touch`. The installer
  touches it after a successful install; the kernel polls the file and
  rereads `runtime.toml`.

Implications for distro authors:

- Adding or removing a plugin is an installer-time change. Re-run
  `tabula-install <distro>` (or edit `runtime.toml` directly and
  `touch run/reload.touch`).
- Anything layout-shaped that needs to change at runtime should live in
  `runtime.toml`.
