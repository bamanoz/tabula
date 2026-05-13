# AM-08 — Agent manifest testbed and documentation

Status: proposed
Type: AFK
Repo: tabula, tabula-distrib, tabula-bundles
Labels: needs-triage, area/testbed, area/docs, area/installer, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add installed-layout coverage and user-facing docs for application manifests,
bindings, and `claw` application values.

Testbed scenarios:

- Apply a repo-local `claw` app manifest.
- Verify app/tenant creation and app lock output.
- Verify directory binding selection.
- Verify managed kernel/runtime startup from manifest topology.
- Start `tabula serve`, launch `tabula-cli --expected-distro-id tabula.claw` from the bound directory, and
  assert the joined session uses the app tenant id.
- Verify `fs` and `exec` resolve `${project_root}` from app/tenant config.
- Verify memory defaults to app-local tenant state.
- Verify a shared memory manifest can point two app tenants at the same
  configured memory path via local override.
- Verify app-local state is isolated between two apps.
- After AM-07, verify two apps can expose different tool catalogs.

Documentation:

- Explain distro/application/binding terminology.
- Document that distro owns prompt semantics and application values.
- Document that committed manifests include non-sensitive launch topology:
  kernel, runtimes, bindings, and distro values.
- Document that the manifest does not contain `instructions`, `features`, or
  `system_prompt`.
- Document local overrides for machine-specific runtime values such as SSH host
  or external kernel URL.
- Document `claw` application values with examples.
- Document `claw` memory modes: project-local by default, shared only by
  explicit value/local override.
- Document binding resolution order and explicit app override.
- Document secret handling: environment/local store/component schemas, never raw
  secret values in lockfiles.

## Acceptance criteria

- [ ] Focused installer tests cover manifest parse, lock, audit, and binding
      registry.
- [ ] Testbed suite covers repo-local `claw` app apply and launch.
- [ ] Testbed suite covers two app instances with isolated tenant state.
- [ ] Docs include a minimal and full `claw` `tabula.app.toml`.
- [ ] Docs include a personal/default `claw` example.
- [ ] Docs state that application scope semantics are distro-owned.

## Blocked by

- AM-01 through AM-07 depending on coverage depth.

## Notes

- Installed-layout tests are required. Unit tests alone are not enough for
  app-scoped distro surface and launcher binding behavior.
