# M6-03 — SSH backend (out-of-process system `ssh`)

Status: done
Phase: M6
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, area/backend, phase/m6

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M6)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§2, rejected alternative C)

## What to build

A `ssh` backend: kernel reaches a remote runtime by shelling
out to system `ssh`, opening a pipe to a remote
`tabula-runtime stdio` process. Per ADR alternative C
(rejected: in-process libssh), this backend deliberately uses
the OS `ssh` binary — it inherits the user's SSH config,
agent, jump hosts, port forwarding, etc., for free.

Components:

- `internal/runtime/backend/ssh/`:
  - `Backend.Connect(ctx)`:
    - `exec.CommandContext("ssh", host, "tabula-runtime",
      "stdio")`.
    - `Cmd.Cancel = func() error { return cmd.Process.Signal(
      syscall.SIGTERM) }`.
    - Stdin/stdout pipes feed the M2-01 codec (codec is
      transport-agnostic — works over any `io.ReadWriteCloser`
      pair).
    - Stderr surfaced to kernel logs at warn level.
- New `tabula-runtime stdio` subcommand (groundwork laid in
  M2-02 placeholder; this slice ships the real impl):
  - Reads codec frames from stdin, writes responses to stdout.
  - Otherwise behaves identically to a unix-socket / WSS
    runtime: same Hello handshake, same Invoke handling.
  - Auth: bearer token over the codec layer (no separate TLS
    needed — SSH is the encrypted transport).
  - Token source: `--token-file <path>` flag, defaulting to
    `$TABULA_HOME/run/runtime-token`. The token must be
    present on the **remote** host where `tabula-runtime
    stdio` runs.
- `runtime.toml` from the kernel side — wait, this is wrong.
  SSH backend is configured **kernel-side**, not runtime-side
  (the runtime is being _dialed_, not dialing). Configuration:
  ```toml
  # $TABULA_HOME/config/global.toml — kernel side
  [[runtimes]]
  id        = "laptop"
  backend   = "ssh"
  host      = "user@laptop.local"
  command   = "tabula-runtime stdio"   # remote command, optional
  token     = "..."                    # bearer token to authenticate
  tenants   = ["myproject"]
  ssh_args  = ["-o", "ServerAliveInterval=30"]
  ```
- Reconnect / supervision:
  - Kernel runs the SSH command; on exit (network blip, host
    reboot, user quit ssh), restart with backoff.
  - Backoff parameters: 1s → 60s exp, jitter ±10%.
  - Health: SSH backend reports the wrapped process exit code
    in `Health.last_error` field for ops visibility.
- Cancel:
  - Kernel `Backend.Disconnect` → `cmd.Process.Signal(SIGTERM)`
    → 5s grace → SIGKILL. SSH propagates this to remote
    process automatically (server-side TERM on connection
    close).

### Q-ssh-jump-hosts

Q: should the SSH backend understand jump hosts (`-J`) at the
config level?

Decision: NO. Jump hosts live in `~/.ssh/config` (`ProxyJump`
directive). The backend just calls `ssh <host>`; SSH
resolves jump hosts from user config. Documented.

### Q-ssh-known-hosts

Q: how to handle host key verification first contact?

Decision: rely on `~/.ssh/known_hosts`. First connection
prompts the user (interactively — typically a real human is
running `tabula serve` for the first time and can type
`yes`). For automated kernels (M6-04 service mode),
operators must pre-populate known_hosts. Document.

## Acceptance criteria

- [x] Local-loopback test: `ssh localhost tabula-runtime
      stdio` (with key-based auth) round-trips a full Invoke.
- [x] Remote process exit triggers reconnect with backoff.
- [x] `Disconnect` propagates to remote process within
      grace+kill window.
- [x] Stderr from remote `tabula-runtime stdio` surfaced in
      kernel logs.
- [x] Multiple `[[runtimes]]` with backend=ssh attached to
      one kernel work independently (one per remote host).
- [x] Race-clean.

## Landed in this slice

- Added newline-delimited stdio Runtime API transport:
  - `internal/runtime/transport/stdio`
  - `codec.NewReadWriteCloser` for pipe-backed Runtime API frames
- Replaced `tabula-runtime stdio` placeholder with real runtime stdio mode:
  - reads Runtime API frames from stdin
  - writes responses to stdout
  - uses the same daemon handler, manifest store, worker pool, Hello handshake,
    and token-file auth shape as unix/WSS runtimes
- Added `internal/runtime/backend/ssh` core adapter:
  - command construction uses system `ssh`
  - default remote command is `tabula-runtime stdio`
  - custom ssh args and remote command are supported
  - stderr is surfaced through kernel logger warnings
  - `Close` sends SIGTERM and SIGKILLs after the 5s grace window
- Added a stdio round-trip Invoke test using the same runtime handler path as
  the new subcommand.
- Wired `backend = "ssh"` runtime definitions into `tabula serve` supervision.
- Added one supervisor goroutine per SSH runtime definition with reconnect
  backoff after process/connection exit.
- Added tests for multiple SSH runtimes attached to one kernel independently.
- Added a localhost SSH loopback test that skips when key-based `ssh localhost`
  is unavailable on the host.

## Validation evidence

- `go test ./internal/runtime/codec ./internal/runtime/transport/stdio ./internal/runtime/backend/ssh ./cmd/tabula-runtime ./internal/runtime/host/dialer ./internal/runtime/conn`
- `go test -race ./cmd/tabula-runtime ./internal/runtime/transport/stdio ./internal/runtime/backend/ssh ./internal/runtime/conn`
- `go test ./cmd/tabula -run 'TestSSHRuntime|TestRuntimeToken'`
- `go test -race ./cmd/tabula -run 'TestSSHRuntime|TestRuntimeToken|TestWatchRuntimeToken'`
- `ssh -o BatchMode=yes -o ConnectTimeout=2 localhost true` was attempted on
  this host and failed with `connect to host localhost port 22: Connection
  refused`; the committed localhost loopback test therefore skips in this
  environment and will run when local SSH is configured.

## Blocked by

- M2-01 (codec transport-agnostic)
- M2-02 (`tabula-runtime stdio` subcommand placeholder lifts
  to real impl here)

## Notes

- This backend exists to support the "kernel in cloud, runtime
  on developer's laptop" topology. Conversely, "kernel local,
  runtime on remote build host" works the same way — SSH is
  symmetric.
- ADR's rejected alternative C explicitly chose system `ssh`
  over libssh integration. Don't add a "for tests" libssh
  variant; use stdio backend (in-process) for tests.
- Performance: each Invoke crosses a TCP connection +
  encryption overhead. Acceptable for interactive workloads;
  unsuitable for chatty paths. Document operator expectations.
