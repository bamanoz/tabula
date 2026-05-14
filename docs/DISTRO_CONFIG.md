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
# components = ["fs", "exec"]  # optional skill/plugin/client allowlist
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

[[runtime_requirements.executables]]
name = "npx"
required = true
required_for = ["mcp.context7"]
install_hint = "Install Node.js LTS from https://nodejs.org/."
```

A bundle is a directory whose top level holds skill directories (`SKILL.md`) and
plugin directories (`plugin.toml`) on the same level. The `components = [...]`
allowlist on a bundle entry filters by component directory name and applies to
both shapes. The legacy `skills = [...]` spelling is still parsed as an alias
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
  - `components` — bundles only. Optional allowlist of skill/plugin/client component
    subdirectories to include. `skills` is a deprecated alias.
  - `override` — required to replace a pre-existing target with the same name.
- `[[runtime_requirements.executables]]` — external commands the distro expects
  on `PATH`. The installer checks these before staging a generation.
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
```

When present, `[requires].kernel` is enforced just like the distro-level
constraint. Bundles without a `bundle.toml` are accepted as legacy/unversioned
and skip the check (their entry in `distro.lock.json` will have no `version`).
If `[bundle].components` is omitted, `tabula-distro` discovers components by
walking immediate child directories and selecting those with `SKILL.md` or
`plugin.toml` or `client.toml`. If `components` is present, only those relative component paths
are installed; missing entries fail the install instead of being silently
skipped.

### Source URI grammar

```
local:<path>
git+<url>@<ref>[#path=<subdir>]
source:<alias>[#path=<subdir>]
```

- `local:` paths are resolved relative to the containing `distro.toml`, or may
  be absolute. Always materialized as a **copy** (not a symlink) in the
  installed generation, so that generations stay immutable snapshots.
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

1. In-tree `skills/<name>/`, `plugins/<name>/`, and `clients/<name>/` directories.
2. `[[skills]]` and `[[plugins]]` entries, in declaration order.
3. `[[bundles]]`, in declaration order — each provides skill components under
   `skills/`, plugin components under `plugins/`, and client components under
   `clients/`.

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
  "version": 3,
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
- Lock v3 records `plugins`, `clients`, plugin protocol, and SDK versions
  alongside `bundles` and `skills`. Older lockfiles are migrated on load and
  rewritten on the next install.

Local sources (`local:…`) are not hashable by design; their lock entry only
records the resolved absolute path.

## Generations and atomic switch

### Generations layout

```
$TABULA_HOME/distrib/claw/
  generations/
    0001-2026-04-21T10-00-00Z/   # full staged tree
    0002-2026-04-21T14-30-00Z/
  current  -> generations/0002-...
  boot.py  -> current/boot.py
  skills   -> current/skills
  plugins  -> current/plugins
  templates-> current/templates
  distro.lock.json
```

- A staging directory (`<name>.staging`) is built first; on success it is
  renamed into place, then the `current` symlink is atomically swapped.
- `tabula-install distro rollback [name] [--to N]` flips `current` to a previous
  generation without touching the filesystem otherwise.
- Old generations are pruned after install (default: keep 5 + the current).

Existing installs without a `generations/` layout are auto-migrated on first
run: the existing tree is moved into `generations/0001-legacy/`.

## Git source cache

Git sources are cached under `$TABULA_HOME/cache/git/<url-sha1>/`:

- `repo.git/` — a bare clone, fetched on demand.
- `worktrees/<commit-sha>/` — one worktree per pinned commit, shared across
  distros and generations.

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
`boot.py` and optionally `distro.toml`).
