# M2-06 — `tabula status` subcommand (JSON output)

Status: open
Phase: M2
Type: AFK
Repo: tabula
Labels: needs-triage, area/cli, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2, Q9)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§9)

## What to build

Add `tabula status` Go subcommand returning a snapshot of system
state. Used by `bootstrap.sh` and other shell scripts to detect
whether kernel/runtime are running and what tenants exist.

Per ADR §9, the Go CLI exposes composable primitives only;
`status` is one such primitive. No prompts, no decoration —
data only.

Output shape (`tabula status --json`):

```json
{
  "kernel": {
    "running": true,
    "pid": 12345,
    "socket": "/Users/mak/.tabula/run/runtime.sock",
    "ws_endpoint": "ws://127.0.0.1:7777/ws",
    "uptime_seconds": 1234
  },
  "runtimes": [
    {
      "id": "local",
      "attached": true,
      "pid": 12346,
      "capabilities": ["fs", "exec", "gateway-telegram"]
    }
  ],
  "tenants": [
    {"id": "myproject", "created_at": "2026-05-02T18:00:00Z"}
  ]
}
```

Exit codes:

- `0`: status retrieved successfully (regardless of whether
  kernel is running).
- `2`: cannot determine status (e.g. `$TABULA_HOME` not set,
  permissions error reading state).

Detection mechanism:

- Kernel running: try to connect to `$TABULA_HOME/run/runtime.sock`
  with a `Health` op via Runtime API. Success = running.
  Connection refused or socket missing = not running.
- Alternative: write `$TABULA_HOME/run/kernel.pid` from `tabula
  serve` and check process liveness — pick whichever is more
  robust on macOS+Linux. Likely both: pidfile for fast check,
  socket for confirmation.
- Runtimes attached: queried from kernel via Runtime API
  introspection op (kernel exposes its attached-runtimes list).
  If kernel is not running, `runtimes: []`.
- Tenants: read from `$TABULA_HOME/state/tenants/` directory
  (each tenant has its own subdirectory). M2 has only one
  default tenant; future M4 makes this richer.

Default human output (no `--json`):

```
kernel:    running (pid 12345, uptime 20m)
runtime:   local (pid 12346) — fs, exec, gateway-telegram
tenants:   myproject (1)
```

Decoration-free; no colors, no boxes.

## Acceptance criteria

- [ ] `tabula status --json` outputs the documented shape.
- [ ] `tabula status` (no flag) outputs the documented human
      form.
- [ ] When kernel is not running: `kernel.running: false`,
      `runtimes: []`, tenants still listed from state dir.
- [ ] When kernel is running but no runtime attached:
      `runtimes: []`.
- [ ] Exit code `0` for successful queries (even with kernel
      down).
- [ ] Exit code `2` when state cannot be read.
- [ ] Tested: snapshot tests for both JSON and human output
      formats covering the matrix above.

## Blocked by

- M2-02 (kernel exposes Runtime API; needs to know about
  attached runtimes)
- M2-05 (need to dial as authenticated client to query
  attached runtimes — `tabula status` has FS access to read
  the runtime token, so it can authenticate as if it were a
  local runtime; alternatively kernel exposes a separate
  unauthenticated read-only Health endpoint — choose simpler)

## Notes

- This subcommand exists to keep `bootstrap.sh` simple: shell
  script only needs `jq` to parse `--json` output.
- M4 expands `tenants` shape; design output now to allow
  additive fields without breaking shell consumers (use
  `--json` shape evolution rules: new fields ok, removing
  fields is breaking).
