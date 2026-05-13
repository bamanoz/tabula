# M5-01 — `fs` plugin: roots-aware filesystem operations

Status: done
Phase: M5
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/bundle, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

A new plugin `workspace/fs` that owns every filesystem tool
the agent uses. Replaces `tabula-bundles/files/files/` skill
with proper roots-based access control.

Per ADR §4 (unified worker model), this is a plugin (warm,
long-lived) — it has shared state per session (open file
handles, search caches), not a per-call skill.

Components:

- New plugin: `tabula-bundles/workspace/fs/`:
  - `plugin.toml` declaring tools: `fs_read`, `fs_write`,
    `fs_edit`, `fs_glob`, `fs_grep`, `fs_list`, `fs_stat`.
    Tool surface mirrors current `files` skill, no new
    capabilities.
  - `scripts/run.py` using `tabula_plugin_sdk` (M2-04).
  - Plugin config schema (`config/plugins/fs/config.toml`):
    ```toml
    roots = ["${project_root}"]                  # required
    deny_globs = ["**/.git/**", "**/node_modules/**"]
    max_read_bytes = 1048576                     # 1 MiB
    follow_symlinks = false
    ```
  - `${project_root}` is a config template variable resolved
    by the SDK at config-load time from per-tenant config (M5-04).
  - Roots: each is an absolute path or template. Plugin
    rejects every operation whose resolved path falls outside
    every root.
  - Path resolution: lexical (clean + check prefix), but
    follow-symlinks=false means symlinked roots are physically
    rejected by default. Configurable per ADR §4 (path
    validation only on macOS — no physical sandbox).
- Tool semantics (kept identical to existing `files` skill
  surface):
  - `fs_read(path, [start_line], [end_line]) -> {content,
    truncated}`.
  - `fs_write(path, content, [overwrite=true]) -> {bytes_written}`.
  - `fs_edit(path, old_string, new_string, [replace_all=false])
    -> {replacements}`.
  - `fs_glob(pattern, [root]) -> {paths[]}`.
  - `fs_grep(pattern, [path], [include]) -> {matches[]}`.
  - `fs_list(path) -> {entries[]}`.
  - `fs_stat(path) -> {kind, size, mtime}`.
- Permission denied → structured error
  `{code: "fs_outside_root", message, requested_path,
  configured_roots[]}`.
- Tests:
  - Unit tests for path validation (lexical normalization,
    `..` traversal, symlink chase).
  - Integration test against a temp directory tree.
  - Testbed coverage exercising each tool through installed
    plugin.
- Documentation: README.md inside the plugin per
  `tabula-bundles/AGENTS.md` "Documentation".

### Path validation rule

Decision: lexical comparison after `filepath.Clean` (Go-style)
or `os.path.normpath` (Python). No `realpath` resolution
unless `follow_symlinks = true`. Rationale: predictable,
testable, no race conditions between check and use.

## Acceptance criteria

- [x] All seven tools implemented and unit-tested.
- [x] Path outside any root → `fs_outside_root` structured
      error.
- [x] `..` traversal blocked (`/root/../etc/passwd` rejected).
- [x] Default `follow_symlinks=false`: symlinked path that
      escapes root rejected.
- [x] `${project_root}` config template resolved per tenant.
- [x] Plugin survives reload (warm worker continues, new
      config picked up on next call after reload).
- [x] Testbed suite green.
- [x] No imports from the old `files` skill surface — fs is
      a standalone plugin.

## Implementation notes

- Added new standalone bundle component `workspace/fs` with plugin id
  `fs`.
- Implemented all seven tools:
  - `fs_read`
  - `fs_write`
  - `fs_edit`
  - `fs_glob`
  - `fs_grep`
  - `fs_list`
  - `fs_stat`
- Added roots-aware lexical path validation with structured
  `fs_outside_root` errors and symlink-path rejection when
  `follow_symlinks=false`.
- Added `tabula_plugin_sdk.ToolError` so plugin tools can return
  structured worker-protocol errors instead of string-only failures.
- Added `workspace` bundle/testbed set and `fs-plugin` installed suite.
- The plugin does not import or reuse the old `files` skill surface.

- `fs-plugin` installed coverage now configures `roots = ["${project_root}"]`
  and sets tenant workspace root via `tabula tenant set` before invoking
  the plugin.
- The installed suite also changes the default tenant workspace root while
  the warm plugin is running and verifies the next call picks up the new
  root without restarting the plugin.

## Validation evidence

- `PYTHONPATH=_lib/python/src:. python3 -m unittest workspace.fs.tests.test_fs_plugin _lib.python.tests.test_contract` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m unittest tools.tabula-testbed.src.tabula_testbed_runner.runner_test`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --bootstrap-check --home /tmp/tabula-m501-fs`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite baseline --suite fs-plugin --bootstrap-check --home /tmp/tabula-m501-baseline-fs`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite fs-plugin --bootstrap-check --home /tmp/tabula-m504-fs-reload`

## Blocked by

- M2-04 (plugin SDK migration done — fs plugin uses it from
  day one)

## Notes

- Roots are configured per-tenant. `${project_root}` is the
  conventional first root; nothing prevents an admin from
  configuring multiple roots (`roots = ["${project_root}",
  "/data/shared"]`). Per `tabula-bundles/AGENTS.md`, the
  plugin must work standalone — no cross-bundle assumptions.
- macOS: validation is lexical only; no sandbox enforcement.
  Documented limitation. Hard isolation = M6 PluginExecPolicy
  (Linux only).
- Atomic writes (write-temp + rename) are the plugin's
  responsibility for `fs_write` and `fs_edit`. Document the
  semantic.
- This issue does NOT delete the old `files` skill — that's
  M5-05 (atomic deletion when nothing references it).
