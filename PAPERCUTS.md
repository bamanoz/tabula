# Papercuts

## 2026-07-26 18:32 — default

Reading required `tabula-guide` skill from `/Users/mak/.tabula/skills/tabula-guide/SKILL.md` -> `fs_read` denied path even though workspace instructions list `/Users/mak/.tabula/skills` as an additional root. Used shell `sed` as fallback. Possible fix: align fs root allowlist with injected workspace roots or expose skill reader helper.

## 2026-07-26 20:25 — default

Verifying GitHub release metadata -> `gh release view --json isLatest` failed because installed `gh` does not expose `isLatest`. Used `tagName,url,assets,publishedAt` instead. Possible fix: release skill should avoid version-sensitive `gh` JSON fields.
