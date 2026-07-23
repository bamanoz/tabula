# Добавить tenant materialization contract

**Type:** AFK
**Status:** completed
**Blocked by:** 005, 006

## What to build

Добавить low-level tenant install/materialize operation без app manifest. Installer создаёт tenant metadata, values и install lock, применяет выбранную distro generation и вызывает distro-owned materializer с tenant-oriented contract.

## Acceptance criteria

- [x] Tenant устанавливается из Git URI или local path без `tabula.app.toml`.
- [x] Materializer получает `TABULA_TENANT_*`, exact distro generation, project root, values file и install lock.
- [x] Installer владеет source resolution, generation, lock, runtime config и effective plugin config.
- [x] Materializer не пишет global kernel/runtime config.
