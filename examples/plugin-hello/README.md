# Hello Reference Plugin

This is the Phase 3 reference plugin for the Tabula skill/plugin architecture.
It demonstrates:

- `plugin.toml` discovery and runtime launch via the Python plugin SDK.
- `register` with two tools (`hello_ping`, `hello_spawn_child`) and one hook
  subscription (`before_tool_call`).
- `tool_call` → `tool_result` round-trip.
- `event` → `event_reply` hook handling (`ok`, `rewrite`, and `deny`).
- `send` bus emission and `log` metric-field convention.
- graceful shutdown cleanup for a plugin-owned child process group.

Run the live smoke test with:

```sh
scripts/test-plugin-hello.sh
# or a focused Go run:
go test ./internal/kernel/ -run 'TestPluginHello' -count=1 -timeout 30s
```
