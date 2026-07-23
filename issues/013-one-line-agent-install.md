# Добавить one-line agent install

**Type:** AFK
**Blocked by:** 010, 011, 012
**Status:** Completed

## What to build

Расширить release bootstrapper: после установки core он вызывает `tabula-agent install` с обязательным full distro source и устанавливает/запускает user service.

## Acceptance criteria

- [x] One-liner принимает `--distro SOURCE` и directory binding по текущему cwd.
- [x] Поддержаны `--default`, `--tenant`, `--replace-binding`, `--no-start`, `--non-interactive`.
- [x] Повторный запуск идемпотентен.
- [x] После установки следующая команда только `tabula-agent`.
