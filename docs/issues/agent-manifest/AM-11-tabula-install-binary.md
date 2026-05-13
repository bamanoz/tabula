# AM-11 — `tabula-install` installer binary

Status: done
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/cli, area/packaging

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Introduce the final installer command surface instead of exposing long-term app
application under `tabula-distro`.

Current state:

```bash
tabula-distro app lock ./tabula.app.toml
tabula-distro app audit ./tabula.app.toml
tabula-distro app apply ./tabula.app.toml
tabula-distro app run ./tabula.app.toml
```

Target UX:

```bash
tabula-install distro install <source>
tabula-install app lock ./tabula.app.toml
tabula-install app audit ./tabula.app.toml
tabula-install app apply ./tabula.app.toml
tabula-install app run ./tabula.app.toml
tabula-install app bindings
```

## Acceptance criteria

- [x] `tabula-install` is installed by dev/install scripts and packaged builds.
- [x] Existing `tabula-distro` commands keep working during transition or print a
      clear migration message.
- [x] Docs use `tabula-install` as the primary command.
- [x] `tabula` kernel binary does not grow app apply/install commands.
- [x] Tests cover both command entrypoints during the transition window.

## Blocked by

- AM-01 through AM-08 command behavior.

## Notes

- Keep this as installer tooling. Do not move apply/run into kernel CLI.

## Implementation Notes

- Added the `tabula-install` Python console script as the primary installer
  entrypoint.
- `tabula-install distro ...` dispatches to the existing distro installer
  commands; `tabula-install app ...` uses the runnable app manifest commands.
- `tabula-distro` remains available and keeps the legacy command shape during
  the transition.
