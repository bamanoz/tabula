# M5-02 — `exec` plugin: cwd-aware subprocess execution

Status: done
Phase: M5
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/bundle, phase/m5

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M5)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

A new plugin `workspace/exec` that owns shell command
execution for the agent. Replaces `tabula-bundles/base/shell/`
skill. Independent of `fs.roots` per the design decision in
the M0 grilling session (Q3.4c): exec is shell, not sandbox
theatre.

Components:

- New plugin: `tabula-bundles/workspace/exec/`:
  - `plugin.toml`: tools `exec_run`, `exec_run_background`,
    `exec_kill_background`, `exec_list_background`.
  - `scripts/run.py` using `tabula_plugin_sdk` (M2-04).
  - Plugin config (`config/plugins/exec/config.toml`):
    ```toml
    cwd_default = "${project_root}"
    timeout_default_seconds = 60
    timeout_max_seconds = 600
    env_passthrough = ["PATH", "HOME", "LANG", "TERM"]
    env_extra = { TABULA_TENANT_ID = "${tenant_id}" }
    deny_commands = ["rm -rf /", "shutdown", "reboot"]    # naive substring match by default
    ```
  - `${project_root}` and `${tenant_id}` config templates
    resolved per-tenant.
- Tool semantics:
  - `exec_run(command, [cwd], [timeout_seconds], [env]) ->
    {stdout, stderr, exit_code, timed_out}`.
    - `cwd` defaults to `cwd_default`. If absolute, used as-is.
      If relative, resolved against `cwd_default`. No
      restriction on whether `cwd` falls under `fs.roots`
      (intentional — see ADR Q3.4c).
    - `command` is the literal shell string passed to
      `bash -c` (or `sh -c` if bash unavailable). Single
      string keeps quoting consistent with what users type.
    - `timeout_seconds` capped at `timeout_max_seconds`.
  - `exec_run_background(command, [cwd], [env]) -> {bg_id}`:
    spawns and returns immediately. Caller polls or kills via
    `bg_id`.
  - `exec_list_background() -> {processes[]}`.
  - `exec_kill_background(bg_id, [signal=TERM])`.
- Background process tracking: plugin keeps an in-process map
  `bg_id → Process`. State lives only as long as the plugin
  worker. On worker restart, background processes are
  abandoned (logged, not migrated). Document this limitation.
- Deny list semantics: substring match on the command string,
  case-sensitive. Crude but predictable. Rejected → structured
  error `{code: "exec_denied", message, matched_pattern}`.
- Tests:
  - Successful run, captured stdout/stderr/exit.
  - Timeout fires → `timed_out: true`, process SIGKILL'd.
  - Background spawn → list shows it → kill works.
  - Deny list match → `exec_denied`.
  - Testbed coverage.

### Subtle: exec independence from fs.roots (Q3.4c restated)

A user running `exec_run("ls /tmp")` is acting like a
terminal user. If `/tmp` isn't a fs root, that's irrelevant —
the agent isn't using fs tools to traverse `/tmp`, it's
running a process. The process can read what its OS user can
read. Anything else is sandbox theatre.

Hard isolation (chroot, cgroups, seccomp) belongs to
PluginExecPolicy (M6, Linux only). On macOS, the runtime
intentionally degrades to "no enforcement", documented.

## Acceptance criteria

- [x] All four tools implemented and unit-tested.
- [x] Timeout behavior verified (process SIGTERM at timeout,
      SIGKILL 5s later if still alive, clean `timed_out:
      true` result).
- [x] Background processes survive across `exec_run` calls
      until killed or worker restarts.
- [x] `${project_root}` and `${tenant_id}` config templates
      resolve correctly per tenant.
- [x] Deny-list rejection is structured.
- [x] No coupling to `fs` plugin (exec works standalone with
      no fs plugin installed).
- [x] Testbed suite green.

## Implementation notes

- Added `workspace/exec` plugin with `exec_run`, `exec_run_background`,
  `exec_kill_background`, and `exec_list_background`.
- `exec_run` uses `bash -c` when `bash` is available and falls back to `sh -c`.
- `cwd` defaults to `cwd_default`; relative `cwd` values resolve under it.
- Config is loaded through `tabula_plugin_sdk.load_plugin_config`, so
  `${project_root}` and `${tenant_id}` resolve per tenant.
- Deny-list failures raise structured `ToolError(code="exec_denied",
  matched_pattern=...)`; kernel-facing testbed output currently surfaces the
  message string.
- Background processes are tracked in the warm plugin worker memory and are
  terminated best-effort on plugin shutdown.

## Validation evidence

- `PYTHONPATH=_lib/python/src:. python3 -m unittest workspace.exec.tests.test_exec_plugin workspace.fs.tests.test_fs_plugin _lib.python.tests.test_contract` in `../tabula-bundles`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m unittest tools.tabula-testbed.src.tabula_testbed_runner.runner_test`
- `PYTHONPATH=tools/tabula-testbed/src python3 -m tabula_testbed_runner.cli run --tabula-root . --source tabula-bundles=../tabula-bundles --suite exec-plugin --bootstrap-check --home /tmp/tabula-m502-exec-2`

## Blocked by

- M2-04 (plugin SDK)

## Notes

- The "string command vs argv list" decision: string. Matches
  the existing `base/shell` skill's UX. Argv-list APIs are
  better but harder for LLMs to produce reliably.
- Background process feature is genuinely new vs the `base/
  shell` skill, justified because `exec` is a long-lived
  warm plugin (per ADR §4) and can hold state. If review
  thinks it's scope creep, it can be cut and shipped in a
  follow-up.
