# Унифицировать managed service и удалить tabula-runner

**Type:** AFK
**Blocked by:** 011
**Status:** Completed

## What to build

Сделать `tabula serve --runtime-mode managed` единственным local stack entrypoint. User service manager владеет kernel, kernel владеет local runtime. Удалить shell/PowerShell runner и дублированные readiness/signal/env paths.

## Acceptance criteria

- [x] launchd/systemd запускают managed kernel/runtime stack.
- [x] Foreground и service mode используют одну topology.
- [x] `tabula-runner` и runner-specific code/docs отсутствуют.
- [x] Readiness подтверждает kernel, runtime, tenant catalog и driver; frontend не является частью stack readiness по ADR 0011.
