# AM-15 — Claw application materializer

Status: done
Type: AFK
Repo: tabula-distrib
Labels: needs-triage, area/distro-integration, area/claw, area/installer

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Implement the real `claw` distro application materializer.

The materializer maps Claw-owned `[values]` into tenant-local config, prompt/boot
metadata, plugin config, client config, and memory config. The generic installer
should continue to treat `[values]` as opaque distro input.

## Acceptance criteria

- [x] `[values.workspace]` configures Claw workspace/project root through
      Claw-owned config.
- [x] `[values.tools]` configures `fs` roots and `exec` cwd through tenant-local
      plugin config.
- [x] `[values.memory] mode = "project"` uses tenant-local memory state.
- [x] `[values.memory] mode = "shared"` uses an explicit shared path or
      reference.
- [x] `[values.prompt]` is handled by Claw-owned prompt policy, not generic
      installer code.
- [x] No Claw-specific assumptions are added to the Tabula kernel.

## Blocked by

- AM-13.
- AM-14.

## Notes

- This issue likely lives primarily in `tabula-distrib`; this file exists to keep
  the cross-repo implementation track visible from `tabula`.

## Progress

- `tabula-distrib/claw/distro.toml` declares an application contract and
  materializer.
- `tabula-distrib/claw/application/apply.py` maps Claw-owned `[values]` into
  tenant-local workspace, tool, memory, permission, provider, and prompt config.
- `claw_prompt.builder` consumes tenant prompt config for project-file selection
  and missing-file creation policy.
- Focused Claw tests cover materialization, prompt config, and gateway tenant
  join behavior.
