# Exec Tool Process And Policy Hardening

Priority: Medium

Repos: `tabula-bundles`, `tabula-distrib`

## Problem

`exec_run` is intentionally powerful but has weak process cleanup and policy
guardrails. Foreground timeout terminates only the shell process, not the whole
process group. The command denylist is substring-based and should not be treated
as a sandbox.

## Evidence

- `workspace/exec/run.py`: `exec_run` launches shell without
  `start_new_session` and kills only the direct process on timeout.
- `workspace/exec/run.py`: background processes use `start_new_session` and
  `killpg`, which is stronger.
- `workspace/exec/run.py`: `deny_commands` checks `pattern in command`.
- `tabula-distrib/code/application/apply.py`: default deny commands are simple
  strings such as `rm -rf /`, `shutdown`, `reboot`.

## Impact

Timed-out foreground commands can leave orphan child processes. The denylist is
easy to bypass and may create a false sense of sandboxing.

## Proposed Fix

- Run foreground commands in their own process group/session on POSIX.
- On timeout, terminate/kill the process group, not only the shell.
- Document `exec` as unsandboxed shell execution.
- Consider optional cwd restriction under project root for distros that want it.
- Keep approvals as the main control, but do not rely on string denylist for
  security.

## Acceptance Criteria

- A foreground command that spawns a child process is fully cleaned up on
  timeout.
- Tests cover child process cleanup.
- Docs/config comments state that `deny_commands` is not a sandbox.
