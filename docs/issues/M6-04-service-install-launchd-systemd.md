# M6-04 — Service install: launchd + systemd

Status: open
Phase: M6
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/cli, phase/m6

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M6)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§9)

## What to build

A `scripts/install-service.sh` that registers `tabula serve`
as a system service: `launchd` on macOS, `systemd --user` (or
system) on Linux. Plus the symmetric `uninstall-service.sh`.

Per ADR §9, this is a shell script using composable Go CLI
primitives, not a Go subcommand.

Components:

- `tabula/scripts/install-service.sh`:
  - Detects platform (`uname`).
  - Generates the service unit file:
    - macOS: `~/Library/LaunchAgents/ai.tabula.kernel.plist`.
    - Linux user: `~/.config/systemd/user/tabula-kernel.service`.
    - Linux system (with `--system` flag): `/etc/systemd/system/
      tabula-kernel.service`.
  - Substitutes paths from the current `tabula` binary
    location and `$TABULA_HOME`.
  - Loads / enables the service.
  - Polls `tabula status --json` until `kernel.running == true`
    (max 30s).
  - Prints next-step hint (e.g. log location, how to inspect).
- `tabula/scripts/uninstall-service.sh`:
  - Stops the service.
  - Removes the unit file.
  - Idempotent — exits 0 if not installed.
- Service unit fidelity:
  - launchd plist: `RunAtLoad=true`, `KeepAlive` only with
    `SuccessfulExit=false` (don't loop on clean shutdown),
    stdout/stderr → `$TABULA_HOME/logs/kernel.log`.
  - systemd unit: `Restart=on-failure`, `RestartSec=5s`,
    `Environment=TABULA_HOME=...`, log via journald.
- New `tabula serve` flag: `--foreground` (or honour absence
  of `--detach`) — the service unit runs the foreground form
  so the supervisor can manage the process directly. No
  daemonization inside `tabula serve`. Document.
- New `tabula service status` Go subcommand (optional,
  recommend):
  - Returns `{installed: bool, running: bool, last_started:
    string, log_path: string}`.
  - Wraps `launchctl list` / `systemctl status`. Output
    purely data; the script consumes it.
  - Skip if review prefers "shell calls launchctl/systemctl
    directly". Recommended for cross-platform consistency.
- Documentation: `docs/operating/service-install.md` (or
  similar — check existing layout) describing the install
  flow, log location, troubleshooting.

## Acceptance criteria

- [ ] Install on macOS: plist created, agent loaded, kernel
      running within 30s. Verified by integration test on a
      macOS CI runner (or documented manual procedure if CI
      doesn't have one).
- [ ] Install on Linux user mode: systemd unit created,
      `systemctl --user is-active tabula-kernel` returns
      `active`. Verified by Linux CI runner.
- [ ] Uninstall reverses install cleanly; re-running uninstall
      is idempotent.
- [ ] `--system` mode on Linux requires root and warns if
      run as non-root.
- [ ] Service survives reboot: kernel is up after host
      restart (not testable in CI; documented manual check).
- [ ] Logs land at the documented location.
- [ ] All composable primitives exist as Go subcommands;
      shell script only orchestrates.

## Blocked by

- M2-06 (`tabula status --json` for readiness check)
- M2-07 (managed-runtime-child means `tabula serve` is the
  whole stack — service installs it directly)

## Notes

- Distros (tabula-distrib/claw) may ship their own service
  install scripts that wrap this one with distro-specific
  branding/paths. Keep this one generic.
- Windows: out of scope. If demand emerges, a future slice
  can add a Windows service variant. ADR doesn't commit
  to it.
- The script intentionally does NOT use `sudo` automatically.
  System mode requires the operator to run with appropriate
  privileges; the script detects and errors out clearly.
