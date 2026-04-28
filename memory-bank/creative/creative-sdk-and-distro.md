# Creative Phase: SDK Packaging & Distro Tooling

Status: **FROZEN** 2026-04-26; **SUPERSEDED ADDENDUM** 2026-04-27 for generic `spawn` SDK surface; **CURRENT-CYCLE REFINEMENT** 2026-04-27 for Phase 6 evidence gates.
Scope: Python/TS plugin SDK location and packaging, distro install behaviour, lock format v2, telemetry.

## 1. DECISIONS SUMMARY

| Item | Decision | Source |
|------|----------|--------|
| Python SDK Phase 3 location | `examples/plugin-sdk-python/` (sibling to `examples/plugin-hello/`) | D3.5 / Agent 4 |
| Python SDK Phase 6+ packaging | Bundled wheel in `tabula-bundles/_lib/python/dist/` | D5.4 Path C / Agent 4 |
| TS SDK Phase 6+ packaging | Bundled tarball in `tabula-bundles/_lib/typescript/dist/` | D5.5 Path C / Agent 8 |
| Distro lib install strategy | `pip install --no-index --find-links` for Python; `bun install` against `file:` tarball for TS | D4.6 |
| Lock format migration | Migrate-on-load (option ii) with bump to v2 | D4.11 / Agent 7 |
| Telemetry / metrics | Convention via existing `log` method (Path A) | D2.12 / Agent 4 |
| Generic SDK `spawn` helper | **SUPERSEDED 2026-04-27 — reserved/future only; not in packaged SDK baseline** | `creative-plugin-runtime.md` §13 |

## 2. Python SDK Location (D3.5)

**Phase 3** (reference plugin lands in this repo):
```
examples/
  plugin-sdk-python/      # NEW: single source of truth for Python SDK
    pyproject.toml
    src/tabula_plugin_sdk/
      __init__.py
      protocol.py          # NDJSON framing, message types, PluginProtocolVersion=1
      api.py               # PluginAPI class (register, on, registerTool, send, log; no generic spawn in current packaged baseline)
      _runtime.py          # main loop: read register_request, dispatch
  plugin-hello/
    plugin.toml
    run.py                 # imports `tabula_plugin_sdk` (editable install in venv)
    README.md
```

Local dev workflow: `scripts/test-plugin-hello.sh` creates `.venv`, runs
`pip install -e examples/plugin-sdk-python/`, then runs the integration test
that spawns kernel + plugin-hello.

**Phase 6** (lib relocation): the SDK source moves to
`tabula-bundles/_lib/python/`, with a `dist/` subdirectory containing the
prebuilt wheel `tabula_plugin_sdk-X.Y.Z-py3-none-any.whl`. `examples/`
content in this repo is kept as a thin reference; the canonical SDK
authority moves to bundles.

## 3. TS SDK Packaging (D5.5)

**Phase 6 final shape** (Path C):

```
tabula-bundles/_lib/typescript/
  package.json             # @tabula/skill-sdk source
  src/
    index.ts
    protocol.ts            # PROTOCOL_VERSION=1 (wire), PLUGIN_PROTOCOL_VERSION=1
  dist/
    tabula-skill-sdk-X.Y.Z.tgz   # output of `npm pack`
```

Each consuming plugin's `package.json`:
```json
{
  "dependencies": {
    "@tabula/skill-sdk": "file:../../_lib/typescript/dist/tabula-skill-sdk-X.Y.Z.tgz"
  }
}
```

(The `file:` path is rewritten by `tabula-distro install` to point at the
installed `$TABULA_HOME` location of the wheel; see §4 install strategy.)

`tsconfig.noEmit: true` and `exports → src/*.ts` are preserved (Bun loads
`.ts` directly), so there is no `tsc` build step beyond typecheck.

**`bin/tabula-coder` lazy reinstall** simplifies to:
```sh
if [ ! -f "$GATEWAY_DIR/node_modules/@tabula/skill-sdk/package.json" ]; then
  (cd "$GATEWAY_DIR" && bun install)
fi
```
No symlink lifecycle (Bun installs the tarball normally into `node_modules`).

## 4. Distro Lib Install Strategy (D4.6)

**Decision**: For each runtime, install from bundled offline artifacts. No
network calls during `tabula-distro install`.

### 4.1 Python (Path C)

```python
# tabula-distro install logic, simplified:
wheels_dir = bundle_root / "_lib" / "python" / "dist"
venv_pip(home / ".venv") .install(
    "--no-index", "--find-links", str(wheels_dir),
    "tabula_plugin_sdk"
)
```

Plugin `run.py` then does `from tabula_plugin_sdk import register, run` —
standard Python import resolution, no `PYTHONPATH` overrides anywhere.

### 4.2 TypeScript (Path C)

For each plugin/skill that declares a TS dependency on `@tabula/skill-sdk`:
1. `tabula-distro install` rewrites the `file:` path in the plugin's `package.json` to point at the installed tarball location under `$TABULA_HOME/lib/typescript/dist/`.
2. Runs `bun install --no-cache` inside the plugin dir.

No symlinks. No `_tslib` magic name. `_tslib` directory is removed entirely
in Phase 6.

### 4.3 Migration shell-script touchpoints (D6.4)

After Path C lands, every `PYTHONPATH=skills/_pylib` and every `bun install`
in `_tslib` site is rewritten:

| File | Old | New |
|------|-----|-----|
| `scripts/install-dev.sh` | `cd skills/_tslib && bun install` | (removed; bundles installer handles it) |
| `scripts/install-coder.sh:28-38` | bun install in `_tslib` + symlink | `bun install` in `gateway-tui` against bundled tarball |
| `bin/tabula-coder:114-131` | symlink existence check | tarball-installed module check |
| `scripts/test-python.sh:6,34,41` | `PYTHONPATH=skills/_pylib pytest ...` | `pip install -e examples/plugin-sdk-python && pytest examples/plugin-sdk-python/tests/` |
| `conftest.py:9`, `pytest.ini:3` | references `skills/_pylib/test_protocol.py` | references `examples/plugin-sdk-python/tests/` (Phase 3) → bundles repo (Phase 6) |

## 5. Lock Format v2 (D4.11)

Bump `LOCK_VERSION = 2` in `tools/tabula-distro/src/tabula_distro/lock.py`.

```python
LOCK_VERSION = 2

@dataclass
class Lock:
    version: int
    distro: str
    bundles: dict[str, LockEntry]
    skills:  dict[str, LockEntry]
    plugins: dict[str, LockEntry]   # NEW in v2
    generated_at: str
    distro_source: str | None
    distro_version: str | None
    kernel_version: str | None
```

`LockEntry` shape unchanged (git/local source ref + resolved sha/path).
Plugin-specific runtime metadata (restart count, registered tool count) lives
in `Hub.SnapshotPlugins()` output, NOT in lock.

### 5.1 Migration: migrate-on-load

```python
@classmethod
def from_json(cls, data: dict) -> "Lock":
    version = data.get("version")
    if version == LOCK_VERSION:
        return cls(**data)
    if version == 1:
        return cls._migrate_v1_to_v2(data)
    raise LockError(f"unsupported lock version: {version}")

@classmethod
def _migrate_v1_to_v2(cls, data: dict) -> "Lock":
    data = dict(data)
    data["version"] = 2
    data.setdefault("plugins", {})
    return cls(**data)
```

Next `lock.save(...)` writes v2 format. No user action required.

Test: `test_lock_v1_loads_as_v2_with_empty_plugins` in
`tools/tabula-distro/tests/test_install.py`.

## 6. Telemetry / Metrics (D2.12)

**Decision**: Path (a) — metrics ride on the existing `log` JSON-RPC method
via a field convention. No new method added to protocol.

**Convention** (documented in `PLUGIN_AUTHORING.md`):
```json
{
  "method": "log",
  "params": {
    "level": "info",
    "msg": "tool_call_completed",
    "fields": {
      "metric_name": "tool_calls_total",
      "metric_value": 1,
      "metric_kind": "counter",
      "tool": "git_status"
    }
  }
}
```

Aggregator plugins (e.g. future `telemetry-otel`) subscribe to the bus log
stream and re-export. If a concrete volume / latency requirement appears,
upgrade to a dedicated `metric` method in plugin protocol v2 (additive, no
breaking change).

## 7. Generic `spawn` SDK Surface Supersession (2026-04-27)

`creative-plugin-runtime.md` §13 supersedes the older §2 note that listed `spawn` as part of the Python `PluginAPI` baseline.

Current decision:
- Generic `api.spawn(cmd, args)` is **reserved/future only** for this task. It is not part of the normative Python or TypeScript packaged SDK surface.
- Child spawning is subagent-plugin-owned. Any subagent-specific child-auth channel, credential, depth accounting, MaxChildren policy, or spawn event emission must be documented and tested by the subagent plugin evidence row before kernel bridge deletion.
- `TABULA_SPAWN_TOKEN` and TS `tabulaSpawnToken()` are not common SDK/runtime helpers. If an external subagent plugin temporarily retains the literal env var, it must be scoped to subagent-private child auth and must not appear in common SDK docs/tests.

Packaging implication:
- The minimal packaged SDK baseline is register/register_request handling, tool registration/calls, event subscription/reply, send, log, update_tools, and shutdown.
- Future addition of a stable generic spawn helper requires a separate design accepting process-group, replay/expiry, parent/session binding, depth/MaxChildren, cleanup, cancellation, and fail-closed hook/security test obligations.

## 8. ACCEPTANCE CRITERIA

- [ ] `examples/plugin-sdk-python/` package created with `pyproject.toml` + minimal API matching the current plugin protocol baseline; no generic `spawn` helper unless a later design supersedes §7.
- [ ] `examples/plugin-hello/` consumes SDK via editable install; smoke test passes.
- [ ] `scripts/test-plugin-hello.sh` runs E2E against built kernel.
- [ ] `lock.py`: bump to v2, `_migrate_v1_to_v2` implemented, `test_lock_v1_loads_as_v2_with_empty_plugins` passes.
- [ ] `PLUGIN_AUTHORING.md` documents metric-via-log convention.
- [ ] Phase 6: shell scripts rewritten per §4.3 table; `_pylib`/`_tslib` directories removed; `git grep -E "skills/_p(ylib|tslib)"` returns empty.

## Rubric Review

```yaml
Rubric Review:
  rubric: rubric-devex.md
  dimensions:
    install_simplicity: 8
    offline_friendliness: 9
    upgrade_path: 9
    discoverability: 7
    cross_lang_consistency: 9
  ai_slop_flags: none
  verdict: PASS
  notes: Symmetry between Python and TS via Path C eliminates two ad-hoc install patterns. Migrate-on-load lock prevents user breakage. Telemetry-via-log avoids premature protocol expansion.
```

## 9. Current-Cycle Phase 6 SDK/Lib Relocation Gate Refinement (2026-04-27)

### 9.1 PROBLEM DEFINITION

- What needs to be designed: a blocker-preserving decision for Phase 6 physical removal of in-repo `skills/_pylib`, `skills/_tslib`, distro `_pylib`/`_tslib` preserve behavior, and inline sibling `_*` support-dir compatibility during the reopened follow-up cycle.
- Constraints:
  - External package artifacts are not present in the canonical matrix as of this CREATIVE pass.
  - Do not remove compatibility bridges based only on target architecture docs or archived repo-local cleanup.
  - Temporary SDK surfaces may already be cleaned so they do not advertise generic spawn or non-empty default kernel tools; that is not proof that physical relocation is safe.
  - Distro install must remain atomic and must not break mixed skill/plugin bundles until bundled `_lib` install paths are validated.
- Success criteria:
  - BUILD knows exactly when SDK/lib deletion is allowed and what to do if evidence is missing.
  - SECURITY can distinguish intentional compatibility retention from accidental public SDK promises.
  - External evidence rows specify artifact, command, and consumer contract proof before cleanup.
- Non-functional requirements: offline installability, migration safety, clear developer errors, reproducibility, and docs/test alignment.

### 9.2 OPTIONS

#### Option A: Remove legacy support dirs immediately

- Description: delete `skills/_pylib`, `skills/_tslib`, distro preserve behavior, and inline sibling `_*` compatibility now because active docs already describe packaged SDKs as the target.
- Architecture: repo assumes external bundles provide `_lib` packages without verifying the artifact locations or consumer commands in this cycle.
- Advantages: fastest physical cleanup and fewer legacy greps.
- Disadvantages: no installable wheel/tarball evidence; likely breaks local contract tests, distro compatibility, or external consumers.
- Risk factors: false Phase 6 completion and unrecoverable consumer breakage.

#### Option B: Keep compatibility until package evidence is green (selected)

- Description: retain physical support dirs and distro compatibility until the Python wheel, TypeScript tarball, and bundle `_lib` rows in the matrix are green. Continue allowing safe doc/SDK wording cleanup that does not claim deletion.
- Architecture: temporary support dirs remain compatibility surfaces; canonical future packages live in external `tabula-bundles/_lib/**` once artifact evidence exists.
- Advantages: avoids breaking consumers; aligns with offline packaging constraints; makes deletion mechanically auditable.
- Disadvantages: legacy code remains; grep gates need documented exclusions for compatibility-only surfaces.
- Risk factors: future agents may confuse retained dirs with normative SDK distribution unless docs keep the caveat explicit.

#### Option C: Add in-repo transitional package artifacts

- Description: create local wheel/tarball packages in this repo as temporary stand-ins, then remove support dirs.
- Architecture: repo temporarily becomes a packaging source instead of waiting for `tabula-bundles`.
- Advantages: could provide contract-test artifacts without external repo access.
- Disadvantages: changes ownership model and may diverge from external bundles layout; does not prove actual bundle `_lib` install paths.
- Risk factors: duplicate package authority and confusing migration path.

### 9.3 ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Evidence traceability | 5 | 1 | 5 | 3 |
| Offline install safety | 5 | 1 | 5 | 3 |
| Consumer migration safety | 5 | 1 | 5 | 3 |
| Developer clarity | 4 | 2 | 4 | 3 |
| Scope discipline | 4 | 3 | 5 | 2 |
| Cleanup completeness | 2 | 5 | 2 | 4 |
| **Weighted Total** | | **34** | **109** | **69** |

Scoring scale: 1–5 where 5 is best fit for this follow-up.

### 9.4 DECISION

**Selected: Option B — Keep compatibility until package evidence is green.**

Justification: Phase 6 is specifically about replacing magic support directories with installable package artifacts. Without the actual Python wheel, TypeScript tarball, bundle `_lib` layout, and consumer validation commands, physical deletion would trade visible legacy code for hidden breakage. Keeping compatibility is the safest implementable state while package evidence remains unavailable.

Trade-offs accepted:
- `skills/_pylib`, `skills/_tslib`, distro preserve behavior, and inline sibling `_*` support-dir compatibility remain until external package rows are green.
- BUILD may update docs/tests only where wording is caveated as target-state or compatibility-only.
- Final grep gates must exclude Memory Bank/archive and explicitly compatibility-scoped support-dir code until deletion is actually unblocked.

### 9.5 BUILD GATES FOR PHASE 6 DELETION

BUILD may remove physical SDK/lib compatibility only after all of these are true and recorded in the external matrix:

1. **Python SDK wheel row green**
   - Exact package source path and artifact path, e.g. `_lib/python/dist/tabula_plugin_sdk-*.whl` or linked release artifact.
   - Validation command equivalent to `pip install --no-index --find-links <dist> tabula_plugin_sdk && <contract tests>`.
   - Consumer contract tests prove imports no longer require `skills/_pylib` or `PYTHONPATH=skills/_pylib`.

2. **TypeScript SDK tarball row green**
   - Exact package source path and tarball path/name, e.g. `_lib/typescript/dist/tabula-skill-sdk-*.tgz` or the final package artifact.
   - Validation command equivalent to `bun install <tarball> && <consumer tests>`.
   - Tests prove consumers no longer require `skills/_tslib`, `skills._tslib`, or symlink lifecycle hacks.

3. **Bundle `_lib` install paths row green**
   - External bundle layout proves `_lib/python`, `_lib/typescript`, and final SDK support paths install into `$TABULA_HOME` without magic `skills/_*` copies.
   - Distro tests plus external smoke prove mixed skill/plugin bundles install atomically.
   - Only then remove `_materialize_inline_symlinks` sibling `_*` compatibility and distro `_pylib`/`_tslib` preserve behavior.

4. **Cleanup after evidence, not before**
   - Remove or rewrite active scripts/docs/tests that reference support dirs in the same deletion lane.
   - Replace temporary support-dir contract tests with packaged consumer contract tests.
   - Keep deprecated builtin literals out of `DEFAULT_KERNEL_TOOLS`; if temporary support dirs still exist, they remain compatibility-only.

### 9.6 SECURITY / QA IMPLICATIONS

- SECURITY should review package-artifact provenance and offline install commands if any row changes to green.
- If no rows change, SECURITY should confirm compatibility retention does not re-advertise generic spawn, common `TABULA_SPAWN_TOKEN`, or non-empty default kernel tools.
- QA should treat `python3 -m unittest skills/_pylib/test_protocol.py` and `bun test` in `skills/_tslib` as temporary compatibility regression checks, not evidence of final Phase 6 relocation.

## Rubric Review — 2026-04-27 Current-Cycle Phase 6 Refinement

```yaml
Rubric Review:
  rubric: rubric-devex.md
  dimensions:
    discoverability: 8
    error_message_quality: 8
    onboarding_friction: 8
    tooling_integration: 8
    documentation_alignment: 9
  ai_slop_flags: none
  verdict: PASS
  notes: Selected gate keeps temporary support dirs discoverable as compatibility surfaces while requiring concrete package artifacts and consumer commands before physical deletion.
```
