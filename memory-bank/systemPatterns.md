# System Patterns

## execute-remote-runtime-program — CREATIVE chosen patterns (2026-05-02)

- **Typed Runtime API contract**: `internal/runtime/wire/` should own a typed operation envelope and operation-specific payloads. All transports consume the same wire contract; transport packages must not define divergent message shapes.
- **Canonical wire errors only**: Runtime API errors use the amendment-normalized roster: `unauthorized`, `runtime_unavailable`, `runtime_busy`, `unknown_runtime`, `tenant_unknown`, `tenant_forbidden`, `target_unknown`, `target_forbidden`, `tool_not_found`, `timeout`, `cancelled`, `protocol_error`, `internal_error`, `skill_exec_failed`. Plugin-internal errors such as `fs_outside_root` and `exec_denied` stay inside tool result envelopes.
- **Runtime daemon as process host**: Kernel backends produce `RuntimeConn`; only `tabula-runtime` hosts/supervises workers. Kernel-side plugin stdio is deleted in M2-07 and kernel-side skill subprocess execution is deleted in M3-08.
- **Dial-out runtime transport**: Kernel listens; runtime dials. Local mode uses unix socket plus token file; WSS and SSH reuse the same Runtime API and auth handshake.
- **Tenant validation split**: Treat syntax validation, hard-reserved validation, runtime-read validation, and user-create policy as separate concerns. `default` is a valid system-managed tenant; `system`, `runtime`, `kernel`, and `admin` are hard-reserved for normal tenant IDs.
- **Layered config registry**: Kernel global config uses singular `[[runtime]]`; tenant config owns `[tenant] allowed_runtimes/default_runtime` and `[workspace] project_root`; runtime-side config uses singular `[[kernel]]`. Do not add plural aliases.
- **Three-layer tenant enforcement**: Kernel router rejects before wire send, runtime whitelist rejects before worker spawn, and worker SDK refuses missing tenant/kernel env. Pool keys include `(kernel_id, tenant_id, target_id)`.
- **Resolver-first workspace decomposition**: Land `${tabula_home}`/`${tenant_id}` scaffold, then real tenant context, then `${project_root}` before fs/exec consume it. Workspace is tenant config plus plugins, not a kernel abstraction.
- **Global code, per-tenant context**: Runtime enumerates skills/plugins from global `$TABULA_HOME/skills` and `$TABULA_HOME/plugins`; per-tenant symlinks are introspection only. Isolation comes from config overlay and env/state/log paths.
- **Remote backend layered security**: Bearer token remains universal; mTLS is optional second factor for WSS; SSH uses system `ssh`; service install remains shell orchestration invoking foreground `tabula serve` directly.
