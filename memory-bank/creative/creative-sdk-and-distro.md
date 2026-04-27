# Creative Phase: SDK Packaging & Distro Tooling

Status: **FROZEN** 2026-04-26.
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

## 2. Python SDK Location (D3.5)

**Phase 3** (reference plugin lands in this repo):
```
examples/
  plugin-sdk-python/      # NEW: single source of truth for Python SDK
    pyproject.toml
    src/tabula_plugin_sdk/
      __init__.py
      protocol.py          # NDJSON framing, message types, PluginProtocolVersion=1
      api.py               # PluginAPI class (register, on, registerTool, send, log, spawn)
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

## 7. ACCEPTANCE CRITERIA

- [ ] `examples/plugin-sdk-python/` package created with `pyproject.toml` + minimal API matching design doc §5.1.
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
