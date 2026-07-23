# Перевести distros на tenant materialization

**Type:** AFK
**Status:** completed
**Blocked by:** 007

## What to build

Перевести все application-capable distros на tenant-oriented materializer contract. Добавить в `distro.toml` values schema/defaults. Требование default frontend отменено ADR 0011.

## Acceptance criteria

- [x] `code`, `claw`, `code-immune`, `harness-bench` materializers не используют `TABULA_APP_*`.
- [x] Default frontend не требуется; поведение отменено ADR 0011.
- [x] Distro tests и real installed-layout check применяют values к tenant без app manifest.
- [x] Runtime requirements проверяются до materialization.
