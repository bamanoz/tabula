# AM-16 — Real Claw app testbed

Status: done
Type: AFK
Repo: tabula, tabula-distrib
Labels: needs-triage, area/testbed, area/installer, area/claw

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add installed-layout coverage that applies and runs a real Claw app manifest
from `tabula-distrib`.

The test should launch installed `tabula-cli --expected-distro-id tabula.claw`, join with the app tenant id, and
execute real installed tools using config produced by the Claw materializer.

## Acceptance criteria

- [x] Testbed uses real Claw distro source.
- [x] `tabula-install app run ./tabula.app.toml` succeeds.
- [x] Installed `tabula-cli` starts from isolated `$TABULA_HOME/bin`.
- [x] Client joins using `tenant_id = application.id`.
- [x] `fs` and `exec` tools work without the test manually writing plugin config.
- [x] Prompt and boot metadata are app-scoped and Claw-owned.

## Blocked by

- AM-15.

## Notes

- This should replace the current generated-distro/stub-client confidence check
  for product behavior. The generated test distro remains useful for generic
  installer contract coverage.
- Implemented as `claw-app-manifest` installed-layout suite. The suite copies
  local `tabula-distrib`/`tabula-bundles` sources into the generated testbed,
  applies a real Claw app manifest, launches installed `tabula-cli --expected-distro-id tabula.claw --app`, and
  executes real `fs`/`exec` tools as tenant `claw-testbed`.
