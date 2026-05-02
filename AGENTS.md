# Tabula Core Working Rules

This repository owns the Tabula kernel, distro installer, testbed runner, docs,
and shared development tooling. Distro-specific product policy belongs in
`tabula-distrib`; bundle/plugin/skill implementations belong in
`tabula-bundles`.

## Boundaries

- Keep kernel changes generic. The kernel should not know about concrete
  distros, bundles, skills, plugins, gateways, MCP servers, workspaces, or
  product-specific policy.
- Put distro policy in distro boot code, not in the kernel.
- Put reusable Python runtime helpers in shared bundle libraries when multiple
  distros or components need the same behavior.
- Do not put distro-specific assumptions into this repo's docs or tests unless
  the document is explicitly about distro integration.

## Config And Runtime

- Treat `TABULA_HOME` as runtime/config/state root, not as a user workspace.
- Do not hardcode `~/.tabula` in user-facing text except when documenting the
  default value of `TABULA_HOME`.
- Plugin runtime config uses `config/global.toml` and
  `config/plugins/<plugin-id>/config.toml`; do not add new `plugin.toml`
  runtime config blocks.

## No Legacy / No Backward Compatibility

- Do not preserve legacy code paths, deprecated aliases, or backward-compat
  shims by default. When you rename, restructure, or replace something,
  delete the old surface in the same change and update every caller, test,
  and doc.
- Do not introduce new "legacy alias" constants, fields, methods, env vars,
  config keys, or wire-format aliases. One name per concept.
- Migration shims are allowed only when there is a concrete migration
  requirement (e.g. on-disk lock-file or `TABULA_HOME` layout that already
  exists in user installs). When a shim is required, scope it narrowly,
  document why it exists, and remove it as soon as the migration window
  closes.
- When asked to remove legacy code, remove it everywhere: source, tests,
  docs, comments, and follow-up plan files. Do not leave dangling references.

## Tests

- If a regression requires installed layout to reproduce, add or update a
  testbed suite. Unit tests alone are not enough for install/fan-out bugs.
- Testbed checks should execute the relevant installed tool/plugin, not only
  assert that it appears in the tool catalog.
- Keep canonical testbed files and the generated testbed template in sync.
- Run focused tests first, then the relevant testbed suite. Run baseline when
  touching shared runtime or installer behavior.

## Installer And Artifacts

- Do not commit generated artifacts: `dist/`, `build/`, `*.egg-info/`,
  `__pycache__/`, or `*.pyc`.
- Use `tabula-distro` install behavior as the source of truth for runtime
  layout. If a component needs files at runtime, ensure the installer actually
  fans them out.
- After switching the active generation, the installer atomically updates
  `$TABULA_HOME/run/reload.touch`. A live `tabula serve` polls that file and
  calls `Hub.ReloadPlugins` when its mtime changes, so reinstalls take effect
  without a manual kernel restart. Keep this trigger best-effort: a missing or
  unwritable `run/` directory must not fail an install.

## Collaboration

- Prefer the smallest correct change.
- Preserve user changes in dirty worktrees.
- Do not commit unless explicitly asked.
- When changing architecture, update the central docs and tests that enforce
  the new rule.
- Keep documentation current with behavior. If code changes user-facing
  behavior, runtime layout, config, installation, or testing expectations,
  update the relevant docs in the same change.
