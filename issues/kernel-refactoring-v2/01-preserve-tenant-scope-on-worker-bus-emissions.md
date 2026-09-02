# Preserve tenant scope on worker bus emissions

**Type:** AFK  
**Status:** proposed

## What to build

Carry the worker's authenticated tenant scope through `WorkerSend -> PluginSend -> hubRuntimeAsyncSink.PluginSent` so runtime-originated bus events never infer tenant identity from a session ID.

The pool already knows the tenant of each worker. It must stamp that tenant onto every published `PluginSend`. Tenant-sensitive runtime paths must reject missing tenant scope rather than falling back to the default tenant.

## Evidence

- `internal/runtime/worker/wire/types.go`: `WorkerSend` carries no tenant.
- `internal/runtime/host/pool/pool.go`: `watchWorker` receives `tenantID` but drops it when constructing `PluginSend`.
- `internal/kernel/runtime_sink.go`: `PluginSent` resolves missing tenant through `sessionTenantID`.
- `internal/kernel/session.go`: ambiguous same-named sessions resolve to the default tenant.
- `../tabula-distrib/testbed/tests/test_multi_distro_tenants.py` intentionally uses `shared-session` across tenants.

## Acceptance criteria

- [ ] Every worker-originated `PluginSend` carries the worker's runtime-attested tenant ID.
- [ ] Kernel rejects a runtime plugin send without tenant scope when session routing is requested.
- [ ] No runtime-originated path infers tenant from a globally non-unique session ID.
- [ ] Pool tests assert tenant propagation, not only message type and target.
- [ ] A multi-tenant test uses the same session ID in two tenants and proves each event reaches only its owning tenant.
- [ ] Focused runtime/kernel tests pass with `-race -count=1`.

## Blocked by

None - can start immediately.
