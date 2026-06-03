# fs opt-in and live verification

Type: Runtime / Plugin

Priority: P2

Status: Completed

Repos: `tabula`, `tabula-bundles`

## Parent

`docs/issues/parallel-runtime/README.md`

## Blocked by

- `004-runtime-scheduler-for-execution-groups.md`

## Problem

`fs` is the first real plugin that needs warm-worker concurrency. Read-heavy
tools like `fs_grep` and `fs_read` should not queue behind each other, while
mutating tools like `fs_write` and `fs_edit` must still serialize against reads
and writes.

## What to build

Opt `fs` into execution-group scheduling.

Suggested first policy:

- `fs_read`, `fs_stat`, `fs_list`, `fs_glob`, `fs_grep`
  - `concurrency = "parallel"`
  - `execution_group = "fs-read"`
  - `conflicts_with_groups = ["fs-write"]`
- `fs_write`, `fs_edit`
  - `concurrency = "serial"`
  - `execution_group = "fs-write"`
  - `conflicts_with_groups = ["fs-read", "fs-write"]`

Then verify live that:

- multiple `fs_grep` calls no longer queue behind each other on one worker
- `fs_write` waits until active reads complete
- hook behavior still works as before

## Acceptance criteria

- [x] `fs` manifest declares execution policy explicitly.
- [x] Two live `fs_grep` calls can overlap on one warm `fs` worker.
- [x] `fs_write` does not run concurrently with active `fs_read/fs_grep` calls.
- [x] Existing `before_prompt_build` behavior stays intact.
- [x] Live verification against `gateway-web` shows the previous `fs_grep`
      queue timeout symptom is gone or materially reduced.

## Files

- Edit: `tabula-bundles/workspace/fs/plugin.toml`
- Add/Edit: targeted `fs` manifest or integration tests as needed
- Possibly edit runtime tests in `tabula` for real `fs` scheduling behavior

## Verify

```bash
PYTHONPATH="tabula-bundles/_lib/python/src:tabula-bundles" python3 -m unittest workspace.fs.tests.test_fs_plugin
go test ./internal/runtime/host/pool
```

Live verification:

1. Start one `fs_grep` against `/Users/mak/src/tabula`.
2. Start a second `fs_grep` before the first finishes.
3. Confirm both complete without the old warm-worker queue timeout.
4. Start `fs_write` while a read call is active; confirm it waits or is queued
   rather than racing.

## Risk

Medium.

`fs` is a good first opt-in target, but it mixes heavy reads, writes, and a
prompt-building hook in one plugin, so it is the first real proof that the
execution-group contract is usable.

## Notes

- If live verification still shows poor performance, search-scope tuning is a
  separate follow-up. It should not be baked into this execution-group series.
