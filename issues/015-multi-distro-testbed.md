# Подтвердить одновременную работу разных distros

**Type:** AFK
**Status:** completed
**Blocked by:** 006, 008, 009, 011

## What to build

Добавить installed testbed: workspace A использует code tenant, workspace B использует claw tenant, оба обслуживаются одним kernel/runtime stack.

## Acceptance criteria

- [x] Выполняется реальный tool call в обоих tenants.
- [x] Plugin catalogs, workspace roots, sessions и plugin state изолированы.
- [x] Reinstall одного distro не меняет второй tenant.
- [x] Frontend selection следует cwd binding.
