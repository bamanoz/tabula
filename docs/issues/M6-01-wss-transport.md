# M6-01 — WSS transport (kernel listener + runtime dialer)

Status: open
Phase: M6
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/transport, phase/m6

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M6)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§2)

## What to build

The remote-capable transport. Parallel to M2-01's unix socket
transport: same codec, same RuntimeConn, different dial /
listen plumbing. After this slice the kernel can accept
runtime connections from off-host.

Components:

- `internal/runtime/transport/wss/`:
  - `Listener` mounted on the existing kernel HTTP server at
    path `/runtime` (per ADR §2). Reuses the kernel's
    existing TLS plumbing if present, otherwise plain WS for
    M6-01 (TLS gating handled by M6-02 mTLS slice — explicitly
    mark this transport as "needs TLS termination upstream"
    until M6-02).
  - `Dialer` for `wss://host:port/runtime` URLs.
  - HTTP upgrade handshake: standard WS upgrade with
    `Sec-WebSocket-Protocol: tabula-runtime.v1` (subprotocol
    advertises wire-format version; mismatched version →
    close with 1002).
  - Origin check: kernel rejects upgrade requests whose
    `Origin` header doesn't match a configured allowlist
    (default empty → all origins accepted; production setups
    set this).
- `runtime.toml` schema extension:
  ```toml
  [[kernel]]
  id    = "prod"
  url   = "wss://kernel.example.com/runtime"
  token_file = "${TABULA_HOME}/run/runtime-token"
  tenants = ["myproject"]
  # TLS settings (M6-02 will add ca_file, cert_file, key_file)
  tls_insecure_skip_verify = false  # default
  ```
- Kernel `serve.go`:
  - WSS endpoint registration optional, gated by config:
    ```toml
    [runtime.endpoints.wss]
    enabled = true
    listen  = ":7777"          # may match existing kernel HTTP port
    path    = "/runtime"
    origins = []               # allowlist
    ```
  - When disabled, only unix socket (M2-01) is offered.
  - Multiple `[[kernel]]` entries on the runtime side imply
    separate connection pools, one per kernel.
- Backpressure / framing: same as M2-01 (1 MiB max frame,
  60s idle read deadline with WS ping keepalive).
- Reconnect: M2-02's reconnect logic (1s → 60s exp backoff)
  applies unchanged.
- Network failure semantics: Q6 fail-fast contract preserved
  end-to-end. Kernel sees in-flight calls as
  `runtime_unavailable` with `retryable: true` on disconnect.

## Acceptance criteria

- [ ] WSS round-trip integration test with a real HTTP server:
      every Runtime API op succeeds.
- [ ] Origin allowlist enforced; rejected upgrade returns
      403.
- [ ] WS subprotocol mismatch → close 1002 with clear log.
- [ ] WS keepalive ping/pong cycles observable at default
      interval.
- [ ] Mid-call disconnect surfaces `runtime_unavailable`
      retryable.
- [ ] Local unix socket transport (M2-01) and WSS transport
      can co-exist on the same kernel; runtime A on unix,
      runtime B on WSS, both serve the same kernel.
- [ ] Race-detector clean.

## Blocked by

- M2-01 (codec + socket transport — WSS is the parallel)

## Notes

- WSS without TLS is intentionally allowed in this slice for
  test harnesses and local dev. Production deployments must
  put TLS at the listener level (M6-02 makes this first-class
  with mTLS).
- The `ws://` (cleartext) URL form is supported for the same
  reason. Configuration audit is the operator's job.
- ADR §2 commitment: transport is pluggable, codec stays
  unchanged. WSS is just a new plug.
