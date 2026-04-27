# AuthN/AuthZ review — skill-plugin-architecture / Attempt 1

Date: 2026-04-27

## Surfaces touched

### WebSocket origin / client auth

- `cmd/tabula/main.go::checkWebSocketOrigin` allows empty Origin, localhost/loopback/request-host origins by default, or exact configured origins via `TABULA_ALLOWED_ORIGINS`.
- Existing spawn-token auth for child clients remains in `PolicyEngine.CanConnect`, but LLM-visible kernel `process_spawn` was removed. Spawn-token code is retained as D1.11(b) dead code until subagent plugin migration.

### Plugin boot and lifecycle

- `cmd/tabula/main.go` loads `bootConfig.Plugins` and calls `hub.LoadPlugins` before `hub.StartReaper()`.
- `plugin.LoadManifest` validates plugin `id`, `version`, `runtime`, and relative `entry` path; `runtime.Spawn` executes only allowed runtime launcher (`python3`/`node`) plus the validated entry file.
- Plugins are local distro-provided code, not remote user input. Boot manifests are therefore local configuration trust boundary.

### Plugin ↔ kernel protocol

- Runtime sends `register_request` with protocol version/plugin id/config and requires a `register` reply with matching protocol version and plugin id (`waitForRegister`, `Handle.MarkRegistered`). Mismatches are `NonRestartable`.
- Inbound plugin messages are centrally handled by `handlePluginProtocolMessage`. Unknown methods warn/drop.
- Plugin tools are advertised through the same `Hub.toolExec` table as skill tools and guarded by `before_tool_call` through `PolicyEngine.CanUseTool` before dispatch.
- Plugin hook subscriptions use `HookSubscriber` adapter and inherit existing hook timeout/fail-closed behavior for security hooks.

### HTTP diagnostics

- New `GET /internal/snapshot/plugins` endpoint returns plugin ids, status, PID, restart count, last error, tool names, subscriptions, registered timestamp.
- The endpoint follows existing unauthenticated local HTTP surface patterns (`/health`, `/sessions`). It exposes local operational metadata; classified as `WARNING` due to sensitive-process metadata potential if server is exposed beyond localhost.

## Auth verdict

No anonymous privilege-escalating route or auth bypass was identified in the BUILD delta. Plugin startup is controlled by local boot config, manifest validation rejects path traversal entries, and plugin tool use remains subject to hook policy before invocation.

Warnings:

- Diagnostic plugin snapshot endpoint is unauthenticated like existing endpoints; safe only under the assumption that the kernel HTTP server is local/trusted or otherwise network-isolated.
- Spawn-token/MaxChildren invariant is intentionally dead-code-kept after kernel `process_spawn` removal until subagent plugin migration. This is a known task risk, not a new exploitable LLM-visible path in this repo.
