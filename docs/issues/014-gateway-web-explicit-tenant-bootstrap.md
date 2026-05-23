# Gateway Web Explicit Tenant Bootstrap

Priority: High

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

Gateway-web is a singleton daemon, but the daemon currently inherits a default
tenant from whichever tenant-scoped plugin wrapper wins the singleton lock. That
means `/api/bootstrap` without an explicit tenant can resolve the wrong tenant.

## Evidence

- The singleton daemon was started by the `claw-tabula-dev` wrapper while the
  user expected `code-immune-tabula-dev` sessions.
- `/api/bootstrap` attempted to load `claw_agents` and returned an empty response
  when that module was missing.
- Kernel `/sessions` showed `code-immune-tabula-dev` sessions while bootstrap was
  resolving `default_tenant: claw-tabula-dev`.

## Impact

The web UI can show no sessions or the wrong sessions, and bootstrap can fail for
unrelated tenants. This makes session switching appear broken even when kernel
state is correct.

## Proposed Fix

- Make tenant identity explicit in web URLs, for example
  `/?tenant_id=code-immune-tabula-dev&token=...`.
- Ensure the frontend always calls `/api/bootstrap?tenant_id=<tenant>`.
- Add a tenant resolver in gateway-web that is request/session scoped, not daemon
  process scoped.
- If no tenant is provided, return a deterministic tenant selection response or a
  clear error; do not silently use the singleton owner wrapper's tenant.
- Keep bootstrap resilient when optional agent loading fails.

## Acceptance Criteria

- Bootstrap for `code-immune-tabula-dev` returns code-immune sessions even when
  the singleton daemon was started by another tenant wrapper.
- Broken agent catalogs for another tenant do not prevent the current tenant from
  loading.
- Frontend persists and restores `{tenant_id, session}` together.
- Tests cover explicit tenant bootstrap and fallback behavior when agents fail.
