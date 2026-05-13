# SSH Deployment

The SSH backend lets the kernel reach a remote runtime through the system `ssh`
binary. Tabula does not embed an SSH library and does not parse `~/.ssh/config`.

## Kernel Config

Configure SSH runtimes in `$TABULA_HOME/config/global.toml`:

```toml
[[runtime]]
id = "laptop"
backend = "ssh"
host = "user@laptop.local"
command = "tabula-runtime stdio"
ssh_args = ["-o", "ServerAliveInterval=30"]
```

Fields:

- `id`: runtime id. For mTLS-style identity consistency, keep ids stable.
- `backend`: `ssh`.
- `host`: passed directly to `ssh`.
- `command`: remote command string. Defaults to `tabula-runtime stdio`.
- `remote_cmd`: argv-style remote command alternative.
- `ssh_args`: extra args before `host`, e.g. `-F`, `-p`, or `-o` options.

Jump hosts belong in `~/.ssh/config` via `ProxyJump`; Tabula intentionally does
not model them.

## Remote Host Requirements

Install `tabula-runtime` on the remote host and make it available in the remote
login environment. The remote host also needs a runtime config and token file.
The token file is read by `tabula-runtime stdio` on the remote host.

## Known Hosts

Tabula relies on OpenSSH known_hosts behavior. Interactive first contact may ask
for confirmation. For services or CI, pre-populate known_hosts before starting
`tabula serve`.

## Debugging

Debug SSH separately from Tabula first:

```sh
ssh -v user@laptop.local tabula-runtime --version
ssh user@laptop.local tabula-runtime stdio --help
```

If SSH exits, the kernel supervisor reconnects with exponential backoff. Remote
stderr is copied into kernel warnings.
