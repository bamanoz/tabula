# Python plugin SDK concurrent tool calls

Type: SDK / Feature

Priority: P1

Status: Planned

Repos: `tabula-bundles`

## Parent

`docs/issues/parallel-runtime/README.md`

## Blocked by

- `001-tool-execution-policy-metadata.md`

## Problem

The Python plugin SDK serves worker calls synchronously. `PluginAPI.serve()`
handles one `OpCall`, runs the handler inline, writes one result, and only then
reads the next frame. Even if the runtime transport can carry multiple inflight
calls, the Python harness still serializes them.

## What to build

Update the Python SDK so tool calls can execute concurrently within one warm
worker process:

- `OpCall` handlers run in separate worker threads/tasks
- protocol writes are thread-safe
- replies preserve `call_id` correlation
- shutdown stops new calls and resolves inflight calls predictably

First pass can use a thread-per-call model; it does not need a sophisticated
executor yet.

## Acceptance criteria

- [ ] Two tool calls can execute concurrently in one Python worker.
- [ ] A slow call does not block a fast unrelated call from returning.
- [ ] Result frames never interleave or corrupt stdout protocol output.
- [ ] Shutdown does not leave hanging inflight call waiters.

## Files

- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/api.py`
- Edit: `tabula-bundles/_lib/python/src/tabula_plugin_sdk/protocol.py`
- Add/Edit: SDK concurrency tests under `tabula-bundles/_lib/python/tests/`

## Verify

```bash
PYTHONPATH="tabula-bundles/_lib/python/src" python3 -m unittest discover -s tabula-bundles/_lib/python/tests
```

## Risk

Medium.

Thread-safe protocol output is the main risk. Broken writes will corrupt the
worker protocol for all plugins, not only `fs`.

## Notes

- Do not add user-facing concurrency knobs.
- Keep hook handlers on the current execution path for now.
