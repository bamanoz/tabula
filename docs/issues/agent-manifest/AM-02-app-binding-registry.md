# AM-02 — Application binding registry

Status: proposed
Type: AFK
Repo: tabula
Labels: needs-triage, area/installer, area/cli, area/tenancy

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Add a technical binding registry materialized from the runnable manifest. It
selects an application instance and kernel endpoint without encoding distro
semantics such as workspace/project/chat/org.

Bindings answer only: which app id should a launcher use from this context?

Registry shape, conceptually:

```toml
default = "personal-claw"

[[directory]]
root = "/Users/mak/src/tabula"
app = "claw-tabula"
kernel = "claw-tabula"

[[directory]]
root = "/Users/mak/src/acme-api"
app = "coder-acme-api"
kernel = "local"
```

Commands:

```bash
tabula-install app bindings
tabula-install app bind <app-id> --directory <path> --kernel <kernel-id>
tabula-install app bind <app-id> --default --kernel <kernel-id>
tabula-install app unbind --directory <path>
```

If implemented under `tabula-distro` first, use the same nouns under that tool.

Resolution order:

1. Explicit CLI app id.
2. `TABULA_APP_ID`.
3. Nearest parent directory binding for the current working directory.
4. Default binding.
5. No app selected: fail with an actionable message.

## Acceptance criteria

- [ ] Registry lives under `TABULA_HOME`, not inside arbitrary project runtime
      state.
- [ ] Directory binding resolution is deterministic and chooses the nearest
      parent binding.
- [ ] Binding an unknown app id fails.
- [ ] Rebinding an existing directory replaces the old entry atomically.
- [ ] Bindings select both app id and kernel id.
- [ ] `--default` is explicit and visible in audit output.
- [ ] Binding commands require or derive a kernel id; ambiguous kernel selection
      fails before writing registry state.
- [ ] `app bindings` prints both human and JSON output.
- [ ] No field named `scope` or `global` is introduced for application
      semantics.

## Blocked by

- AM-01 for app id/materialized instance records.

## Notes

- A manifest may declare `[bindings.default]` and `[[bindings.directory]]`; the
  installer materializes those into `TABULA_HOME` registry state.
- `default` means fallback selection, not a platform-level user-wide agent
  scope.
