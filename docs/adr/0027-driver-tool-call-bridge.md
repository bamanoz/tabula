# ADR 0027 - Driver tool calls use correlated execution frames

Date: 2026-08-08
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Protocol v4 permits a session-scoped driver to call a provider only after a fenced permit. Providers may then request tools and require the terminal tool results before they can continue generation.

The existing paths were disconnected:

- driver workers could emit sequenced output and terminal frames but had no request/result operation for tools;
- Runtime API could carry driver mutations but could not relay a tool request from a worker or route its result back to the exact attempt;
- kernel tool dispatch was entered only by a client connection, although policy, lifecycle, runtime invocation, streaming, and attempt fencing are generic session behavior.

Encoding a tool request as `turn.output` would make a durable output acknowledgement block provider continuation and would conflate an execution request with committed output. Making the kernel aware of provider continuation semantics would also violate the kernel boundary.

## Decision

Add a bidirectional, correlated tool bridge to execution protocol v4.

The driver worker emits `tool_call` after permit with the complete attempt/fence scope, a provider tool-call ID, tool name, JSON input, and a correlation ID. Runtime translates it to `turn.tool_call` and immediately returns the ordinary correlated `driver.result` acknowledgement after kernel accepts dispatch. The Runtime API reader therefore remains free to process later frames.

Kernel dispatches the request through the existing generic tool service. It applies the same attempt validation, hooks, approval/exchange behavior, runtime invocation, streaming, cancellation, lifecycle, and terminal-result claim used by client-originated v4 calls. Runtime-originated calls do not require or synthesize a WebSocket client.

When the tool reaches a terminal result, kernel sends a separate `turn.tool_result` request through the authenticated runtime connection. Runtime routes it to the exact session worker and fenced attempt as `tool_result`. The driver SDK correlates the result to the waiting provider tool call, calls `ProviderSession.add_tool_results`, and continues provider generation until no more tool calls remain.

The kernel treats tool names, inputs, outputs, artifacts, and provider continuation as opaque. It owns only routing, policy/lifecycle coordination, attempt validation, and correlation.

## Consequences

Positive:

- real provider tool use works in durable protocol v4 sessions;
- provider continuation does not block the Runtime API reader;
- tool calls reuse one policy, approval, lifecycle, streaming, and fencing path;
- results return only to the exact runtime worker and fenced attempt that requested them;
- kernel remains independent of providers and concrete tools.

Negative:

- execution v4 adds two operations on both the Runtime API and worker wire surfaces;
- driver SDKs must keep a correlated pending-request table and unblock it on cancellation or shutdown;
- runtime and driver components must move forward together because a provider tool request cannot continue on an older runtime protocol implementation.

## Verification

Implementation must prove:

- worker and Runtime API strict round trips for tool request/result frames;
- runtime transport remains bidirectional while a tool call is in flight;
- supervisor translation preserves authenticated runtime identity, complete attempt scope, fence, and correlation;
- kernel dispatch returns terminal tool output through the same fenced attempt;
- Python provider execution calls `add_tool_results` and performs a subsequent generation;
- cancellation and worker close unblock pending SDK requests;
- live browser execution renders the tool call/result and completes with the tool-derived nonce.
