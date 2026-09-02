# Troubleshooting

## `runtime_unavailable`

The selected runtime disconnected, was revoked, or failed before completing a
tool call.

Check:

```sh
tabula status --json
```

Look at runtime `attached`, `last_error`, and target lifecycle diagnostics.

## Tracing One Turn

When you need to trace one agent turn end-to-end, query the durable session
projection and committed events to find the accepted input and its `turn_id`.
Use the attempt and generation identifiers from subsequent turn events to
correlate kernel, runtime, driver, hook, and tool diagnostics.

```text
turn_correlation_trace {"session":"SESSION_ID","turn_correlation_id":"tc-..."}
```

The trace merges matching history and ledger events so you can correlate:

- the original user turn
- driver lifecycle and usage events
- tool dispatch events
- hook dispatch audit entries
- edit diffs
- compaction boundaries

Structured kernel/runtime logs also include `turn_correlation_id` on tool and
runtime-dispatch paths, so you can cross-reference the same id in log files
without exposing raw prompt contents.

Logging policy is controlled by environment variables. Kernel processes use
`TABULA_LOG_LEVEL`, `TABULA_LOG_FORMAT` or `TABULA_LOG_TYPE`,
`TABULA_LOG_OUTPUT`, `TABULA_LOG_FILE`, `TABULA_FILE_LOG_LEVEL`, and
`TABULA_FILE_LOG_FORMAT` or `TABULA_FILE_LOG_TYPE`. Runtime processes use the
same names with `TABULA_RUNTIME_` prefix, for example
`TABULA_RUNTIME_LOG_LEVEL=debug` and `TABULA_RUNTIME_LOG_TYPE=text`. Levels are
`debug`, `info`, `warn`, `error`, or `silent`; formats are `json` or `text`.

## Tool Result Spool Files Accumulate

`$TABULA_HOME/run/tool-results/*.json` contains ephemeral in-flight tool output.
Normal call completion removes its file, and kernel startup removes orphaned files
left by crashes or forced termination. Do not delete files while calls are active:
that can race result hooks and produce a missing-source error. Durable large tool
results belong to an artifact-owning plugin, not this runtime spool.

## Kernel SQLite Grows Rapidly

`$TABULA_HOME/state/kernel/sessions.db` stores canonical session aggregates,
events, outbox state, and compact command-deduplication boundaries. Current
`commands.result` rows contain only `version` and `cursor`; they must not contain
full session projections or outbox payloads.

To inspect command-row size while kernel is stopped:

```sh
sqlite3 "$TABULA_HOME/state/kernel/sessions.db" \
  'SELECT count(*), sum(length(result)), max(length(result)) FROM commands;'
```

Any other `commands.result` shape is corrupt state and fails closed. Tabula does
not translate alternate representations.

## `tenant_forbidden`

The tenant is not allowed to use the selected runtime. Check tenant config:

```toml
[tenant]
allowed_runtimes = ["local", "laptop"]
default_runtime = "local"
```

## mTLS Handshake Failures

Common causes:

- runtime cert not signed by `client_ca`
- `client_auth = "require"` but runtime has no cert
- client cert Common Name does not match runtime id
- runtime does not trust kernel server certificate CA

Use `openssl s_client` or runtime logs to inspect TLS errors.

## Token Failures

Tokens are issued on the kernel host:

```sh
tabula runtime token issue --runtime-id ID --json
tabula runtime token list --json
tabula runtime token revoke --runtime-id ID --json
```

The token is printed only once at issue time. The on-disk store contains salted
hashes, not plaintext tokens.

## SSH Failures

Run the equivalent SSH command manually:

```sh
ssh -v host tabula-runtime stdio
```

If key auth or known_hosts fails manually, fix SSH first. Tabula deliberately
delegates SSH configuration to OpenSSH.

## Service Failures

macOS logs:

```text
$TABULA_HOME/logs/kernel.log
```

Linux logs:

```sh
journalctl --user -u tabula-kernel.service
```

Check service state:

```sh
tabula status --json
```
