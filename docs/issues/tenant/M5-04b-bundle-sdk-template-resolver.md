# M5-04b — Bundle SDK: config template resolver

Status: done
Phase: M5
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
Companion to: `M5-04` (kernel-side workspace config)
Amendment: AMENDMENTS.md C5, C7, M12

## What to build

The Python implementation of the `${var}` template resolver
used by `tabula_plugin_sdk.config.load_plugin_config()`. The
Go side (skill `exec` template) is implemented in
M3-02..04's harnesses; this slice ships the Python side and
the shared variable registry contract.

Components:

- `tabula_plugin_sdk.config.expand_templates(value, ctx)`:
  - Walks dicts/lists/strings, expands every `${var}` against
    `ctx`.
  - Unknown variable → raises with structured error,
    aborting config load.
- Variables (per AMENDMENTS C7 staging):
  - `${tabula_home}` — `$TABULA_HOME`. Available since M2-04.
  - `${tenant_id}` — `$TABULA_TENANT_ID`. Available since
    M4-03b.
  - `${project_root}` — `[workspace] project_root` from
    tenant.toml. **Introduced in this slice.**
- Reading workspace config:
  - SDK loads `$TABULA_TENANT_DIR/tenant.toml` once at startup
    and caches the `[workspace]` section.
  - Missing `[workspace] project_root` → `${project_root}`
    expansion fails with structured error
    `template_var_unresolved`. Document.
- Cross-language conformance:
  - Shared test fixture corpus at
    `tabula-bundles/_lib/_shared/template_test_corpus.json`
    with input/expected pairs.
  - Python tests parse and assert against this corpus.
  - Go-side harness tests (M3-02..04) parse the same corpus
    via test data import.
  - CI in both repos asserts no drift.

## Acceptance criteria

- [x] All three variables expand correctly in nested
      dict/list/string structures.
- [x] Unknown variable raises `template_var_unresolved`.
- [x] Missing `[workspace] project_root` for `${project_root}`
      raises structured error.
- [x] Shared corpus exists and passes both Python and Go
      tests.
- [x] Existing plugin configs that don't use templates pass
      through unchanged.

## Implementation notes

- Added `tabula_plugin_sdk.config.expand_templates(value, ctx)`.
- `load_plugin_config()` now expands `${tabula_home}`,
  `${tenant_id}`, and `${project_root}` after all config layers merge.
- `${project_root}` is read from
  `$TABULA_TENANT_DIR/config/tenant.toml` `[workspace] project_root`.
- Unknown or unavailable variables raise `SkillConfigError` containing
  `template_var_unresolved: <name>`.

- Added shared corpus at
  `tabula-bundles/_lib/_shared/template_test_corpus.json`.
- Python SDK tests consume the `plugin_config` corpus cases.
- Go bash harness tests consume the `skill_exec` corpus cases for the
  existing cold-skill exec-template surface (`${SKILL_DIR}` and
  `${TABULA_HOME}`). Plugin config resolution remains intentionally in the
  Python SDK consumer path.

## Validation evidence

- `PYTHONPATH=_lib/python/src:. python3 -m unittest _lib.python.tests.test_contract workspace.fs.tests.test_fs_plugin` in `../tabula-bundles`
- `go test ./internal/runtime/host/harness/bash`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --bootstrap-check --home /tmp/tabula-m504-fs-reload`

## Blocked by

- M2-04 (resolver scaffolding for `${tabula_home}` /
  `${tenant_id}`)
- M4-03b (`${tenant_id}` resolves against real tenant)
- M5-04 (kernel-side `[workspace] project_root` schema lands)

## Notes

- AMENDMENTS C7 inverted the dependency: M5-04 (and this slice
  M5-04b) **block** M5-01/M5-02, not the other way around.
  Update `M5-01` / `M5-02` "Blocked by" lists accordingly when
  applying amendments to those issues during implementation.
- Single source of truth for the variable list: this issue.
  When adding a new variable later, extend both Python and
  Go implementations + the shared corpus in the same change.
