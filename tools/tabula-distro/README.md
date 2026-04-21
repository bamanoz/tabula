# tabula-distro

Installer and composer for Tabula distributions.

A Tabula distribution (`distrib/<name>/`) is a directory of skills, templates and
a boot script. This tool resolves the distro's declared sources (local paths or
git repositories), composes them into a single staged tree, and atomically
switches `$TABULA_HOME` to point at it.

The installer is intentionally separate from the kernel binary: it only manages
files on disk under `$TABULA_HOME`. The kernel does not need to know about
sources, locks, or generations — at runtime it only sees the materialized
`distrib/active/` symlink.

## Quick start

```sh
pip install -e tools/tabula-distro
tabula-distro install distrib/ouroboros
tabula-distro list
tabula-distro update --frozen   # CI-style: lock must match
tabula-distro rollback ouroboros
```

## Distro config

A distro may declare external sources in `distro.toml` at its root:

```toml
[distro]
name = "ouroboros"

[[bundles]]
name   = "memory"
source = "git+https://github.com/bamanoz/tabula-bundles.git@main#path=memory"

[[bundles]]
name   = "caveman"
source = "git+https://github.com/bamanoz/tabula-bundles.git@main#path=caveman"

[[skills]]
name   = "weather"
source = "git+https://github.com/foo/weather-skill.git@main#path=skill"
```

Source URI grammar:

- `local:<path>` — relative to `distro.toml`, or absolute.
- `git+<url>@<ref>[#path=<subdir>]` — `<ref>` can be tag, branch, or sha.

Local overrides go in `distro.override.toml` (gitignored).

See `docs/distro-config.md` in the main repo for full schema, lockfile format,
and generation/rollback semantics.
