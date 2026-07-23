# Перевести Web projects на tenant installation service

**Type:** AFK
**Status:** completed
**Blocked by:** 007, 009, 015

## What to build

Web Gateway project creation вызывает общий tenant installation service вместо генерации app manifest и запуска app installer. Project остаётся optional bundle-level user-facing view с одним backing tenant.

## Acceptance criteria

- [x] Project create не генерирует app manifest и не ищет app lock.
- [x] Project ↔ tenant остаётся 1:1 для workspace/tool/prompt isolation.
- [x] Project switch bootstrap/join-ит backing tenant.
- [x] Workspace root читается из canonical tenant materialized config.
- [x] Kernel не получает project semantics.
