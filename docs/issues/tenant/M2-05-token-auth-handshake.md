# M2-05 — Token auth handshake

Status: done
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/security, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§7)

## What to build

Implement the M2 bearer-token authentication on the Hello
handshake (per Q4). Local backend uses a file-based shared
token; remote backends will reuse the same handshake mechanism
in M6.

Components:

- Wire types: `Hello.token` field already in M1-01; ensure
  `unauthorized` error code exists in the constants set
  (`internal/runtime/wire/`). If missing, add it.
- Kernel side:
  - At `tabula serve` startup: generate random 32-byte token,
    base64url-encoded with `rtk_` prefix.
  - Write to `$TABULA_HOME/run/runtime-token` with permissions
    `0600`; parent dir `0700`.
  - In-memory store: `{token, runtime_id (placeholder "local"
    in M2), created_at}`.
  - On Hello: validate token against in-memory store. Mismatch
    → close connection with `HelloAck{ok: false, error:
    {code: "unauthorized"}}` then close, count a metric.
- Runtime side:
  - At startup: read token from path in `runtime.toml`
    (`token_file = "$TABULA_HOME/run/runtime-token"`).
  - Send in `Hello.token`.
  - On `unauthorized` ack: log clearly, exit non-zero.
- Token file generation is best-effort: if `run/` directory
  cannot be created or written, `tabula serve` fails to start
  with a clear error (do not silently degrade to no-auth).
- Future-proof storage abstraction: kernel side's in-memory
  store has an interface so M6 can swap in
  `$TABULA_HOME/state/runtime-tokens.json` with hashed entries
  and richer metadata.

## Acceptance criteria

- [ ] Correct token → handshake success, runtime appears
      attached.
- [ ] Wrong token → kernel returns `unauthorized`, runtime
      exits non-zero with clear log message.
- [ ] Missing token file on runtime side → runtime fails to
      start with clear error.
- [ ] Token file permissions are exactly `0600`; parent dir
      `0700`. Asserted in test.
- [ ] Token regenerated on every `tabula serve` restart; old
      token rejected after restart.
- [ ] No plaintext token in any kernel log line (log only
      prefix or hash if needed).
- [ ] Race-clean.

## Blocked by

- M2-02 (binary skeleton exists)

## Notes

- Per Q4f, plain token in `runtime.toml` is the M2 trade-off.
  Document it in `docs/plans/REMOTE_RUNTIME.md` (already
  documented; do not duplicate).
- Per Q4d, refresh-on-restart edge case is documented; an
  externally-started runtime must re-read the token file on
  auth fail. M2-02's reconnect loop should re-read the token
  before redialing — implement that behavior here as part of
  this slice.
- mTLS, hashed kernel-side storage, `tabula runtime token
  issue/revoke` CLI — all M6 (Q4 phasing).
