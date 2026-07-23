# Заменить app bindings на tenant bindings

**Type:** AFK
**Status:** completed
**Blocked by:** 007

## What to build

Ввести host-local `$TABULA_HOME/bindings.toml` с directory/default → tenant. Удалить app и kernel из binding contract. Один directory binding выбирает один tenant; конфликт завершается ошибкой, замена требует `--replace-binding`.

## Acceptance criteria

- [x] Longest matching directory root выбирает tenant.
- [x] Default binding используется только при отсутствии directory match.
- [x] Повторная запись того же binding идемпотентна.
- [x] Другой tenant требует `--replace-binding`.
- [x] CLI/ACP/Web joins получают tenant ID без app lock.
