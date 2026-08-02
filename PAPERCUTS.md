# Papercuts

## 2026-07-26 18:32 — default

Reading required `tabula-guide` skill from `/Users/mak/.tabula/skills/tabula-guide/SKILL.md` -> `fs_read` denied path even though workspace instructions list `/Users/mak/.tabula/skills` as an additional root. Used shell `sed` as fallback. Possible fix: align fs root allowlist with injected workspace roots or expose skill reader helper.

## 2026-07-26 20:25 — default

Verifying GitHub release metadata -> `gh release view --json isLatest` failed because installed `gh` does not expose `isLatest`. Used `tagName,url,assets,publishedAt` instead. Possible fix: release skill should avoid version-sensitive `gh` JSON fields.

## 2026-07-27 08:50 — default

Listing installed hook-permissions files -> `find` command was rewritten through RTK and failed on compound predicates. Used `rtk proxy find ...` as documented. Possible fix: hook should auto-suggest or auto-proxy unsupported `find` forms.

## 2026-07-27 10:35 — default

Checking testbed suite discovery -> `python3 -m tabula_testbed_runner list` failed because package has no `__main__`. Used `tabula-testbed list --source tabula-bundles=/Users/mak/src/tabula-bundles` instead. Possible fix: document CLI entrypoint in testbed help/errors or add `__main__.py`.

## 2026-07-27 13:48 — default

Splitting `internal/tabula/app.go` by fixed line ranges -> generated files missed closing braces when adjacent function boundaries shifted. Fixed manually and ran `gofmt`. Possible fix: use AST-aware split helper or symbol-range export instead of raw line slicing.

## 2026-07-27 14:43 — default

Checking stale env references with `fs_grep` over repo root and `include="*"` -> timed out because search included too much generated/cache surface. Re-ran targeted searches under `internal/`, `docs/`, and `README.md`. Possible fix: prefer targeted roots/includes or have grep ignore generated dirs by default.

## 2026-07-27 14:57 — default

Scanning stale Go code with `staticcheck ./...` -> failed with `internal error in importing "internal/byteorder" (unsupported version: 2)`. Likely staticcheck version lags current Go export data format. Fix: update staticcheck or pin analyzer to repo Go toolchain.

## 2026-07-27 15:15 — default

Reading installed `tabula-guide` skill with `fs_read` at `/Users/mak/.tabula/skills/tabula-guide/SKILL.md` -> denied as outside configured roots even though `/Users/mak/.tabula/skills` is listed in workspace roots. Used shell `sed` fallback. Possible fix: align fs tool allowlist display with actual enforcement.

## 2026-07-27 17:12 — default

Verifying `scripts/test-python.sh unit` directly -> failed `Permission denied` because script lacks executable bit. Used `bash scripts/test-python.sh ...` fallback. Possible fix: chmod tracked script or document bash invocation.

## 2026-07-27 17:49 — externcash

Formatting mixed code/docs after kernel refactor → `gofmt -w CONTEXT.md ...` failed with `illegal character U+0023 '#'`. Keep Markdown out of gofmt command or use targeted Go globs only.

## 2026-07-27 18:34 — externcash

Auditing kernel callers with broad `fs_grep` → result exceeded inline delivery limit and before_tool_result did not rewrite it. Narrower patterns or automatic artifact fallback would avoid failed search output.

## 2026-07-27 18:56 — externcash

Running `bash scripts/test-python.sh unit` under Ghostty → terminfo warning `No entry for terminal type "xterm-ghostty"; using dumb terminal settings.` Tests still passed; environment could set a known TERM for noninteractive test runs.

## 2026-07-29 13:22 — externcash

Updating `/Users/mak/.bashrc` after PATH loss -> `fs_read` denied `/Users/mak/.bashrc` as outside configured roots. Used shell append fallback. Possible fix: expose safe home-dotfile editing root or documented dotfile helper.

## 2026-07-29 11:15 — externcash

Mapping repo files during Ouroboros/Tabula comparison -> bare `rg --files` was rewritten as BSD `grep` and failed with ambiguous `--files`; raw `find -print` was also altered by RTK. Used `rtk proxy` fallback. Possible fix: RTK hook should preserve `rg` identity or auto-proxy unsupported flags.

## 2026-07-29 19:58 — externcash

Running focused Python tests and isolated testbed checks → first unittest invocation imported active `~/.tabula` package instead of workspace source, guessed wrong unittest class name, used wrong `tabula-testbed run --lint` CLI form, and requested unsupported 1200-second foreground timeout. Used explicit `PYTHONPATH`, discovered class name, switched to `tabula-testbed lint`, and retried with 900 seconds. Possible fix: repo test helpers should force workspace imports and docs should show subcommand syntax/tool timeout ceiling.

## 2026-07-29 23:43 — externcash

Delegating read-only lifecycle mapping with an explicit `fs_read`/`fs_grep` whitelist → both subagents reported those tools unavailable and returned blocked summaries. Continued investigation in parent session. Possible fix: subagent tool whitelist metadata and actual child permission surface should agree.

## 2026-07-29 23:52 — externcash

Running complete subagents tests with `python3 -m unittest discover -s subagents -p 'test*.py'` → discovery returned zero tests because nested directories are not importable discovery packages. Used explicit test modules instead. Possible fix: add bundle-level test target or package markers for reliable discovery.

## 2026-07-30 00:55 — externcash

Linting task and scheduling SDK changes with `ruff` → process spawn failed because `ruff` is not installed or unavailable on PATH. Python compilation and tests passed. Possible fix: declare a repo-managed lint environment or document the required lint bootstrap command.

## 2026-07-30 09:32 — externcash

Running `make testbed SUITE=tool-result-store` after updating generated testbed template → runner executed canonical `/Users/mak/src/tabula-distrib/testbed` files, which still contained old manually-seeded artifact layout. Synchronized both copies. Possible fix: add one command or check that updates canonical and generated testbed files together.

## 2026-07-30 00:00 — externcash

Memory Bank fs_list blocked by hook and fs_read rejected configured skill path despite root list → used exec_run for checks. Possible tool permission/root mismatch.

## 2026-07-30 11:00 — externcash

Delegating issue implementation to `4-build-l2-deep` subagents → both exited immediately because hidden phase metadata lacked Intent/Category, despite complete task prompts. Retrying with phase-agnostic agents. Possible fix: presets should infer metadata or surface required fields in spawn schema.

## 2026-07-30 14:01 — externcash

Reading required installed skills → fs_read denied /Users/mak/.tabula/skills despite root listing includes /Users/mak/.tabula/skills. Used shell cat workaround. Possible root matching bug or stale fs allowlist.

## 2026-07-30 11:31 — externcash

Running focused cross-package bundle tests → pytest console entrypoint omitted repository root from `sys.path`, so `subagents` namespace import failed despite running from bundle root. Added `.` explicitly to `PYTHONPATH`; 114 tests passed. Possible fix: provide canonical bundle test target with source paths configured.

## 2026-07-30 12:30 — externcash

Listing testbed suites with workspace `.venv` → `tabula_testbed_runner` was unavailable because testbed runner lives only in `.venv-testbed`. Retried with `.venv-testbed/bin/python`. Possible fix: expose one repository testbed wrapper for list/lint/run commands instead of requiring environment knowledge.

## 2026-07-31 20:58 — externcash

Running continuity unit tests with `python3 -m unittest continuity/test_continuity.py` → unittest treated slash path as module name and failed import. Ran test file directly instead. Possible fix: bundle README or test target should provide canonical invocation.

## 2026-07-31 22:45 — externcash

Running focused `drivers.test_driver_agents` tests directly → import failed because `tabula_plugin_sdk` was absent from `PYTHONPATH`. Include `extensions/plugin-sdk/sdk/python/src` and collaboration session SDK paths or provide a canonical driver test target.

## 2026-07-31 23:10 — externcash

Running a new bundle unit test with source SDK paths in `PYTHONPATH` → plugin bootstrap skipped paths already present, then inserted active `$TABULA_HOME/packages` ahead of them and imported stale installed SDKs. Bootstrap must remove and reinsert component SDK paths in explicit priority order.

## 2026-07-31 23:24 — externcash

Starting reflection testbed with a 1200-second foreground timeout → exec tool rejected values above 900 seconds. Retried with the supported 900-second limit; long suites should use background execution when they need more.

## 2026-07-31 21:10 — externcash

Inspecting retained testbed logs with `find -maxdepth ... -print` → RTK hook rewrote command and ignored `-print`. Retried with `rtk proxy find`. Possible fix: hook should preserve unsupported `find` flags or emit exact proxy guidance.

## 2026-08-01 10:58 — externcash

Running `make testbed SUITE=multi-distro-tenants` with local bundles override → suite failed before tests because Make target did not pass required `tabula-distrib` source alias. Retried through `TESTBED_EXTRA_ARGS="--source tabula-distrib=..."`. Possible fix: Make target should always pass local `tabula-distrib` when canonical suite declares it.

## 2026-08-01 11:13 — externcash

Searching three repositories with `rg --hidden` → RTK hook rewrote command to BSD `grep`, which rejected ripgrep flags. Retried with `rtk proxy rg`. Possible fix: hook should preserve `rg` identity or proxy commands using unsupported flags.

## 2026-08-01 11:48 — externcash

Checking several untracked Markdown files with a shell loop around `git diff --no-index --check` → wrapper exited with code 3 even though an individual check returned expected diff code 1. Used direct whitespace searches instead. Possible fix: provide a repository Markdown/check target that includes untracked planning files.

## 2026-08-01 12:05 — externcash

Running focused `tabula-distro` tests and installed host-service testbed → plain unittest imported active `$HOME/.tabula` package instead of checkout, while testbed home had no `$TABULA_HOME/bin/tabula-install` wrapper. Used `PYTHONPATH=src` for unit tests and isolated venv `python -m tabula_distro.cli` in testbed. Possible fix: canonical test helpers should force checkout imports and expose installed tool entrypoint location to suites.

## 2026-08-01 13:09 — externcash

Running focused plugin SDK and evolution tests with one partial `PYTHONPATH` → imports failed for sibling exported SDKs. Retried with every declared checkout SDK path. Possible fix: provide bundle-level test targets that materialize component dependency paths consistently.

## 2026-08-01 14:57 — externcash

Running final evolution checks from core repo → copied summary included repo-name-prefixed relative path and a nonexistent `make testbed-lint` target. Resolved actual path and runner CLI from checkout. Possible fix: record verification commands as absolute paths or canonical Make targets.

## 2026-08-01 15:29 — externcash

Preparing isolated dev agent with `make agent dev prepare` → fresh install failed because `scripts/install-dev.sh` unconditionally copies missing source `config/global.toml`. Failed run had already created target config directory, so retry safely skipped seeding. Possible fix: seed from an existing example/default or tolerate absent optional source config.

## 2026-08-01 16:24 — externcash

Running package-level `pytest` from tool directories → repository-root pytest discovery executed installed testbed scripts as unit tests, created runtime-layout directories in the source tree, and imported active installed packages unless `PYTHONPATH=src` was explicit. Used exact unit-test paths plus checkout `PYTHONPATH`, then removed generated directories. Possible fix: tool-local pytest configuration or canonical Make targets should exclude testbed templates and force checkout imports.

## 2026-08-01 21:12 — externcash

Running bundle-owned `gateway-web-browser` suite with `--testbed-dir gateways/tests` → runner expected `generate.py` inside manifest directory and failed before runtime startup. Retried with canonical `tabula-distrib/testbed` directory while keeping bundle suite source. Possible fix: distinguish manifest roots from generator/testbed directory in CLI help or discover generator independently.

## 2026-08-01 22:53 — externcash

Rematerializing isolated dev tenant with `make agent dev prepare` → command stopped the running dev kernel despite `prepare` sounding like install-only, and immediate `make agent dev run` timed out even though kernel and gateway became ready seconds later. Possible fix: separate non-disruptive update from stop/install, or make readiness timeout match full multi-tenant cold-worker priming.

## 2026-08-01 23:48 — externcash

Running plugin SDK test discovery with only its own source on `PYTHONPATH` → `test_contract.py` could not import sibling `tabula_drivers`. Retried with driver SDK source added. Possible fix: provide canonical bundle test target that assembles exported SDK dependency paths.

## 2026-08-02 12:21 — default

Committing with `vcs_commit` using documented `repository` path → tool rejected call with `vcs.repositories is required`, despite path matching configured workspace. Used non-interactive `git add` and `git commit` instead; tool configuration or error should expose accepted repository IDs.
