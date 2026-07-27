# Papercuts

## 2026-07-26 18:32 — default

Reading required `tabula-guide` skill from `/Users/mak/.tabula/skills/tabula-guide/SKILL.md` -> `fs_read` denied path even though workspace instructions list `/Users/mak/.tabula/skills` as an additional root. Used shell `sed` as fallback. Possible fix: align fs root allowlist with injected workspace roots or expose skill reader helper.

## 2026-07-26 20:25 — default

Verifying GitHub release metadata -> `gh release view --json isLatest` failed because installed `gh` does not expose `isLatest`. Used `tagName,url,assets,publishedAt` instead. Possible fix: release skill should avoid version-sensitive `gh` JSON fields.

## 2026-07-27 08:50 — default

Listing installed hook-permissions files -> `find` command was rewritten through RTK and failed on compound predicates. Used `rtk proxy find ...` as documented. Possible fix: hook should auto-suggest or auto-proxy unsupported `find` forms.

## 2026-07-27 10:35 — default

Checking testbed suite discovery -> `python3 -m tabula_testbed_runner list` failed because package has no `__main__`. Used `tabula-testbed list --source tabula-bundles=/Users/mak/src/tabula-bundles` instead. Possible fix: document CLI entrypoint in testbed help/errors or add `__main__.py`.
