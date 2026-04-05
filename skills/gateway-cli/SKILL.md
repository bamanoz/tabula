# gateway-cli

Interactive CLI gateway for terminal use.

Connects to the kernel via Unix socket (`TABULA_SOCKET` env var), reads user
input from `/dev/tty`, and displays streaming responses on stdout.

## Usage

Configured in `tabula.yaml` under `spawn`:

    .venv/bin/python3 skills/gateway-cli/run.py

## Protocol

- Sends: `message`, `cancel`
- Receives: `stream_start`, `stream_delta`, `stream_end`, `done`, `error`

## Requirements

    pip install prompt_toolkit rich

## Environment variables

- `TABULA_SOCKET` — kernel socket path (default: /tmp/tabula.sock)

## Notes

- Reads input from `/dev/tty` directly — works when spawned with piped stdin
- Ctrl+C during streaming sends `cancel` to kernel
- Ctrl+C at prompt or Ctrl+D exits the gateway
- Requires an interactive terminal
