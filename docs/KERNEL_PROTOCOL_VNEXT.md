# Kernel Protocol vNext Design Notes

Status: completed and accepted as kernel WebSocket protocol `v: 3`.

Implemented:

- `v: 3`
- `hello` / `hello_ack`
- `hello.data` with `name`, `auth_token`, `send_topics`, `receive_topics`,
  `global_topics`, and `hooks`
- validation of `v == 3` on every incoming message
- kernel-owned `meta.kernel` for routed session/global deliveries
- user messages as `event` with `topic: "message.user"` and `data.text`
- assistant stream messages as `event` with `topic: "stream.start"`,
  `topic: "stream.delta"` + `data.text`, and `topic: "stream.end"`
- reasoning messages as `event` with `topic: "reasoning.start"`,
  `topic: "reasoning.delta"` + `data.text`, and `topic: "reasoning.end"`
- compaction messages as `event` with `topic: "compaction.start"`,
  `topic: "compaction.end"`, and `topic: "compaction.error"`
- tool calls as `request` with `topic: "tool.call"`; tool results as `reply`
  with `topic: "tool.result"`
- `exchange.choose` and `exchange.approve` as identity-bound `request` / `reply`
  flows
- `hook_reply` for hook replies, identity-bound to the hook recipient
- `usage.update` as an event topic instead of `status`
- first-party Python and TypeScript SDKs, gateways, drivers, testbed clients,
  and installed test suites on v3 frames

Known non-goals / deferred items:

- wildcard topic matching such as `stream.*`; current capability checks use exact
  topic names
- generic targeted client events beyond the implemented exchange/request flows

This document proposes a cleaned-up kernel WebSocket protocol that replaces
ad-hoc message shapes with a smaller set of generic envelopes and makes
identity-bound interactions first-class.

The main goal is to avoid adding special pairs such as `ask_request` /
`ask_response`, `approval_request` / `approval_response`, or future one-off
request/reply protocols. Instead, the protocol should expose one reusable
operation for interactive, identity-bound exchanges.

## Problems In The Current Protocol

Previous protocol version: `2`. Current protocol version: `3`.

The current protocol works, but the surface grew organically:

- `type` mixes transport category, domain action, lifecycle event, and custom
  plugin event names.
- Message-specific fields are top-level: `text`, `input`, `output`, `context`,
  `tools`, `payload`, `action`, `reason`, `sends`, `receives`, `hooks`.
- `meta` is opaque, but not structured into trusted kernel metadata vs
  user/plugin metadata.
- `status` is overloaded for usage, display notices, interactive questions, and
  approval answers.
- `ask_request` / `ask_response` are not kernel-mediated and are matched only by
  ids inside `meta`.
- There is no generic “send request to this client and accept reply only from
  that same client” primitive.
- Clients self-describe by `name`, `sends`, `receives`, and `hooks`, but there is
  no structured role/capability model.
- `receives_global` is powerful and currently used as a workaround for
  cross-session control flows.
- Protocol version was only validated on `connect`.

## Design Goals

- Keep the kernel generic; no distro-specific concepts in core protocol.
- Keep the transport small and explicit.
- Move message-specific payloads under `data`.
- Reserve `meta.kernel` for kernel-controlled metadata.
- Keep user/plugin metadata under `meta.user` or component-specific namespaces.
- Make client identity first-class after authenticated `hello`.
- Provide one generic identity-bound request/reply primitive for approvals,
  ask-user, confirmations, file-picker-like UX, future handoffs, and other
  interactive operations.
- Reduce the need for `receives_global`.
- Preserve session bus semantics for normal chat/tool/stream messages.
- Make routing explicit: session, global, client-targeted, or exchange-targeted.

## Non-Goals

- This proposal does not redesign the Runtime API / worker protocol.
- This proposal does not define UI rendering details for approvals.
- This proposal does not put product policy into the kernel.
- This proposal does not require a distributed ACL system; it only defines
  kernel-verifiable sender/recipient identity for protocol messages.

## vNext Envelope

All messages use this envelope:

```json
{
  "v": 3,
  "type": "event",
  "id": "msg_01J...",
  "session": "main",
  "tenant_id": "default",
  "topic": "message.user",
  "data": {},
  "meta": {
    "kernel": {},
    "user": {}
  }
}
```

Fields:

| Field | Required | Meaning |
|---|---:|---|
| `v` | yes | Kernel WebSocket protocol version. Proposed value: `3`. |
| `type` | yes | Transport-level message kind. Small enum. |
| `id` | no | Message id or correlation id. Required for request/reply flows. |
| `session` | no | Session route. Required for session-scoped bus events. |
| `tenant_id` | no | Tenant/app route. Defaults to `default` when session is joined. |
| `topic` | no | Domain event/action name. Required for `event`, `request`, `reply`, `hook`. |
| `data` | no | Type/topic-specific payload. Replaces top-level `text`, `input`, `output`, `payload`, `context`, `tools`, `action`, `reason`. |
| `meta` | no | Metadata container. `meta.kernel` is reserved and overwritten by kernel. |

## Transport-Level Types

The `type` field should be a small enum:

```text
hello
hello_ack
join
joined
event
request
reply
hook
hook_reply
error
ping
pong
```

Optional later:

```text
leave
goodbye
```

### Why `topic` Exists

Instead of adding many top-level `type` values like `stream_delta`,
`reasoning_delta`, `tool_use`, `status`, and `compaction_start`, v3 uses:

```json
{
  "type": "event",
  "topic": "stream.delta",
  "data": {
    "text": "partial"
  }
}
```

This keeps kernel routing stable while allowing domain protocols to evolve.

## Reserved Metadata

`meta.kernel` is always kernel-controlled. Clients may send it, but the kernel
must ignore or overwrite it before routing.

Example routed event:

```json
{
  "v": 3,
  "type": "event",
  "topic": "message.user",
  "session": "main",
  "data": {
    "text": "hello"
  },
  "meta": {
    "kernel": {
      "sender": {
        "id": "c7",
        "name": "gateway-cli-main",
        "roles": ["gateway", "interactive"]
      },
      "route": {
        "scope": "session",
        "session": "main",
        "tenant_id": "default"
      },
      "received_at": "2026-05-21T00:00:00Z"
    },
    "user": {
      "source": "gateway-cli"
    }
  }
}
```

Reserved `meta.kernel` fields:

| Field | Meaning |
|---|---|
| `sender` | Kernel-authenticated sender identity. |
| `recipient` | Kernel-selected recipient identity for targeted delivery. |
| `route` | Route chosen by kernel. |
| `exchange` | Identity-bound exchange metadata. |
| `received_at` | Kernel receive timestamp. |
| `sequence` | Optional monotonically increasing per-session sequence. |

Client/plugin metadata should use `meta.user` or a namespaced key:

```json
{
  "meta": {
    "user": {
      "agent": "reviewer",
      "model": "anthropic/..."
    },
    "gateway.web": {
      "browser_message_id": "..."
    }
  }
}
```

## Client Hello

`hello` is the authenticated kernel WebSocket handshake.

Client:

```json
{
  "v": 3,
  "type": "hello",
  "id": "hello_01J...",
  "data": {
    "name": "gateway-cli-main",
    "auth_token": "ktk_...",
    "roles": ["gateway", "interactive"],
    "send_topics": [
      "message.user",
      "turn.cancel",
      "exchange.approve"
    ],
    "receive_topics": [
      "stream.start",
      "stream.delta",
      "stream.end",
      "reasoning.start",
      "reasoning.delta",
      "reasoning.end",
      "tool.call",
      "tool.result",
      "exchange.approve",
      "session.member_joined",
      "error"
    ],
    "global_topics": [],
    "hooks": []
  }
}
```

Kernel:

```json
{
  "v": 3,
  "type": "hello_ack",
  "id": "hello_01J...",
  "data": {
    "client_id": "c7",
    "server_protocol": 3,
    "accepted_roles": ["gateway", "interactive"]
  },
  "meta": {
    "kernel": {
      "sender": {
        "id": "kernel"
      }
    }
  }
}
```

Notes:

- `roles` are not product policy. They describe generic transport behavior such
  as `gateway`, `driver`, `observer`, `interactive`, `hook_subscriber`,
  `delivery`, `readiness_probe`.
- The kernel may reject unknown roles or ignore them depending on strictness.
- `send_topics`/`receive_topics` replace `sends`/`receives`.
- `global_topics` replaces `receives_global` and should be rare.

## Join

Client:

```json
{
  "v": 3,
  "type": "join",
  "id": "join_01J...",
  "session": "main",
  "tenant_id": "default"
}
```

Kernel:

```json
{
  "v": 3,
  "type": "joined",
  "id": "join_01J...",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "created": false
  }
}
```

Initial context/tools become a normal event after join:

```json
{
  "v": 3,
  "type": "event",
  "topic": "session.init",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "context": "initial context",
    "tools": [],
    "workspace": {
      "path": "/workspace"
    }
  }
}
```

Member joined becomes:

```json
{
  "v": 3,
  "type": "event",
  "topic": "session.member_joined",
  "session": "main",
  "data": {
    "client": {
      "id": "c9",
      "name": "driver"
    }
  }
}
```

## Events

`event` is fire-and-forget bus delivery.

User message:

```json
{
  "v": 3,
  "type": "event",
  "topic": "message.user",
  "session": "main",
  "id": "msg_01J...",
  "data": {
    "text": "Implement feature X"
  },
  "meta": {
    "user": {
      "source": "gateway-web"
    }
  }
}
```

This shape is implemented for user messages. Clients now advertise
`message.user` in `send_topics` / `receive_topics` / `global_topics`.

Assistant stream:

```json
{
  "v": 3,
  "type": "event",
  "topic": "stream.start",
  "session": "main",
  "data": {}
}
```

```json
{
  "v": 3,
  "type": "event",
  "topic": "stream.delta",
  "session": "main",
  "data": {
    "text": "partial text"
  }
}
```

```json
{
  "v": 3,
  "type": "event",
  "topic": "stream.end",
  "session": "main",
  "data": {}
}
```

Reasoning stream:

```json
{
  "v": 3,
  "type": "event",
  "topic": "reasoning.start",
  "session": "main",
  "data": {},
  "meta": {
    "user": {
      "provider": "anthropic"
    }
  }
}
```

```json
{
  "v": 3,
  "type": "event",
  "topic": "reasoning.delta",
  "session": "main",
  "data": {
    "text": "reasoning chunk"
  }
}
```

```json
{
  "v": 3,
  "type": "event",
  "topic": "reasoning.end",
  "session": "main",
  "data": {}
}
```

Turn done:

```json
{
  "v": 3,
  "type": "event",
  "topic": "turn.done",
  "session": "main",
  "data": {
    "usage": {
      "input_tokens": 1200,
      "output_tokens": 300,
      "context_window": 128000
    }
  }
}
```

Error:

```json
{
  "v": 3,
  "type": "error",
  "id": "msg_01J...",
  "data": {
    "code": "message_blocked",
    "message": "message blocked"
  }
}
```

## Tools As Requests

Tool calls are identity-bound request/reply exchanges too. The driver asks the
kernel/tool system to perform a tool call; the kernel replies with the result.

Driver to kernel:

```json
{
  "v": 3,
  "type": "request",
  "topic": "tool.call",
  "id": "toolu_123",
  "session": "main",
  "data": {
    "name": "fs_read",
    "input": {
      "path": "README.md"
    }
  },
  "meta": {
    "user": {
      "actor": "plan"
    }
  }
}
```

Kernel broadcasts observable tool call event to session observers:

```json
{
  "v": 3,
  "type": "event",
  "topic": "tool.call_started",
  "id": "toolu_123",
  "session": "main",
  "data": {
    "name": "fs_read",
    "input": {
      "path": "README.md"
    }
  }
}
```

Kernel replies to requester and/or broadcasts result:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "tool.result",
  "id": "toolu_123",
  "session": "main",
  "data": {
    "name": "fs_read",
    "ok": true,
    "output": "file contents"
  }
}
```

Tool error:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "tool.result",
  "id": "toolu_123",
  "session": "main",
  "data": {
    "name": "fs_read",
    "ok": false,
    "error": {
      "code": "unknown_tool",
      "message": "unknown tool fs_read"
    }
  }
}
```

## Hooks

Hooks remain generic, but use `hook` / `hook_reply` envelopes.

Hook subscription on hello:

```json
{
  "v": 3,
  "type": "hello",
  "data": {
    "name": "hook-permissions",
    "auth_token": "ktk_...",
    "roles": ["hook_subscriber"],
    "hooks": [
      {
        "topic": "tool.before_call",
        "priority": 100,
        "timeout_ms": 0
      }
    ],
    "send_topics": ["hook.reply"],
    "receive_topics": ["hook.request"]
  }
}
```

Kernel hook request:

```json
{
  "v": 3,
  "type": "hook",
  "topic": "tool.before_call",
  "id": "hook_01J...",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "tool": {
      "name": "exec_run",
      "id": "toolu_123",
      "input": {
        "command": "ls"
      }
    }
  },
  "meta": {
    "kernel": {
      "recipient": {
        "id": "c12",
        "name": "hook-permissions"
      }
    }
  }
}
```

Hook pass:

```json
{
  "v": 3,
  "type": "hook_reply",
  "topic": "tool.before_call",
  "id": "hook_01J...",
  "data": {
    "action": "pass"
  }
}
```

Hook modify:

```json
{
  "v": 3,
  "type": "hook_reply",
  "topic": "tool.before_call",
  "id": "hook_01J...",
  "data": {
    "action": "modify",
    "patch": {
      "tool": {
        "input": {
          "command": "ls",
          "approved": true
        }
      }
    }
  }
}
```

Hook block:

```json
{
  "v": 3,
  "type": "hook_reply",
  "topic": "tool.before_call",
  "id": "hook_01J...",
  "data": {
    "action": "block",
    "reason": "dangerous command"
  }
}
```

The kernel accepts `hook_reply` only from the recipient identity that received
the hook request.

## Generic Identity-Bound Exchange

This is the key replacement for `ask_request` / `ask_response` and future
one-off request/response pairs.

An exchange is a kernel-mediated request sent to one specific client, with a
reply accepted only from that same client.

Exchange use cases:

- ask user a question
- request tool approval
- confirm destructive action
- request file/path selection
- request credential handoff without broadcasting secrets
- request UI-specific input from a gateway
- future interactive plugin operations

### Exchange Topics

Use generic topics:

```text
exchange.ask
exchange.confirm
exchange.choose
exchange.approve
exchange.input
```

The kernel should not know product-specific semantics of these topics. It only
enforces routing, identity, timeout, and reply ownership.

### Exchange Request

Requester to kernel:

```json
{
  "v": 3,
  "type": "request",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "title": "Approve tool call",
    "body": "Run shell command?",
    "details": {
      "tool": "exec_run",
      "input": {
        "command": "ls"
      }
    },
    "choices": [
      {"id": "allow_once", "label": "allow once"},
      {"id": "allow_always", "label": "allow always"},
      {"id": "deny_once", "label": "deny once"},
      {"id": "deny_always", "label": "deny always"}
    ],
    "default_choice": "deny_once",
    "timeout_ms": 300000
  },
  "meta": {
    "user": {
      "source": "hook-approvals"
    }
  }
}
```

Kernel chooses an interactive responder in the same session/tenant and forwards
the request to that one client:

```json
{
  "v": 3,
  "type": "request",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "title": "Approve tool call",
    "body": "Run shell command?",
    "details": {
      "tool": "exec_run",
      "input": {
        "command": "ls"
      }
    },
    "choices": [
      {"id": "allow_once", "label": "allow once"},
      {"id": "allow_always", "label": "allow always"},
      {"id": "deny_once", "label": "deny once"},
      {"id": "deny_always", "label": "deny always"}
    ],
    "default_choice": "deny_once",
    "timeout_ms": 300000
  },
  "meta": {
    "kernel": {
      "exchange": {
        "id": "ex_01J...",
        "requester": {
          "id": "runtime:local:plugin:hook-approvals"
        },
        "responder": {
          "id": "c7",
          "name": "gateway-cli-main"
        },
        "expires_at": "2026-05-21T00:05:00Z"
      }
    }
  }
}
```

Responder to kernel:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "session": "main",
  "data": {
    "choice": "allow_once"
  }
}
```

Kernel validates:

- exchange id exists
- exchange not expired
- sender client id equals stored responder id
- session/tenant match exchange route
- reply topic matches request topic

Then kernel delivers the reply to the requester:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "session": "main",
  "tenant_id": "default",
  "data": {
    "choice": "allow_once"
  },
  "meta": {
    "kernel": {
      "exchange": {
        "id": "ex_01J...",
        "requester": {
          "id": "runtime:local:plugin:hook-approvals"
        },
        "responder": {
          "id": "c7",
          "name": "gateway-cli-main"
        }
      }
    }
  }
}
```

Any other client attempting to reply gets an error or silent denial:

```json
{
  "v": 3,
  "type": "error",
  "id": "ex_01J...",
  "data": {
    "code": "exchange_wrong_responder",
    "message": "client is not allowed to reply to this exchange"
  }
}
```

### Exchange Timeout

Kernel timeout reply to requester:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "data": {
    "error": {
      "code": "exchange_timeout",
      "message": "exchange timed out"
    }
  }
}
```

### Ask User Without New Message Types

Ask-user becomes the same exchange primitive:

```json
{
  "v": 3,
  "type": "request",
  "topic": "exchange.choose",
  "id": "ex_ask_01J...",
  "session": "main",
  "data": {
    "title": "Question",
    "body": "Continue?",
    "choices": [
      {"id": "yes", "label": "yes"},
      {"id": "no", "label": "no"}
    ]
  }
}
```

Reply:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.choose",
  "id": "ex_ask_01J...",
  "data": {
    "choice": "yes"
  }
}
```

### Approval Without `ask_request` / `ask_response`

The old approval shape:

```json
{
  "type": "status",
  "meta": {
    "ask_request": {
      "id": "abcd1234",
      "question": "Approve tool exec_run?",
      "options": ["allow once", "deny once"]
    }
  }
}
```

becomes:

```json
{
  "v": 3,
  "type": "request",
  "topic": "exchange.approve",
  "id": "ex_approval_01J...",
  "session": "main",
  "data": {
    "title": "Approve tool exec_run",
    "body": "A tool requires approval.",
    "details": {
      "tool": "exec_run",
      "input": {
        "command": "ls"
      }
    },
    "choices": [
      {"id": "allow_once", "label": "allow once"},
      {"id": "allow_always", "label": "allow always"},
      {"id": "deny_once", "label": "deny once"},
      {"id": "deny_always", "label": "deny always"}
    ]
  }
}
```

The old response:

```json
{
  "type": "status",
  "meta": {
    "ask_response": {
      "id": "abcd1234",
      "choice": "allow once",
      "index": 0
    }
  }
}
```

becomes:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "ex_approval_01J...",
  "data": {
    "choice": "allow_once"
  }
}
```

No `ask_request` or `ask_response` fields are needed.

## Targeted Events

Most messages are session bus events, but the kernel should support targeted
delivery for generic infrastructure flows.

Targeted event example:

```json
{
  "v": 3,
  "type": "event",
  "topic": "ui.notice",
  "id": "notice_01J...",
  "data": {
    "level": "info",
    "message": "Background task completed"
  },
  "meta": {
    "kernel": {
      "recipient": {
        "id": "c7"
      },
      "route": {
        "scope": "client"
      }
    }
  }
}
```

For client-originated targeted messages, clients should not set
`meta.kernel.recipient` directly. Instead use an explicit route field in `data`
or a kernel API that validates the target. Kernel-owned `meta.kernel` remains
authoritative.

## Topic Taxonomy

Recommended first-party topics:

```text
session.init
session.member_joined
turn.cancel
message.user
message.assistant
stream.start
stream.delta
stream.end
reasoning.start
reasoning.delta
reasoning.end
turn.done
hook.request
hook.reply
exchange.ask
exchange.confirm
exchange.choose
exchange.approve
exchange.input
ui.notice
usage.update
compaction.start
compaction.end
compaction.error
```

Component-specific topics should use namespaces:

```text
gateway.web.browser_joined
sessions.lifecycle
subagent.result
mcp.catalog_updated
```

## Mapping From Current Protocol To vNext

| Current | vNext |
|---|---|
| `connect` | `hello` |
| `connected` | `hello_ack` |
| `join` | `join` |
| `joined` | `joined` |
| `member_joined` | `event` + `topic=session.member_joined` |
| `init` | `event` + `topic=session.init` |
| `message` from gateway | `event` + `topic=message.user` |
| `stream_start` | `event` + `topic=stream.start` |
| `stream_delta` | `event` + `topic=stream.delta`, `data.text` |
| `stream_end` | `event` + `topic=stream.end` |
| `reasoning_start` | `event` + `topic=reasoning.start` |
| `reasoning_delta` | `event` + `topic=reasoning.delta`, `data.text` |
| `reasoning_end` | `event` + `topic=reasoning.end` |
| `done` | `event` + `topic=turn.done` |
| `tool_use` | `request` + `topic=tool.call` |
| `tool_result` | `reply` + `topic=tool.result` |
| `hook` | `hook` + hook topic |
| `hook_result` | `hook_reply` + hook topic |
| `cancel` | `event` + `topic=turn.cancel` |
| `error` | `error` with `data.code` and `data.message` |
| `status` usage | `event` + `topic=usage.update` |
| `status` ask_request | `request` + `topic=exchange.choose` or `exchange.approve` |
| `status` ask_response | `reply` + same `exchange.*` topic |
| `compaction_start` | `event` + `topic=compaction.start` |
| `compaction_end` | `event` + `topic=compaction.end` |
| `compaction_error` | `event` + `topic=compaction.error` |

Top-level field mapping:

| Current field | vNext location |
|---|---|
| `version` | `v` |
| `type` | `type` + `topic` |
| `name` on connect | `data.name` |
| `name` on tool | `data.name` |
| `text` | `data.text` |
| `input` | `data.input` |
| `output` | `data.output` |
| `context` | `data.context` |
| `tools` | `data.tools` |
| `payload` | `data` or `data.payload` only when truly opaque |
| `action` | `data.action` |
| `reason` | `data.reason` |
| `sends` | `data.send_topics` |
| `receives` | `data.receive_topics` |
| `receives_global` | `data.global_topics` |
| `hooks.event` | `data.hooks[].topic` |
| `meta` | `meta.user` or namespaced metadata |

## Client Role Model

Proposed generic roles:

```text
gateway
driver
observer
interactive
hook_subscriber
delivery
readiness_probe
runtime_proxy
```

Roles are generic transport capabilities, not distro policy.

Examples:

Gateway CLI:

```json
{
  "roles": ["gateway", "interactive"]
}
```

Driver:

```json
{
  "roles": ["driver"]
}
```

Hook permissions plugin/client:

```json
{
  "roles": ["hook_subscriber"]
}
```

Delivery helper:

```json
{
  "roles": ["delivery"]
}
```

Readiness check:

```json
{
  "roles": ["readiness_probe"]
}
```

The kernel can use roles for routing choices, especially exchanges. For example,
an `exchange.*` request defaults to clients with `interactive` role in the same
session/tenant.

## Exchange Responder Selection

Default responder selection for `exchange.*`:

1. Same session as request.
2. Same tenant as request.
3. Client has role `interactive`.
4. Client receive topics match the requested exchange topic or `exchange.*`.
5. Prefer the gateway that originated the current turn if known.
6. Otherwise choose most recent interactive gateway in session.

If no responder exists, the kernel replies to requester:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "ex_01J...",
  "data": {
    "error": {
      "code": "exchange_no_responder",
      "message": "no interactive responder is available"
    }
  }
}
```

The requester can choose fail-open or fail-closed policy. For tool approvals,
the policy should fail closed.

## Security Rules

Required vNext kernel rules:

- Validate `v` on every incoming message, not only `hello`.
- Require authenticated `hello` before any other message except `ping`.
- Ignore or overwrite incoming `meta.kernel`.
- Add authenticated sender identity to routed messages.
- Enforce `send_topics` for `event`, `request`, `reply`, `hook_reply`.
- Enforce joined session for session-scoped messages.
- For `hook_reply`, require sender identity to match pending hook recipient.
- For `exchange.*` reply, require sender identity to match pending exchange
  responder.
- For `tool.result`, only kernel/runtime tool machinery should emit canonical
  result replies for tool requests.
- Avoid `global_topics` for interactive control flows.

## Completion Notes

The migration was completed in one breaking protocol bump to `3`; there is no
kernel WebSocket fallback for v2 frames. First-party clients and installed
testbed suites now use `hello`, exact topic capabilities, and `event` /
`request` / `reply` frames.

The implemented completion criteria were:

- all first-party clients authenticate with `hello.data.auth_token`
- all client->kernel frames carry `v: 3`
- user messages use `event topic=message.user`
- stream, reasoning, compaction, usage, done, and cancel use event topics
- tools use `request topic=tool.call` and `reply topic=tool.result`
- ask-user uses `exchange.choose`
- approvals use `exchange.approve`
- hook replies use `hook_reply`
- installed `baseline`, `ask-user`, `subagents`, and `approvals` suites pass

## Summary

The core simplification is:

```text
transport type = how the kernel routes/correlates the message
topic = what domain operation/event the message represents
data = operation payload
meta.kernel = trusted kernel metadata
meta.user / namespaced meta = client/plugin metadata
```

The generic exchange primitive replaces `ask_request`/`ask_response` and gives
the kernel one reusable mechanism for all future identity-bound interactive
flows.
