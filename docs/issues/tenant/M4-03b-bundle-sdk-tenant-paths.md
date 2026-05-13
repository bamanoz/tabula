# M4-03b — Bundle SDK: tenant-aware state paths

Status: not-started
Phase: M4
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
Companion to: `M4-03` (kernel-side plumbing)
Amendment: AMENDMENTS.md C5, C1

## What to build

Update `tabula_plugin_sdk` and `tabula_skill_sdk` so
`plugin_state_dir()` / `skill_state_dir()` resolve under the
per-tenant tree.

Components:

- `tabula_plugin_sdk.paths.plugin_state_dir(plugin_id) -> Path`:
  - Reads `TABULA_TENANT_DIR` env var (set by runtime per
    M4-05).
  - Returns `$TABULA_TENANT_DIR/state/plugins/<plugin_id>/`.
  - Creates the directory if missing (`exist_ok=True`).
  - Per AMENDMENTS C1: missing/empty `TABULA_TENANT_DIR` →
    raise `RuntimeError("TABULA_TENANT_DIR not set; refusing
    to run without tenant context")`. Worker exits 1; harness
    surfaces `internal_error`. NO fallback to global path.
- `tabula_skill_sdk.paths.skill_state_dir(skill_name) -> Path`:
  same shape, under `state/skills/<skill_name>/`.
- Same for `cache_dir` if those helpers exist.
- Update SDK tests: assert env-missing case raises; assert
  tenant-set case returns correct path.

## Acceptance criteria

- [ ] Both SDKs return tenant-scoped paths when env set.
- [ ] Both SDKs raise / refuse when env missing.
- [ ] No fallback path code survives (grep clean).
- [ ] Tests cover both paths.
- [ ] SDK version bumped (minor).

## Blocked by

- M4-03 (kernel sets `TABULA_TENANT_DIR` correctly)
- M2-04 (plugin SDK base)
- M3-05 (skill SDK base)

## Notes

- This change is breaking for any plugin/skill that called
  `plugin_state_dir()` outside a worker context. Per
  `tabula-bundles/AGENTS.md` no-legacy: don't add a
  compatibility branch. Audit callers; fix in place.
- AMENDMENTS C1 is the source of truth for the
  refuse-on-missing decision.
