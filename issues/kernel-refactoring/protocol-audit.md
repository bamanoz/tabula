# Current protocol audit

**Status:** completed  
**Scope:** committed protocol surface at `HEAD`, plus active first-party consumers in `tabula-bundles` and `tabula-distrib`

## Protocol boundaries

Tabula has three distinct protocols. Cleanup decisions must not mix them:

1. Kernel WebSocket protocol v3: kernel to clients, gateways, TUI, and the current driver.
2. Runtime API v1: kernel to `tabula-runtime`.
3. Runtime worker protocol v1: runtime to plugin workers.

The proposed agent execution protocol v4 changes the first boundary and adds typed driver/runtime control. It does not justify opportunistic removal from Runtime API or worker protocol.

## Remove independently from v3

### Unused `status` transport type

`internal/kernel/protocol.go` declares `MsgStatus = "status"`, but committed core has no producer, consumer, router branch, or validation specific to it. No first-party bundle or distro uses it as a kernel WebSocket transport type.

`docs/KERNEL_PROTOCOL_VNEXT.md` records that v3 replaced old `status` usage with:

- `event` plus `usage.update`;
- `request`/`reply` plus `exchange.choose` or `exchange.approve`.

Remove `MsgStatus` and stale examples or prose that still present `status` as current.

### Legacy top-level handshake fields

`Message` still contains these fields from pre-v3 handshake shapes:

```text
Sends
Receives
ReceivesGlobal
Token
AuthToken
Hooks
```

Current v3 handshake reads only `hello.data` through `helloData`:

```text
send_topics
receive_topics
global_topics
auth_token
hooks
```

Committed kernel code does not read the old top-level fields. Remove them from `Message` and `cloneMessage`. Keep `helloData` fields.

The old top-level `token` field is not a supported auth field. Current docs already state that kernel-managed spawn tokens were removed. Unknown JSON fields are tolerated, so an old sender will not authenticate through it.

### Active top-level message fields

The v3 design says topic-specific payload moved under `data`, but active kernel paths still use several top-level fields. `State`, `Context`, and `Tools` are used by `session.init` and tool-result flows; `Input`, `Output`, `Artifact`, `Truncated`, `Payload`, `Action`, and `Reason` also participate in active tool or hook paths.

These fields are not legacy cleanup candidates. They require a coordinated protocol v4 replacement and remain part of v3 until cutover.

## Replace during protocol v4 cutover

These surfaces are active, but encode the fragile current agent lifecycle:

- `message.user` as an ordinary routed event;
- driver inference from `receive message.user` plus `send turn.done`;
- `client_role=driver` metadata outside a typed registration contract;
- kernel selection through `pickTurnReceiver`;
- managed user input metadata;
- in-memory pending message plans and capability-driven FIFO dispatch;
- `waiting_for_driver` and `turn_receiver_unavailable` status values;
- gateway wake/rejoin retry behavior;
- `turn.done` as the only positive execution boundary;
- `session.member_joined` as a readiness proxy.

They must be removed only after v4 provides durable input acceptance, driver registration, fenced leases, attempts, execution permits, terminal outcomes, and resumable state subscriptions.

### Session lifecycle events

`session.archive`, `session.delete`, `session.archived`, and `session.deleted` are active in kernel and gateway clients. Do not delete them as legacy. In v4 they should become typed commands and committed events over the client API, backed by `SessionRepository` semantics.

### Exchange topics

`exchange.choose` and `exchange.approve` have active producers and consumers, including question and approval flows. They are identity-bound request/reply operations, not dead legacy.

The v4 design must decide whether to:

- retain them as typed client exchanges; or
- replace both with one typed interaction command/event schema.

Removal without replacement would break approvals and user questions.

### Stream, reasoning, usage, provider, and compaction events

These topics have active driver and gateway consumers:

```text
stream.start / stream.delta / stream.end
reasoning.start / reasoning.delta / reasoning.end
usage.update
provider.retry / provider.error
compaction.start / compaction.end / compaction.error
```

They should move under sequenced durable turn output in v4. Their product payloads remain driver-owned; kernel persists and orders the envelopes but does not interpret provider or compaction policy.

### Tool result streaming

`tool.result.start`, `tool.result.delta`, and `tool.result.end` are active across kernel, runtime, driver, and gateways. They are not cleanup candidates. Issue 12 must decide how their lifecycle maps to durable attempts and cancellation.

## Keep outside this refactoring unless evidence changes

### Runtime API v1

All declared operations have implementation and test references, including:

```text
hello / hello_ack
invoke / invoke_result
invoke_result_start / delta / end
cancel / cancel_ack
health / health_resp
list_capabilities / list_capabilities_resp
reload / reload_ack
hook_event / hook_event_reply
catalog_update
plugin_send
plugin_log
lifecycle_notice
```

The driver supervision work may add or split a typed execution control channel. It must not remove plugin Runtime API operations just because names overlap with the new agent protocol.

### Runtime worker protocol v1

All declared worker operations represent active plugin host behavior:

```text
init / init_ack
call / result
event / event_reply
send
log
tools_updated
shutdown
error
```

Driver should no longer pretend to be a generic kernel WebSocket app, but plugin workers remain generic runtime workers. Any cleanup here requires a separate producer/consumer audit and protocol decision.

## Documentation cleanup

`docs/KERNEL_PROTOCOL_VNEXT.md` is historical design material marked as completed v3. It still contains old before/after `status` examples later in the document, which can be mistaken for current protocol examples. Issue 02 should either:

- reduce it to a concise immutable historical note and rely on current protocol docs; or
- clearly label migration examples as historical throughout.

The authoritative current docs must remain `docs/PROTOCOL.md` and `docs/KERNEL_PROTOCOL_EXAMPLES.md` until v4 replaces them.

## Audit method

The audit used:

- committed source via `git show HEAD:` and `git grep HEAD` to avoid treating unfinished worktree experiments as current behavior;
- repository-wide searches in `tabula`, `tabula-bundles`, and `tabula-distrib`;
- current protocol docs and the accepted v3 design notes;
- producer/consumer checks for suspect topics and operations.

Historical competitor repositories under `/Users/mak/src` were not treated as Tabula consumers.
