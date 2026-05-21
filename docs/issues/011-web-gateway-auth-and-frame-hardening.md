# Web Gateway Auth And Frame Hardening

Priority: Medium

Repos: `tabula-bundles`

## Problem

The web gateway accepts auth tokens in query strings and sets a cookie without
`HttpOnly`. Origin defaults allow all origins when `allowed_origins` is empty.
Its manual WebSocket implementation lacks normal protocol hardening.

## Evidence

- `gateways/gateway-web/daemon.py`: `_authorized` accepts `?token=`,
  Authorization header, or cookie.
- `gateways/gateway-web/daemon.py`: `_maybe_set_auth_cookie` does not set
  `HttpOnly`; `Secure` depends on `X-Forwarded-Proto`.
- `gateways/gateway-web/daemon.py`: `_origin_allowed` returns true when no
  allowlist is configured.
- `gateways/gateway-web/daemon.py`: `BrowserSocket` accepts unmasked frames and
  has no size limit.

## Impact

Tokens can leak via browser history, copied URLs, logs, referrers, or script
access. Remote/proxied deployments are especially exposed.

## Proposed Fix

- Replace query token auth with a one-time pairing/bootstrap exchange.
- Store session auth in `HttpOnly; SameSite=Strict` cookies.
- Set `Secure` whenever public URL is HTTPS or remote mode is enabled.
- Require explicit `allowed_origins` for remote mode.
- Use a maintained WebSocket implementation or add frame validation and limits.

## Acceptance Criteria

- Query token is not required after one-time bootstrap and is not used for
  normal API/WebSocket calls.
- Cookie includes `HttpOnly` and correct `Secure` behavior.
- Remote mode refuses wildcard origins unless explicitly configured.
- Tests cover token exchange, cookie flags, origin rejection, and bad frames.
