# M3-07 — Kernel routes skill calls through Runtime API

Status: open
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/kernel, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

The kernel-side cutover for skills, parallel to M2-07's plugin
cutover. After this slice the kernel calls
`RuntimeConn.Invoke{target_id="skill:<name>", ...}` for skill
tools instead of going through `process_manager.go`.

`process_manager.go` deletion lands in M3-08.

Components:

- `Hub.RouteToolCall` (or wherever skill tool dispatch lives):
  - For tools whose source is a skill: build
    `Invoke{target: skill.target_id, tool: tool_name, args}`,
    send via `RuntimeRegistry.Pick(tenant_id).Invoke(...)`.
  - Surface `WorkerResult` ok/error back to caller transparently.
- `internal/kernel/skill/` (current location of skill catalog):
  populate skill catalog from runtime's `ListCapabilities`
  response at boot, just like plugin tools (M2-07 already
  established the pattern for plugins). Skills disappear from
  the catalog when the runtime restarts and re-announces.
- Init / discovery: kernel asks runtime for `ListCapabilities`
  on (re)connect. Skill targets land in the same registry as
  plugin targets, distinguished by `target_id` prefix.
- Tool catalog → init handshake: `init.tools[]` content is
  unchanged for the agent client. Source of those tools is now
  always the runtime, never `process_manager.go`.
- `process_manager.go` retained but **unused** for skill exec
  by end of this slice. Deletion is M3-08.

## Acceptance criteria

- [ ] Calling a migrated skill tool (e.g. `timer_start`) from a
      session results in:
      - `RouteToolCall` invokes `RuntimeConn.Invoke`.
      - `process_manager.go` `LocalProcessLauncher.CombinedOutput`
        is NOT called for that call (verify via test spy / log
        assertion).
- [ ] Skill catalog at session init lists every skill known to
      the runtime.
- [ ] Skill that doesn't exist in runtime → kernel surfaces
      `target_unknown` cleanly to caller, does NOT fall back
      to `process_manager.go`.
- [ ] Runtime disconnect mid-skill-call: caller sees
      `runtime_unavailable` (M2 contract) — same fail-fast
      behavior as plugins.
- [ ] Existing skill tests pass against the new path.
- [ ] Race-clean.

## Blocked by

- M2-07 (plugin path established the routing pattern)
- M3-01..06 (runtime can answer skill Invokes correctly)

## Notes

- Per the ADR §4 commit, after this slice the kernel sees no
  difference between plugin and skill calls — both are
  "tools served by some target on the runtime". This is the
  unification.
- Skills do NOT get N:M routing in M3: they execute on the
  tenant's runtime like plugins. Cross-runtime skill execution
  is not a goal.
