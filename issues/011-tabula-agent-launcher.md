# Сделать tabula-agent generic tenant stack launcher

**Type:** AFK
**Status:** completed
**Blocked by:** 008, 009, 010

## What to build

`tabula-agent` без subcommand разрешает tenant по cwd и проверяет service readiness. Первоначальное frontend exec поведение отменено ADR 0011: lifecycle клиентов не принадлежит generic launcher.

## Acceptance criteria

- [x] Launcher не знает concrete distro/client IDs.
- [x] `--tenant` предоставляет advanced explicit selection.
- [x] Missing/ambiguous binding выдаёт одно actionable сообщение.
- [x] Selected tenant runtime readiness проверяется без запуска client component.
- [x] `tabula` остаётся kernel/operator binary.
