# M6-05 — Remote backend operator docs + testbed

Status: open
Phase: M6
Type: AFK
Repo: tabula
Labels: needs-triage, area/docs, area/testbed, phase/m6

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M6)

## What to build

Operator-facing documentation for remote backends, plus
testbed coverage that validates the full remote flow on real
network transports.

Components:

- New docs under `tabula/docs/operating/`:
  - `remote-runtime-overview.md`: when to use which backend
    (local / SSH / WSS), security model summary, sequencing
    of operator decisions.
  - `wss-deployment.md`: production WSS setup with mTLS,
    cert issuance flow, token rotation, behind-load-balancer
    notes (sticky sessions if any — usually not, runtime
    pins one connection per kernel).
  - `ssh-deployment.md`: developer-laptop scenario, jump
    host, known_hosts setup, ServerAliveInterval, debugging
    SSH errors.
  - `service-install.md`: launchd / systemd flow (M6-04).
  - `troubleshooting.md`: common errors (`tenant_forbidden`,
    `runtime_unavailable`, mTLS handshake failures, expired
    tokens), diagnostic commands.
- Update `tabula/docs/SECURITY.md` (or create) with the
  full M6 security model: layered defense (TLS → cert →
  token → tenant whitelist → worker env check), threat model,
  what mTLS adds over bearer-token-alone.
- New testbed suites:
  - `runtime-wss-loopback`: kernel + runtime both on the
    same host but talking over `ws://localhost:7777/runtime`.
    Full Invoke round-trip. Verifies WSS transport.
  - `runtime-mtls-loopback`: same but with self-signed certs
    on both sides. Verifies mTLS.
  - `runtime-ssh-loopback`: kernel spawns
    `ssh localhost tabula-runtime stdio`. Requires
    SSH key-based auth on CI (set up via test harness, not
    prod keys). Verifies SSH backend.
  - `runtime-token-revoke`: issue token, attach runtime,
    revoke, verify connection drops within 1s.
  - `runtime-multi-backend`: one kernel, three runtimes
    attached over (unix, WSS, SSH) simultaneously, each
    serving a distinct tenant. Invoke routing works
    correctly across all three.
- CI pipeline:
  - Linux CI runs all five suites.
  - macOS CI runs WSS + mTLS + multi-backend (skip SSH if
    the runner can't ssh-to-self; document).
  - Windows CI: skip M6 entirely (out of scope per M6-04).

## Acceptance criteria

- [ ] Five new testbed suites pass on Linux CI.
- [ ] WSS + mTLS + multi-backend pass on macOS CI.
- [ ] Operator docs reviewed for completeness:
      - Every config knob in `runtime.toml` + kernel-side
        `[runtime.endpoints.*]` documented.
      - Token rotation flow documented end-to-end.
      - mTLS cert issuance documented (with or without
        `tabula runtime cert sign` helper from M6-02).
- [ ] `docs/SECURITY.md` updated with M6 model.
- [ ] Cross-links from `docs/plans/REMOTE_RUNTIME.md` to the
      operator docs added; the plan is no longer the
      operator's reading material — the operator docs are.

## Blocked by

- M6-01..04 (everything to document and test)

## Notes

- This issue closes M6 and the entire program. After it
  merges:
  - Local / WSS / SSH backends all ship.
  - mTLS opt-in available.
  - Service install scripts ship.
  - Operator documentation matches reality.
  - Testbed exercises the full matrix on real network
    transports.
- Future work (post-program): Docker / k8s backends. Out of
  scope here per ADR §2 (mentioned as future, not committed
  for M6).
