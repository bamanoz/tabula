# M6-05 — Remote backend operator docs + testbed

Status: partial
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
- [x] Operator docs reviewed for completeness:
      - Every config knob in `runtime.toml` + kernel-side
        `[runtime.endpoints.*]` documented.
      - Token rotation flow documented end-to-end.
      - mTLS cert issuance documented (with or without
        `tabula runtime cert sign` helper from M6-02).
- [x] `docs/SECURITY.md` updated with M6 model.
- [x] Cross-links from `docs/plans/REMOTE_RUNTIME.md` to the
      operator docs added; the plan is no longer the
      operator's reading material — the operator docs are.

## Landed in this slice

- Added operator docs:
  - `docs/operating/remote-runtime-overview.md`
  - `docs/operating/wss-deployment.md`
  - `docs/operating/ssh-deployment.md`
  - `docs/operating/troubleshooting.md`
  - existing `docs/operating/service-install.md` is linked into the remote docs
- Added `docs/SECURITY.md` with the M6 layered security model.
- Added cross-links at the top of `docs/plans/REMOTE_RUNTIME.md` so operators
  are pointed to deployment docs instead of the design plan.
- Added remote-oriented testbed suite entries:
  - `runtime-wss-loopback`
  - `runtime-mtls-loopback`
  - `runtime-ssh-loopback`
  - `runtime-token-revoke`
  - `runtime-multi-backend`
- Added `test_runtime_remote_backends.py`, which runs focused Go loopback tests
  for WSS, mTLS, SSH skip-if-unavailable, token revoke, and mixed backend
  coverage.

## Still open

- Run the new `m6-remote-backends` GitHub Actions job on Linux and macOS and
  record the result.
- Replace or supplement Go-loopback-backed testbed scripts with full installed
  network runtime orchestration if CI can support the process matrix reliably.

## Validation evidence

- `python3 tools/tabula-testbed/src/tabula_testbed_runner/testbed_template/tests/test_runtime_remote_backends.py --tabula-root .`
- `go test ./internal/runtime/transport/wss ./internal/runtime/auth ./internal/runtime/backend/ssh ./internal/runtime/transport/stdio ./cmd/tabula-runtime ./internal/runtime/host/dialer ./cmd/tabula`
- `go test -race ./internal/runtime/transport/wss ./internal/runtime/auth ./internal/runtime/backend/ssh ./internal/runtime/transport/stdio ./internal/runtime/conn ./cmd/tabula-runtime ./internal/runtime/host/dialer ./cmd/tabula`
- `go test ./internal/runtime/transport/wss ./internal/runtime/backend/ssh ./cmd/tabula -run 'TestWatchRuntimeTokenRevocationsDetachesRuntime|TestSSHRuntimeSupervisorsAttachMultipleRuntimes'`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli direct --tabula-root . --source tabula-bundles=../tabula-bundles --suite runtime-wss-loopback --suite runtime-mtls-loopback --suite runtime-ssh-loopback --suite runtime-token-revoke --suite runtime-multi-backend`
- `ruby -ryaml -e 'ARGV.each { |p| YAML.load_file(p); puts p }' .github/workflows/*.yml`
- GitHub Actions job added: `m6-remote-backends` on `ubuntu-latest` and
  `macos-latest`.

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
