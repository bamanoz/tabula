# Сделать exchange continuations полностью generic и schema-driven

**Type:** AFK
**Blocked by:** None - can start immediately

## What to build

Довести текущую архитектуру exchange до долгосрочной модели, где kernel остаётся полностью generic router/continuation coordinator, а все product-specific UX и policy semantics живут в bundles/gateways.

Текущий фикс уже убрал approval-specific resolution из production kernel path: kernel хранит suspended exchange, redeliver'ит его при join и прокидывает raw reply обратно в hook pipeline. Следующий шаг — формализовать это как стабильный protocol/runtime contract, чтобы новые exchange сценарии не добавляли новые специальные знания в kernel.

Нужно заменить неявную связку `tool_call -> before_tool_call suspend -> exchange.approve/exchange.choose -> resume` на explicit generic continuation model:

- kernel хранит `pendingContinuation`, а не tool/approval-specific состояние;
- exchange topic/kind/schema являются opaque для kernel;
- responder lease явно привязан к tenant/session/client membership;
- reply возобновляет continuation через documented hook continuation contract;
- gateway рендерит exchange по schema/render hints, а не по hardcoded product topic;
- approval/question остаются bundle-provided schemas and handlers.

## Acceptance criteria

- [ ] В kernel введён generic continuation contract для suspended work: continuation ID, tenant/session, requester, exchange envelope, resume target/strategy и lifecycle state.
- [ ] Tool call является только одним пользователем continuation механизма; kernel не содержит tool approval/question-specific веток для suspend/resume.
- [ ] Kernel route logic работает с `exchange.*` как opaque topic/capability matching; kernel не inspect'ит поля вроде `kind`, `choice`, `approved`, `questions`, `options`.
- [ ] Exchange envelope документирован как protocol-level wrapper: `exchange_id`, opaque topic, optional `schema`, optional `payload`, optional render hints/capabilities.
- [ ] `exchange.approve` и `exchange.choose` либо становятся bundle-defined opaque topics, либо заменяются одним generic `exchange.request` с schema внутри envelope.
- [ ] Responder ownership оформлен как lease: reply принимается только от клиента, который всё ещё joined в исходные tenant/session и владеет active lease.
- [ ] При project/session switch lease invalidates deterministically; stale reply получает generic protocol error без product outcome.
- [ ] Pending suspended continuations redeliver'ятся при join/reconnect eligible responder'а.
- [ ] Pending continuations persist across kernel restart или documented как intentionally in-memory with follow-up ADR; если выбирается persistence, восстановление проверено installed testbed'ом.
- [ ] Hook continuation contract явно описывает, как raw exchange reply попадает обратно в hook/plugin pipeline, без специальных approval hooks.
- [ ] `hook-approvals` реализует approval policy полностью вне kernel: allow once, deny once, allow always, deny always, remembered rules, denied result shape.
- [ ] `question` реализует question/answer exchange полностью вне kernel: request schema, reply normalization, tool input rewrite.
- [ ] `gateway-web` рендерит exchange forms по schema/render hints и не требует hardcoded kernel approval semantics.
- [ ] Protocol docs and `tabula-guide` фиксируют правило: kernel may route and store exchange continuations, but must not interpret product-specific exchange payload fields.
- [ ] Добавлены focused Go tests на continuation lease invalidation, redelivery, restart behavior decision, stale reply rejection and generic exchange routing.
- [ ] Добавлены bundle/gateway tests, доказывающие approval/question behavior end-to-end без kernel-specific approval semantics.
- [ ] Installed testbed выполняет хотя бы один approval exchange и один question exchange через установленный runtime/gateway path.

## Blocked by

None - can start immediately

## Explicit non-goals

- Менять пользовательскую approval UX без необходимости.
- Возвращать legacy aliases вроде `approval_id`.
- Добавлять product policy в `internal/kernel`.
- Делать kernel-aware project entities или project-specific routing semantics.
- Поддерживать backward-compatible deprecated exchange payload names без отдельного migration requirement.

## Notes

Это продолжение после исправления бага, где UI переключался в другой project/tenant, а suspended tool approval в старом session получал `approval_denied / no responder`. Текущий код уже должен сохранять exchange pending и redeliver'ить его при возврате responder'а; эта issue про финальную архитектурную нормализацию contract'а, чтобы такой класс ошибок не возвращался при новых exchange типах.
