# AuthN/AuthZ and Boundary Review — skill-plugin-architecture-followups — Attempt 1

Date: 2026-04-27

## `/internal/snapshot/plugins`

- The endpoint remains unauthenticated but is no longer remotely reachable by default.
- `cmd/tabula/main.go::internalDiagnosticsGuard` wraps only `/internal/snapshot/plugins`.
- `isLocalInternalDiagnosticsRequest` requires:
  - parseable `RemoteAddr`, and the remote host must be loopback;
  - `Host` must be loopback or `localhost`, with a narrow loopback-listener equivalence;
  - wildcard binds (`0.0.0.0`, `::`, empty host) do not authorize public-looking `Host` values;
  - `X-Forwarded-*` headers are not trusted.
- Focused tests cover loopback IPv4/IPv6, remote address rejection, public host rejection, malformed remote address rejection, forwarded-locality rejection, and non-GET handling.
- Docs warn operators not to expose the endpoint through public ingress or unauthenticated reverse proxies and enumerate the metadata returned.

Verdict: OK.

## Plugin runtime / protocol authz boundary

- Plugin registration failures do not install registry, dispatch, or hook-index state.
- Unsupported subscription events are rejected through parent-kernel validation.
- Dynamic `update_tools` validation is atomic: invalid updates do not erase prior valid dispatch entries.
- Invalid `event_reply` actions cancel the pending call and rely on the hook timeout/fail-closed path instead of mapping unknown actions to pass.
- Invalid / ambiguous `tool_result` payloads are rejected and pending calls are released rather than delivered as success.

Verdict: OK.

## External-evidence-gated spawn / SDK surfaces

- The kernel D1.11(b) spawn bridge, skipped spawn tests, and temporary SDK directories remain present because the external driver/subagent, Python wheel, TypeScript tarball, and bundle `_lib` rows are still `unknown/blocker`.
- BUILD did not remove those bridges prematurely.
- Active docs / SDK public surfaces no longer claim stable generic `api.spawn`, common `TABULA_SPAWN_TOKEN`, or non-empty default kernel tools.

Verdict: OK, with a non-blocking warning that external evidence blockers remain and must be surfaced in REFLECT/ARCHIVE.
