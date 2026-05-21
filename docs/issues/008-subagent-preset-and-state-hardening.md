# Subagent Preset And State Hardening

Priority: Medium

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

Subagent typed presets use stale tool names, and subagent registry/result state
is updated without file locking. ACP control sockets use predictable paths under
`/tmp` with socket mode `0600`, but the parent directory permissions are not
explicitly controlled.

## Evidence

- `subagents/subagents/types/explore.toml`, `review.toml`, `fix.toml`, and
  `plan.toml`: allow tools such as `read_file`, `write_file`, `bash`, `grep`,
  and `glob`.
- Installed tools are currently named `fs_read`, `fs_write`, `fs_grep`,
  `fs_glob`, `fs_list`, `exec_run`, etc.
- `subagents/subagents/run.py`: allowed_tools enforcement uses `fnmatch` against
  actual tool names.
- `subagents/subagents/run.py` and `subagents/subagent-acp/run.py`: registry
  JSON read-modify-write has no lock.
- `subagents/subagent-acp/run.py`: socket path is under `/tmp/tabula-subagents`;
  socket itself is chmod `0600`.

## Impact

Typed subagents may be blocked from the tools their descriptions promise. Race
conditions can lose registry updates. Shared temp directory behavior depends on
umask and environment.

## Proposed Fix

- Update preset `allowed_tools` to current tool names.
- Add a validation test that every preset tool pattern matches at least one
  advertised installed tool unless explicitly marked virtual/external.
- Add per-subagent locking around registry read-modify-write.
- Move ACP control sockets under `$TABULA_HOME/run` or enforce a `0700` temp
  directory with owner/mode checks.

## Acceptance Criteria

- Explore/review/fix/plan presets can use the intended installed tools.
- Concurrent subagent parent/child registry updates do not lose fields.
- ACP control socket directory has deterministic restrictive permissions.
- Unit or testbed coverage exercises preset enforcement with installed tools.
