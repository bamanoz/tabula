# AM-06 — Move Claw prompt policy out of shared driver library

Status: proposed
Type: AFK
Repo: tabula-distrib, tabula-bundles
Labels: needs-triage, area/distro, area/bundles, area/drivers, area/prompt

## Parent

Track: `tabula/docs/issues/agent-manifest/README.md`

## What to build

Move `claw`-specific prompt assembly out of the shared driver library into a
`claw`-owned component.

Current problem:

- `claw/boot.py` no longer emits the final system prompt.
- `tabula_drivers.prompt_builder` in shared `_lib` knows about `SYSTEM.md`,
  `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md`, `IDENTITY.md`, `SOUL.md`, `USER.md`,
  `AGENTS.md`, and `claw` first-run behavior.
- That logic is distro policy and should not live in reusable SDK/shared driver
  code.

Target boundary:

- Shared driver/runtime code may provide provider initialization, context
  injection, history handling, tool visibility plumbing, and a narrow prompt
  builder extension point.
- `claw` owns project file selection, template composition, first-run behavior,
  external skill presentation, and final prompt text.

Proposed component:

```text
claw/application/claw_prompt/
  __init__.py
  builder.py
```

The builder owns:

- `SYSTEM.md`, `TOOLS.md`, `GUIDELINES.md`, `SAFETY.md` composition.
- `IDENTITY.md`, `SOUL.md`, `USER.md`, `AGENTS.md` policy.
- first-run setup text.
- external instruction-only skill presentation.
- provider-specific prompt variants if `claw` needs them.

Shared driver API should become conceptually:

```python
prompt = prompt_builder.build_main_prompt(
    app_id=tenant_id,
    values=values,
    tools=tools,
    context=context,
    provider=provider,
    session=session,
)
```

The shared driver must not know that `AGENTS.md` exists.

## Acceptance criteria

- [ ] No `claw` prompt file names remain hardcoded in shared
      `tabula_drivers.prompt_builder`.
- [ ] `claw` prompt builder can produce the same current prompt shape for the
      existing default install.
- [ ] Driver and subagent runtime load the prompt builder from app/distro
      metadata or client config.
- [ ] `claw` tests cover project-file creation, first-run behavior, and external
      skill presentation from the `claw` prompt component.
- [ ] Shared driver tests cover extension-point behavior without `claw` file
      assumptions.

## Blocked by

- AM-05 for `claw` application contract metadata.

## Notes

- Do not move prompt text into the application manifest. The manifest supplies
  distro-owned values; the distro builds the prompt.
