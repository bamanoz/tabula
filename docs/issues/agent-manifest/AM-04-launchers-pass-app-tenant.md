# AM-04 — Launchers resolve app id and join with tenant id

Status: proposed
Type: AFK
Repo: tabula
Labels: needs-triage, area/clients, area/kernel, area/tenancy, area/distro-integration

## Parent

Track: `docs/issues/agent-manifest/README.md`

## What to build

Teach launchers and driver clients to select an application id plus kernel id,
connect to that kernel, and pass the application id as the join `tenant_id`.

Current problem:

- Kernel join already supports `tenant_id`.
- Tool invocation already routes by session tenant id.
- Runtime workers already receive `TABULA_TENANT_ID` and `TABULA_TENANT_DIR`.
- The shared main driver and `claw` gateway CLI currently join without
  `tenant_id`, so everything lands in the `default` tenant.

Components:

- App resolver helper used by shell launchers and Python clients:
  - explicit `--app <id>`
  - explicit `--kernel <id>`
  - `TABULA_APP_ID`
  - directory/default binding registry
- Add `--tenant` or `--app` to:
  - unified main driver client
  - subagent driver client where appropriate
  - `claw` `gateway-cli`
- client entrypoints such as `tabula-cli --expected-distro-id tabula.claw`
- Join payload includes `tenant_id`:
  ```json
  {"type": "join", "session": "main-...", "tenant_id": "claw-tabula"}
  ```
- Logs/status should show selected app id/tenant id.
- Kernel URL comes from the selected binding/manifest materialization, not from
  distro prompt or values semantics.

## Acceptance criteria

- [ ] `tabula-cli --expected-distro-id tabula.claw --app claw-tabula` joins sessions under tenant
      `claw-tabula`.
- [ ] `tabula-cli --expected-distro-id tabula.claw --app claw-tabula --kernel local` connects to the selected
      kernel endpoint before join.
- [ ] `TABULA_APP_ID=claw-tabula tabula-cli --expected-distro-id tabula.claw` joins the same tenant.
- [ ] Running from a directory binding selects the bound app and kernel without
      extra args.
- [ ] If no app is selected and no default exists, launcher fails before starting
      the driver with an actionable error.
- [ ] Existing launches without app binding continue to work through `default`
      until the app model becomes mandatory.
- [ ] Tests cover join payload tenant id for main driver and gateway CLI.

## Blocked by

- AM-02 for binding registry.
- AM-03 for kernel/runtime launch orchestration when `app run` is used.

## Notes

- This issue does not require different distros concurrently. It only makes
  clients select the intended tenant/application instance.
- Keep kernel semantics as tenants; do not add workspace/project concepts.
