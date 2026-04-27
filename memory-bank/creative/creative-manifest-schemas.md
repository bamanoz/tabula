# Creative Phase: Manifest Schemas

Status: **FROZEN** 2026-04-26.
Scope: `plugin.toml`, `bundle.toml`, `SKILL.md` compat changes.

## 1. `plugin.toml` Schema

Top-level fields (TOML):

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `id` | yes | string | Unique plugin id; matches dir name conventionally. `[a-z0-9_-]+`. |
| `name` | yes | string | Human-readable name. |
| `version` | yes | string | SemVer (`X.Y.Z`). |
| `runtime` | yes | string | One of `python`, `node`. Determines launcher selection. |
| `entry` | yes | string | Relative path inside the plugin dir to the entry script. |
| `tags` | no | array of strings | Free-form tags for distro/UI filtering. Kernel ignores. |

Optional sub-tables:

```toml
id = "mcp"
name = "MCP bridge"
version = "0.3.0"
runtime = "python"
entry = "run.py"
tags = ["mcp_bridge"]

[config.schema]
# JSON Schema (object) describing accepted config keys.

[config.defaults]
# Default values, merged with user override before delivery to plugin.

[ui_hints]
# Free-form table; surfaced to TUI/distro UIs. Kernel ignores.
```

**Validation rules**:
- `entry` must not contain `..` or be absolute.
- `runtime` value not in `{python, node}` → `ManifestError`.
- `version` must parse as SemVer.
- `id` must match `^[a-z0-9_-]+$`.
- Unknown top-level keys are tolerated with WARN log (forward-compat for new optional fields).

**No fields**: `kind`, `capabilities`, `requires-kernel-tools` — by design (per design doc §2).

## 2. `bundle.toml` Schema (D4.9 / D5.6)

Extension to existing `BundleManifest` dataclass in
`tools/tabula-distro/src/tabula_distro/manifest.py`.

```toml
[bundle]
id = "base"
version = "0.3.0"
components = [
  "shell",
  "mcp",
  "hook-permissions",
  "cron"
]

[requires]
kernel = ">=0.4.0"
```

**`components` field — flat list of strings** (D4.9 final decision).

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `bundle.id` | yes | string | Bundle identifier. |
| `bundle.version` | yes | string | SemVer. |
| `bundle.components` | no (legacy) | array of strings | Relative paths to component dirs. Each must contain `SKILL.md` (skill) or `plugin.toml` (plugin). |
| `requires.kernel` | no | string | Kernel version constraint (existing). |

**Backward compat**:
- `bundle.toml` entirely absent → existing placeholder `BundleManifest(name=None, ...)` (unchanged).
- `bundle.toml` present but no `components` field → `components = None` in dataclass; `install.py:_install_bundle` falls back to walk-discovery (current behaviour with `_`-prefix special handling REMOVED per D4.7).
- `components = []` (explicit empty list) → empty bundle, valid.
- `components = ["does-not-exist"]` → `BundleInstallError` with clear message (no silent skip).

**Why flat list, not table-of-tables**:
- §7 design doc shows table-of-tables (`[[components]] path = "..."`) but every example has only the `path` field — no per-component overrides exist or are planned.
- Flat list of strings is the minimum surface that fulfils the requirement.
- If per-component overrides are ever needed (config inlining, version pinning, etc.), they can be added later as an alternative form: list items can be either string (path) or `{path = "...", ...}` table — TOML supports heterogeneous arrays via inline tables.

**Dataclass extension** (`manifest.py`):
```python
@dataclass(frozen=True)
class BundleManifest:
    name: str | None
    version: str | None
    requires_kernel: str | None
    path: Path
    components: tuple[str, ...] | None = None  # NEW; None = walk-fallback
```

**Parser change** (~15 LOC):
```python
components_raw = bundle.get("components")
if components_raw is None:
    components = None
elif isinstance(components_raw, list):
    components = tuple(_validate_component_path(c) for c in components_raw)
else:
    raise ManifestError(f"bundle.components must be a list of strings")
```

`_validate_component_path` rejects empty strings, `..`, and absolute paths.

## 3. `SKILL.md` Compat Changes

Per design doc §3.1: `SKILL.md` stays Anthropic-style YAML frontmatter,
fully backward compatible with current Tabula and shared ecosystem.

**Add**:
- `tools[].exec` field (string) — command kernel runs to dispatch this tool.
  - Already documented in design doc; no new schema concern, just install/parser awareness.
  - Format: shell command line; kernel performs `<venv_python>` and similar template substitutions documented in `SKILL_AUTHORING.md`.

**Remove**:
- `requires-kernel-tools` — no longer meaningful (kernel has empty tool catalog after Phase 1). Distro install + boot loader stop reading it; existing entries are silently ignored with WARN log for one release cycle.

**Unchanged**:
- `name`, `description`, `tools[]` (with new `exec` per entry), `user-invocable`.

**Tool entry shape** (now formal):

```yaml
tools:
  - name: git_status
    description: "..."
    params:
      cwd:
        type: string
        description: "..."
    required: []
    exec: "<venv_python> skills/git/run.py tool git_status"
```

`exec` is **required** for every entry in `tools[]` after Phase 1. Validation
in distro install + kernel boot: missing `exec` → `SkillError` with clear
message pointing the author to the migration guide.

## 4. ACCEPTANCE CRITERIA

- [ ] `plugin.toml` parser implemented in `internal/kernel/plugin/manifest.go` with all validation rules from §1.
- [ ] `BundleManifest` extended with `components: tuple[str, ...] | None`; parser validates per §2; tests `test_bundle_toml_with_explicit_components`, `test_bundle_toml_legacy_compat_no_components`, `test_bundle_toml_components_missing_dir_fails` pass.
- [ ] `_install_bundle` rewritten to dispatch on `manifest.components is not None` (explicit list) vs walk-discovery (legacy); `_`-prefix special-case removed.
- [ ] `requires-kernel-tools` removal documented in migration guide D5.1; warning log added for one cycle.
- [ ] Skill `tools[].exec` required-field validation in distro install AND kernel boot.

## Rubric Review

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    surface_minimality: 9
    composability: 8
    invariant_strength: 8
    migration_safety: 9
    debuggability: 8
  ai_slop_flags: none
  verdict: PASS
  notes: Flat components list is minimum-surface; backward compat (None → walk) preserves existing bundles without action; clear errors on negative paths.
```
