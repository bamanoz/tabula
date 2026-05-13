# Service Install

Tabula provides generic service scripts for running `tabula serve` under the
host supervisor. Distro-specific wrappers may call these scripts, but kernel
service behavior stays generic in this repo.

## Install

```sh
TABULA_HOME="$HOME/.tabula" scripts/install-service.sh
```

On macOS this writes and loads:

```text
~/Library/LaunchAgents/ai.tabula.kernel.plist
```

On Linux user mode this writes and enables:

```text
~/.config/systemd/user/tabula-kernel.service
```

Linux system mode writes `/etc/systemd/system/tabula-kernel.service` and must be
run as root:

```sh
sudo TABULA_HOME=/opt/tabula scripts/install-service.sh --system
```

The scripts do not run `sudo` for you.

## Uninstall

```sh
scripts/uninstall-service.sh
```

Use `--system` to remove a Linux system service. Uninstall is idempotent: it
exits successfully when the unit is already absent.

## Logs

macOS launchd sends stdout and stderr to:

```text
$TABULA_HOME/logs/kernel.log
```

Linux systemd logs through journald:

```sh
journalctl --user -u tabula-kernel.service
```

For system mode, omit `--user`.

## Status

Readiness is checked with:

```sh
tabula status --json
```

The install script polls for `kernel.running == true` for up to 30 seconds.

## Manual Reboot Check

CI cannot prove reboot survival. To verify manually:

1. Install the service.
2. Reboot the host.
3. Run `tabula status --json` and confirm `kernel.running` is `true`.
4. Check logs at the documented location if the kernel did not start.

## Render Without Installing

For review or tests:

```sh
scripts/install-service.sh --render
scripts/install-service.sh --dry-run
scripts/uninstall-service.sh --dry-run
```

## Host-Gated Integration Test

The repository includes script-level tests that fake `launchctl`/`systemctl` and
verify unit generation plus orchestration. To run host service-manager checks,
use an environment prepared for service installation and set:

```sh
TABULA_SERVICE_INTEGRATION=1 python3 -m unittest scripts.test_service_scripts
```

This hook is intentionally opt-in because it can load/unload host services.
