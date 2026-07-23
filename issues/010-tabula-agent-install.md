# Добавить tabula-agent install

**Type:** AFK
**Status:** completed
**Blocked by:** 007, 009

## What to build

Добавить отдельную generic команду `tabula-agent install --distro SOURCE --bind PATH`. Она создаёт локальный agent instance tenant, применяет distro, записывает binding и подготавливает runtime. Core не содержит distro aliases или catalog.

## Acceptance criteria

- [x] Принимаются полный Git URI и local path.
- [x] Existing binding переиспользует tenant только для той же установки.
- [x] Новый tenant ID генерируется локально и не пишется в repo.
- [x] Binding conflict требует `--replace-binding`.
- [x] Source URI сохраняется, resolved revision фиксируется lock-файлом.
