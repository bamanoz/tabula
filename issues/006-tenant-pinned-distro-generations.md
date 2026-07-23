# Привязать tenant к конкретной distro generation

**Type:** AFK
**Status:** completed
**Blocked by:** 005

## What to build

Заменить runtime path `tenant -> distrib/active` на `tenant -> exact distro generation`. Tenant surfaces должны ссылаться на зафиксированные plugins, skills, apps, templates и packages своей generation. Один `TABULA_HOME` должен одновременно обслуживать tenants разных distros.

## Acceptance criteria

- [x] Tenant install lock фиксирует distro source, resolved revision (для Git) и generation.
- [x] Tenant runtime surfaces не зависят от `distrib/active`.
- [x] Установка или обновление одного distro не меняет другой tenant.
- [x] Installer tests покрывают два разных distro одновременно; полный live tool-call testbed остаётся issue 015 после materializer/runtime slices.
