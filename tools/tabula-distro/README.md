# tabula-distro

Installer and composer for Tabula distributions.

A Tabula distribution is a directory of skills, templates and a boot script.
Built-in distros live in the sibling `tabula-distrib` repository. This tool
resolves the distro's declared sources (local paths or git repositories),
composes them into a single staged tree, and atomically switches `$TABULA_HOME`
to point at it.

The installer is intentionally separate from the kernel binary: it manages
files and generic external host-service lifecycle under `$TABULA_HOME`. The
kernel does not know about sources, installer transactions, projects, bindings,
or host services. Installed distro content lives at
`$TABULA_HOME/distrib/<name>/`; root and tenant surfaces link to that stable tree.

## Quick start

```sh
pip install -e tools/tabula-distro
tabula-install distro install ../tabula-distrib/claw
tabula-install distro list
tabula-install distro reinstall claw --frozen

tabula-install host-service reconcile
tabula-install host-service list
tabula-install host-service status <service-id>

tabula-agent install \
  --distro ../tabula-distrib/claw \
  --values /path/to/values.toml \
  --no-start

# Advanced explicit tenant identity:
tabula-install tenant install ../tabula-distrib/claw \
  --id claw-my-project \
  --root /path/to/my-project
```

Tenant installation writes `tenants/<id>/install.lock.json`, links component
surfaces to the stable installed distro tree, runs an optional distro-owned
`[tenant_contract]` materializer, and adds tenant-specific runtime directories.
See `docs/TENANT_MATERIALIZER_CONTRACT.md` in the main repo.

Bundles may also contain `service.toml` host-service components. Distro install
pins and copies them under `distrib/<name>/host-services`; explicit
`host-service reconcile` installs immutable releases under
`$TABULA_HOME/host-services`, registers the platform service, and verifies
generic readiness. Upgrade retains the previous executable and rolls back on
failed readiness. `remove` preserves service state; `remove --purge` removes it.
macOS uses a user launchd adapter by default. `--adapter process` is an explicit
development/testbed path.

`tabula-agent install` binds current directory by default and starts/checks the
managed local service. Use `--bind <path>` to select another directory,
`--default` for fallback selection, `--tenant <id>` for explicit local identity,
`--replace-binding` for intentional replacement, and `--no-start` to materialize
without service startup. `--non-interactive` is accepted by automation and
requires all mandatory input on command line. Repeating same source and binding
reuses existing tenant.

Run `tabula-agent` from a bound project directory. Longest directory binding
wins; use `tabula-agent --tenant <id>` for explicit selection. It checks kernel
and tenant runtime readiness, then exits. Gateway and frontend lifecycle belongs
to installed components and their plugins; `tabula-agent` does not require or
execute a frontend.

## Optional project declaration

`tabula.agent.toml` is optional. Create minimal declaration:

```sh
tabula-agent init --distro 'git+https://github.com/owner/distros.git@main#path=my-distro'
```

File contains only distro source and distro-owned values:

```toml
[distro]
source = "git+https://github.com/owner/distros.git@main#path=my-distro"

[values]
mode = "strict"
```

Apply it with `tabula-agent apply`; `--no-start`, `--update`, `--frozen`, and
`--replace-binding` are supported. Existing same-source backing tenant is
rematerialized. Unbound clones create distinct local tenants. Tenant IDs,
bindings, secrets, kernel URLs, and runtime topology are rejected from project
declaration.

```toml
[tenant_contract]
materializer = "python3 tenant/apply.py"
```

## Distro config

A distro may declare external sources in `distro.toml` at its root:

```toml
[distro]
id = "tabula.claw"
name = "claw"

[[bundles]]
name   = "mempalace"
source = "git+https://github.com/bamanoz/tabula-bundles.git@main#path=mempalace"

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

See `docs/DISTRO_CONFIG.md` in the main repo for full schema, lockfile format,
and transactional replacement semantics.
