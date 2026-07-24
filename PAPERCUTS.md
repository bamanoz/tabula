## 2026-07-23 18:33 — openai/gpt-5.6-sol

Trying to stop test dev kernel via recalled `service_runtime.stop_managed_kernel` → function no longer exists in current source. Use targeted process lookup by `$TABULA_HOME/bin/tabula serve` until a supported `tabula-agent stop` command exists.

## 2026-07-23 18:28 — openai/gpt-5.6-sol

Trying to bound an interactive `tabula-agent` smoke with GNU `timeout` on macOS → command is unavailable. Use background process tooling with explicit termination, or `gtimeout` when coreutils is installed.

## 2026-07-23 16:10 — openai/gpt-5.6-sol

Running `make -n agent-prepare` to inspect syntax → GNU Make executed full recipe because shell line contains recursive `$(MAKE)`, then collided with stale pre-ADR tenant directory. Do not use dry-run for recipes containing recursive Make; inspect recipe or test against isolated `AGENT_HOME`.

## 2026-07-23 16:06 — openai/gpt-5.6-sol

Compiling core and sibling distro Python paths in one command from `tabula-distrib` → core glob resolved nowhere. Run repository-local compile commands separately; shell globs are cwd-relative.

## 2026-07-23 15:49 — openai/gpt-5.6-sol

Running broad `rg` with a shell single-quoted regex containing a literal apostrophe → shell terminated the pattern and all parallel searches failed. Splitting into multiple `-e` patterns then hit an RTK rewrite to BSD `grep`, which rejected `--hidden`. Use `fs_grep` or `rtk proxy rg` for raw ripgrep behavior.

## 2026-07-23 23:12 — openai/gpt-5.6-sol

Using Chrome DevTools `/json/new` with `GET` for headless browser smoke → Chrome returned plain text `Using unsafe HTTP verb GET...`; endpoint now requires `PUT`. Use `PUT /json/new?<url>`.

## 2026-07-23 23:12 — openai/gpt-5.6-sol

Appending a papercut via `fs_write` after reading only first lines → overwrote untracked `PAPERCUTS.md` content beyond preview. Use `fs_read` full file or shell append for append-only notes; avoid `fs_write` unless replacing known full content.

## 2026-07-24 00:34 — externcash/gpt-5.5

Running focused `caveman/tests` from `tabula-bundles` → tests need both core `tools/tabula-testbed/src` and local `caveman/tests` on `PYTHONPATH`. Use `PYTHONPATH="/Users/mak/src/tabula/tools/tabula-testbed/src:caveman/tests"` for these focused tests.

## 2026-07-24 00:53 — externcash/gpt-5.5

Verifying a GitHub release with `gh release view --json isLatest` → this installed `gh` version does not support `isLatest` even though release creation accepts `--latest`. Use supported fields such as `tagName,targetCommitish,publishedAt,url,assets`.

## 2026-07-24 09:08 — externcash/gpt-5.5

Publishing a Tabula patch release after `make push` → commits pushed but the new release tag remained local, while direct `git push origin <tag>` is policy-denied. Add a repo-sanctioned tag push target or make `make push` include release tags.

## 2026-07-24 09:28 — externcash/gpt-5.5

Probing live kernel internals with Python `urllib.request.urlopen('http://127.0.0.1:8089/...')` → got `ConnectionRefusedError` even while `lsof` showed PID 74683 listening on 127.0.0.1:8089. Use a no-proxy opener for loopback diagnostics, matching the distro runtime fix.

## 2026-07-24 12:12 — externcash/gpt-5.5

Running a live gateway WebSocket smoke with system `python3` → import failed with `ModuleNotFoundError: No module named 'websocket'` even though the installed Tabula runtime has its own dependency environment. Use `$TABULA_HOME/venv/bin/python` or a helper under the installed runtime environment for websocket diagnostics.

## 2026-07-24 12:49 — externcash/gpt-5.5

Running `tabula-bundles/gateways/tests` directly → imports failed with `ModuleNotFoundError: No module named 'tabula_testbed'`. Include core `/Users/mak/src/tabula/tools/tabula-testbed/src` on `PYTHONPATH` for bundle installed-smoke tests.

## 2026-07-24 13:31 — externcash/gpt-5.5

Running the broader `tools.tabula-distro.tests.test_runtime_config` suite while validating unrelated readiness edits → existing `test_rewrite_preserves_user_comments_and_unknown_sections` failed because leading comments were not preserved. Keep readiness validation narrowed unless fixing runtime config TOML trivia preservation.
