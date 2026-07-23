# Добавить optional tabula.agent.toml

**Type:** AFK
**Blocked by:** 010
**Status:** Completed

## What to build

Добавить optional source-controlled project declaration, содержащий только `[distro].source` и distro-owned `[values]`. Файл не является prerequisite для install.

## Acceptance criteria

- [x] `tabula-agent init` создаёт минимальный manifest без generated topology.
- [x] `tabula-agent apply` применяет manifest к backing tenant выбранного binding.
- [x] Manifest не содержит tenant ID, bindings, kernel/runtime topology или secrets.
- [x] Две clones одного manifest получают разные local tenants.
