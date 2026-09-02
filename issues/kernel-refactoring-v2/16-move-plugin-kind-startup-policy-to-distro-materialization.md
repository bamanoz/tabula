# Move plugin-kind startup policy to distro materialization

**Type:** AFK  
**Status:** proposed

## What to build

Remove deployment composition policy from the generic runtime host. Distro installation must compile plugin startup dependencies into an opaque, validated runtime plan or explicit target dependency graph consumed mechanically by runtime.

Runtime may topologically execute a supplied graph, but it must not interpret product plugin kinds as a policy vocabulary or derive dependencies from mutable manifests at startup.

## Evidence

- `internal/runtime/host/config.Config` exposes `PluginKinds map[string]PluginKind`.
- `internal/runtime/host/pool` derives startup ordering and readiness from plugin kind names and `depends_on` policy.
- Distro materializers already own installed component composition and runtime config compilation.

## Acceptance criteria

- [ ] Distro installer/materializer is the only component deciding product plugin dependencies.
- [ ] Runtime consumes a generic target dependency plan with validated IDs and cycle errors.
- [ ] Plugin manifests identify capabilities but do not acquire deployment policy authority.
- [ ] Runtime contains no concrete plugin-kind dependency configuration surface.
- [ ] Installed multi-distro startup proves synchronous first-turn catalog readiness.
- [ ] Installer transactions atomically publish the dependency plan with the active generation.
- [ ] Runtime layout docs and `tabula-guide` are updated.

## Blocked by

None - can start immediately.
