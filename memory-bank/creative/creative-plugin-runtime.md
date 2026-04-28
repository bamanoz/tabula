# Creative Phase: Plugin Runtime (Go Architecture)

Status: **FROZEN** 2026-04-26; **SUPERSEDED ADDENDUM** 2026-04-27 for subagent spawn auth/policy ownership; **CURRENT-CYCLE REFINEMENT** 2026-04-27 for blocker-preserving BUILD/SECURITY sequencing.
Scope: Go-side architecture for PluginRuntime, hook integration, Hub API.

## 1. DECISIONS SUMMARY

| Item | Decision | Source |
|------|----------|--------|
| `HookSubscriber` interface | **Adopt (option C)** | D2.16 / Agent 9 |
| `Hub.RegisterPlugin(...)` builder | **Adopt** | D2.14 / Agent 6 |
| `Hub.toolExec` unification | **Single map, source-tagged entries** | D1.7 |
| `hub.SnapshotPlugins()` | **Separate endpoint** | D2.15 / Agent 6 |
| `StartReaper` interaction | **Same reaper covers plugin processes** | D2.9 |
| `CanSpawn`/spawn-token vacuum | **SUPERSEDED 2026-04-27 — subagent-plugin-owned replacement-auth; kernel bridge removable only after green driver/subagent evidence** | skill-plugin-architecture-followups CREATIVE addendum §13 |
| `before_spawn`/`after_spawn` hook fate | **SUPERSEDED 2026-04-27 — not kernel-emitted; reserved/subagent-owned only until a concrete subagent plugin contract exists** | skill-plugin-architecture-followups CREATIVE addendum §13 |
| `bootConfig.Tools` field | **Rename → `bootConfig.Skills`** | D1.10 |
| `PluginProtocolVersion` constant | **In `internal/kernel/protocol.go`** | D2.13 |
| PluginRuntime package | **`internal/kernel/plugin/` subpackage** | D2.6 |

## 2. `HookSubscriber` Interface (D2.16)

Add to `internal/kernel/hook_subscriber.go`:

```go
package kernel

// HookSubscriber is anything that can receive bus hook events.
// Both *Client (WebSocket-attached) and *plugin.Handle (stdio-attached)
// implement this interface.
type HookSubscriber interface {
    Name() string
    Session() string
    IsConnected() bool
    Hooks() []HookSubscription
    SendMsg(*Message)
    Done() <-chan struct{}
}
```

**`*Client` adapter**: add four trivial getter methods to satisfy the
interface (`Name() string { return c.name }`, `Session() string { return
c.session }`, `Hooks() []HookSubscription { return c.hooks }`; `IsConnected`,
`SendMsg`, `Done` already exist).

**Refactor in `hook_engine.go`** (rename `client` → `sub`):
- `hookEntry.client *Client` → `hookEntry.sub HookSubscriber`.
- `RebuildIndex(clients []*Client)` → `RebuildIndex(subs []HookSubscriber)`.
- All `entry.client.X` → method calls (`.Name()`, `.Session()`, etc.).
- `DispatchExcept(..., exclude *Client)` → `DispatchExcept(..., exclude HookSubscriber)`.

**`Hub` change** in `hooks.go`:
- `h.hooks.RebuildIndex(h.allClients())` → `h.hooks.RebuildIndex(h.allHookSubscribers())`.
- New helper `(h *Hub) allHookSubscribers() []HookSubscriber` returns the union of `h.allClients()` (cast to `HookSubscriber`) and `h.plugins.All()` (the new `pluginRegistry`).

**Rationale**: Pseudo-client (option A) conflates supervision domains and
abuses `Add(c, 0)` to bypass `maxClients`. Separate dispatcher (option B)
duplicates priority/pending logic. Interface (option C) is the smallest
abstraction layer that cleanly separates Plugin lifecycle from Client
lifecycle.

## 3. `Hub.RegisterPlugin` Builder Pattern (D2.14)

**Final API shape**:

```go
// In internal/kernel/kernel.go (additive; NewHub signature unchanged)

// RegisterPlugin loads a plugin process from manifest, performs the register
// handshake, and adds it to the Hub's plugin registry. Idempotent on plugin
// id: re-registering replaces the existing plugin (graceful shutdown of old).
//
// Must be called AFTER NewHub and BEFORE StartReaper.
func (h *Hub) RegisterPlugin(manifest plugin.Manifest, config map[string]any) error

// LoadPlugins is a convenience wrapper for boot-time bulk loading.
func (h *Hub) LoadPlugins(entries []plugin.BootEntry) error
```

`NewHub(toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger)` signature
is **unchanged** — all 9 existing callsites remain untouched.

**`bootConfig.Plugins`** in `cmd/tabula/main.go`:
```go
type bootConfig struct {
    Skills  []skillEntry  `json:"skills"`  // renamed from Tools, per D1.10
    Plugins []pluginEntry `json:"plugins"` // new
    // ...
}
type pluginEntry struct {
    Manifest string         `json:"manifest"` // path to plugin.toml
    Config   map[string]any `json:"config"`
}
```

After `NewHub(...)` and before `hub.StartReaper()`, `main.go` calls
`hub.LoadPlugins(boot.Plugins)`. Failures of individual plugins are logged
but do not abort kernel boot (per §2.6 register-fail policy).

## 4. `Hub.toolExec` Unification (D1.7)

**Decision**: Single `toolExec` map, value carries source metadata.

```go
type toolDispatch struct {
    Source   toolSource    // toolSourceSkill | toolSourcePlugin
    Command  string        // for skill tools: exec command from SKILL.md
    Plugin   *plugin.Handle // for plugin tools: handle for CallTool
    Schema   json.RawMessage
}

type toolSource int
const (
    toolSourceSkill toolSource = iota
    toolSourcePlugin
)

// Hub field:
toolExec map[string]toolDispatch
```

`handleDynamicTool` switches on `Source`:
- `toolSourceSkill` → existing `SkillExec.Run` path.
- `toolSourcePlugin` → `entry.Plugin.CallTool(req)` over JSON-RPC.

`update_tools` (per protocol §2.5) atomically replaces all entries whose
`Source == toolSourcePlugin && Plugin == thisHandle`.

**Rationale**: One source of truth for tool name → dispatch route prevents
the "two registries" risk (Risk 7). Tool name collisions are detected at
registration time (last-writer-wins with WARN per protocol §2.5).

## 5. `Hub.SnapshotPlugins()` (D2.15)

```go
// SnapshotPlugins returns a JSON snapshot of currently registered plugins.
// Separate from SnapshotSessions (which is per-session); plugins are
// kernel-level singletons.
func (h *Hub) SnapshotPlugins() []byte

// Output shape:
// {
//   "plugins": [
//     {
//       "id": "mcp",
//       "status": "running" | "restarting" | "failed",
//       "pid": 12345,
//       "restart_count": 0,
//       "last_error": null,
//       "registered_tools": ["mcp__server__foo", ...],
//       "subscriptions": ["before_tool_call", ...],
//       "registered_at": "2026-04-26T10:00:00Z"
//     }
//   ]
// }
```

Exposed via the existing internal debug snapshot endpoint (alongside
`SnapshotSessions`). Not added to Phase 2 BUILD critical path; integration
test in Phase 2 D2.5 may use it for assertions.

## 6. `StartReaper` Interaction (D2.9)

**Decision**: Same `StartReaper` goroutine in `Hub` reaps both
session-spawned children AND plugin processes. PluginRuntime registers each
plugin's PID with the existing `ProcessSupervisor` so `waitpid` cleanup is
unified.

**Rationale**: Two reaper goroutines would race on `waitpid` syscalls. PG-based
`killpg` is orthogonal to reaping (kill sends signal, reaper waits status).

## 7. `CanSpawn` / Spawn-Token Vacuum (D1.11)

> **SUPERSEDED 2026-04-27** by §13, “Subagent Plugin Spawn Auth and Policy Ownership.”
> The old dead-code-keep decision remains accurate historical context, but it is no longer the current target architecture for this follow-up. BUILD must follow §13 for deletion gates and public API/env/event status.

**Decision**: **Option (b) — keep as dead code with skip markers** until
subagent plugin GA in `tabula-bundles`.

Concretely:
- `policy.go::CanSpawn`, `SpawnTokenStore`, `generateSpawnToken`, `Hub.MaxChildren`, `Hub.MaxSpawnDepth` — **kept** in code, but no callsites remain after Phase 1 D1.2 (handleSpawn deletion).
- TODO comment added at each definition: `// TODO(skill-plugin-arch): unused after kernel cleanup; remove once subagent plugin GA in tabula-bundles restores invariant.`
- Tests `TestSpawnDeniedAtMaxChildren`, `TestSpawnTokenPropagatedInEnv`, `TestSpawnTokenOneTimeUse`, `TestSpawnTokenExpiry`, `TestExpiredSpawnTokenRejectedOnConnect` are marked `t.Skip("dead-code-keep until subagent plugin GA — see tasks.md D1.11(b)")` rather than deleted.
- `TestSecurityHookTimeoutBlocksSpawn` (the canonical `before_spawn` integration test) — same `t.Skip` treatment.

**Rationale**: hard-delete (option a) creates a window with NO kernel-level
spawn cap that affects all spawning skills/plugins. Exposing via PluginAPI
(option c) violates §4.5 "kernel does not know". Dead-code-keep buys
correctness with a clear cleanup task.

## 8. `before_spawn` / `after_spawn` Hook Fate (D1.8 / D1.9)

> **SUPERSEDED 2026-04-27** by §13. The kernel does not own or emit spawn events after the bridge is removed. The names are reserved/historical unless the external subagent plugin documents and tests a plugin-owned event surface.

**Decision**: **Keep registry entries** in `HookEvents` (`hooks.go:52-53`)
**but remove all kernel-side dispatch sites** (which go away naturally with
`handleSpawn` deletion in D1.2).

- Plugins MAY still subscribe to `before_spawn`/`after_spawn` via `register.subscriptions[]`; the subagent plugin will dispatch these events itself once it lands in bundles.
- Kernel never emits these events from its own code paths.
- Migration guide D5.1 documents this clearly: external hook-skill authors who relied on these events must migrate to subscribing through the future subagent plugin.

**Rationale**: keeping the registry entries preserves the event names as a
public contract for the subagent plugin migration. Removing them would
require coordinated bumps in bundles repo.

## 9. `bootConfig.Tools` → `bootConfig.Skills` Rename (D1.10)

**Decision**: Rename the JSON field `tools` to `skills` in
`cmd/tabula/main.go`'s `bootConfig`. Plugin tools enter via the parallel
`plugins` field (per §3 above) and register through `Hub.LoadPlugins`.

**Backward compat**: read both `skills` and legacy `tools` for one release
cycle (boot loader prefers `skills` if present, falls back to `tools` with a
WARN log). Migration guide D5.1 instructs distros to update boot JSON.

**Rationale**: "Tools" was overloaded — both kernel-builtin tools and skill
exec entries used the same word. After kernel cleanup, the field exclusively
contains skill exec specs; "Skills" is the accurate name.

## 10. `PluginProtocolVersion` Constant Placement (D2.13)

In `internal/kernel/protocol.go`, immediately below the existing
`ProtocolVersion`:

```go
const ProtocolVersion = 1       // WebSocket wire protocol (kernel ↔ client)
const PluginProtocolVersion = 1 // stdio JSON-RPC protocol (kernel ↔ plugin)
```

Both versioned independently (see protocol creative §2.2 bump policy).

## 11. PluginRuntime Package Layout (D2.6)

New subpackage `internal/kernel/plugin/`:

```
internal/kernel/plugin/
  manifest.go     // PluginManifest struct, plugin.toml parser
  runtime.go      // PluginRuntime interface, default implementation
  handle.go       // Handle struct: ID(), Send(), CallTool(), Shutdown(), Hooks(), Done()
  protocol.go     // NDJSON framing, message types, marshal/unmarshal
  registry.go     // pluginRegistry (Add/Remove/All/Get)
  supervisor.go   // restart/backoff loop using parent kernel ProcessSupervisor
  handle_hooksub.go // adapter implementing HookSubscriber for *Handle
```

`Hub` holds `*plugin.Registry` (new field `plugins`). `pluginRegistry` is
intentionally separate from `ClientRegistry` (no `maxClients` cap, no
numeric id allocation).

Reference: `*plugin.Handle` implements `kernel.HookSubscriber` directly:
- `Name() string` → manifest id
- `Session() string` → `""` (kernel-level singleton; matches `client.session == ""` global subscriber semantics)
- `IsConnected() bool` → process alive AND register completed
- `Hooks() []HookSubscription` → from register reply
- `SendMsg(*Message)` → marshal as `event` JSON-RPC, write to stdin pipe
- `Done() <-chan struct{}` → closed on process exit

## 12. ACCEPTANCE CRITERIA (BUILD)

- [ ] `HookSubscriber` interface introduced; `*Client` and `*plugin.Handle` both implement it.
- [ ] `HookEngine` refactor: `client → sub` rename; tests pass without modification.
- [ ] `internal/kernel/plugin/` subpackage created with manifest parser + runtime + registry.
- [ ] `Hub.RegisterPlugin` / `LoadPlugins` API; `cmd/tabula/main.go` calls `LoadPlugins` between `NewHub` and `StartReaper`.
- [ ] `bootConfig.Skills` field with backward-compat `tools` fallback.
- [ ] `Hub.toolExec` migrated to `map[string]toolDispatch`; `handleDynamicTool` switches on `Source`.
- [ ] `Hub.SnapshotPlugins()` exposed.
- [ ] `CanSpawn`/spawn-token tests marked `t.Skip` with TODO references.

## 13. Subagent Plugin Spawn Auth and Policy Ownership — Supersession Addendum (2026-04-27)

### 13.1 PROBLEM DEFINITION

- What needs to be designed: the ownership boundary for child-spawn authentication, spawn policy, depth accounting, child limits, hook/event semantics, and public SDK/docs surfaces before BUILD removes the D1.11(b) kernel spawn bridge.
- Constraints:
  - This task has `Intent=implement`, so the design must unblock concrete repo-local hardening while preventing unsafe deletion of compatibility bridges.
  - External `tabula-bundles` evidence is not present in this repo; driver/subagent plugin migration remains matrix-gated.
  - Kernel builtins are being removed from the advertised tool catalog; docs/tests must not preserve generic spawn promises by accident.
  - `creative-plugin-protocol.md` §2.9 already assigns plugin-grandchild process-group cleanup to plugin SDK/runtime, but process ownership does not by itself define child authentication.
  - Existing source still contains kernel token/depth surfaces until BUILD gates are satisfied: `PolicyEngine.CanConnect`, `PolicyEngine.CanSpawn`, `SpawnTokenStore`, `generateSpawnToken`, `ProcessManager.Spawn`, `Hub.MaxChildren`, `Hub.MaxSpawnDepth`, and skipped kernel spawn tests.
- Success criteria:
  - One authoritative decision for `api.spawn`, `TABULA_SPAWN_TOKEN`, `tabulaSpawnToken()`, `before_spawn`, `after_spawn`, stale builtin tool constants, and `DEFAULT_KERNEL_TOOLS`.
  - A deletion checklist maps every kernel spawn bridge symbol/test to replacement plugin-side evidence or a retained blocker.
  - BUILD lanes are unambiguous: safe repo-local hardening may proceed; D1.11(b) and Phase 6 deletion remain evidence-gated.
  - Active docs and SDK packages do not advertise stable generic child-spawn or removed builtin-tool contracts unless those semantics are implemented and tested.
- Non-functional requirements:
  - Security: replay resistance, expiry/revocation, parent/session binding, cross-session denial, depth/cap enforcement, fail-closed malformed replies.
  - Maintainability: kernel remains a dynamic tool/hook router, not the spawn policy engine for subagents.
  - Migration safety: stale public surfaces become reserved/private/deprecated rather than silently live.
  - Testability: all retained child-spawn behavior must have plugin-side integration or linked external evidence before kernel bridge deletion.

### 13.2 OPTIONS

#### Option A: Token-retained plugin model

- Description: keep the literal spawn-token model but move token issuance/storage/expiry/depth/MaxChildren ownership into the subagent plugin. Children receive a plugin-issued credential through `TABULA_SPAWN_TOKEN` or a plugin-private side channel. The kernel only accepts child connections if a plugin-authenticated handoff still requires kernel participation.
- Architecture: subagent plugin becomes the policy authority; kernel bridge code is deleted after plugin tests prove equivalent token semantics. SDK/docs scope token helpers to subagent children only.
- Advantages:
  - Closest migration path from existing D1.11(b) tests.
  - Easier to preserve one-time/expiry test vocabulary.
  - Allows temporary compatibility for external child clients already expecting an env token.
- Disadvantages:
  - Keeps `TABULA_SPAWN_TOKEN` visible and risks being mistaken for a generic runtime variable.
  - May require residual kernel `MsgConnect` token consumption, delaying complete kernel bridge deletion.
  - Perpetuates an env-secret surface with leakage/logging risk unless tightly documented.
- Risk factors:
  - Stale SDK helpers (`tabulaSpawnToken()`) could become public API by inertia.
  - Token ownership split between plugin issuance and kernel consumption can recreate ambiguous policy boundaries.

#### Option B: Replacement-auth subagent-owned model (selected)

- Description: treat child spawning as a subagent-plugin capability, not a generic kernel/plugin SDK feature. The subagent plugin defines a private authenticated parent-child channel or credential that is replay-resistant, revocable/expiring, bound to parent/session/depth, and cleaned up on child exit/shutdown. The literal `TABULA_SPAWN_TOKEN` environment variable is not a stable public contract; it may be used only as an implementation-private compatibility detail during external migration and must not appear in common runtime docs or public SDK helpers.
- Architecture: kernel exposes dynamic plugin tools and generic hook/event routing only. The subagent plugin owns spawn API shape, child credential issuance/validation, depth and MaxChildren accounting, child lifecycle/list/kill semantics, and any spawn-specific event emission. Kernel spawn bridge code is removable once the external driver/subagent matrix row proves the plugin replacement and tests are green.
- Advantages:
  - Cleanest separation of concerns: kernel no longer knows child-spawn policy.
  - Avoids freezing a generic `api.spawn` before process/auth semantics are fully productized.
  - Matches `creative-plugin-protocol.md` §2.9 two-tier process ownership and the migration goal to remove kernel builtin spawning.
  - Lets active docs be honest: generic plugin authors get tool/hook APIs, while subagent spawning is a specialized plugin capability.
- Disadvantages:
  - Requires external subagent plugin evidence before D1.11(b) deletion; repo alone cannot mark that lane complete.
  - Existing token-specific tests must be rewritten as replacement-auth invariant tests, not mechanically moved.
  - Consumers expecting `api.spawn` need migration guidance rather than an immediate stable replacement.
- Risk factors:
  - If BUILD only removes docs without external plugin coverage, spawn invariants could disappear; matrix gates must stay hard.
  - The private credential name/mechanism must be documented enough for SECURITY without becoming public generic API.

#### Option C: Stable generic `api.spawn` SDK API

- Description: expose child spawning as a first-class generic plugin SDK method (`api.spawn(cmd,args)`) with kernel- or SDK-enforced auth, depth, MaxChildren, cleanup, and hook semantics.
- Architecture: SDK and kernel/plugin protocol gain stable spawn semantics; docs and tests assert process-group, child-auth, replay/expiry, session binding, cancellation, and security-hook behavior for all plugin authors.
- Advantages:
  - Strong discoverability for plugin authors.
  - Could support future non-subagent plugins needing managed child processes.
  - Provides a direct replacement for old examples that mention `api.spawn`.
- Disadvantages:
  - Largest new public surface and highest test/security burden.
  - Contradicts the follow-up's cleanup goal by reintroducing generic spawn policy during bridge removal.
  - Requires design beyond available external evidence and beyond the minimal packaged SDK baseline.
- Risk factors:
  - Prematurely freezing API semantics would make later subagent implementation changes breaking.
  - Security review scope expands substantially.

### 13.3 ANALYSIS

| Criterion | Weight | Option A: Token-retained plugin | Option B: Replacement-auth subagent-owned | Option C: Stable generic API |
|-----------|--------|----------------------------------|-------------------------------------------|------------------------------|
| Separation of concerns | 5 | 3 | 5 | 2 |
| Migration safety | 5 | 4 | 4 | 2 |
| Security invariant strength | 5 | 4 | 4 | 3 |
| Public API minimality | 4 | 3 | 5 | 1 |
| Testability / evidence fit | 4 | 4 | 4 | 2 |
| External dependency honesty | 3 | 3 | 5 | 2 |
| Developer-doc clarity | 3 | 3 | 4 | 3 |
| **Weighted Total** | | **93** | **126** | **55** |

Scoring scale: 1–5 where 5 is best fit for this follow-up.

### 13.4 DECISION

**Selected: Option B — Replacement-auth subagent-owned model.**

Justification: this is the only option that both unblocks the intended kernel bridge removal and avoids accidentally creating a new stable generic spawn API without external subagent evidence. It preserves the core safety invariants from D1.11(b) — replay resistance, expiry/revocation, parent/session binding, depth derivation, MaxChildren, cross-session denial, and cleanup — but moves their ownership to the only component that will still perform subagent spawning: the external subagent plugin. The kernel's responsibility narrows to validated dynamic tool registration, hook routing, local diagnostics, and fail-closed plugin protocol handling.

Trade-offs accepted:
- D1.11(b) deletion remains blocked until the driver/subagent external matrix row is green or explicitly owner-approved `not-applicable` with rationale.
- Active docs must remove or reserve some previously advertised conveniences (`api.spawn`, `TABULA_SPAWN_TOKEN`) before a replacement public API exists.
- Token-specific tests become invariant tests for the replacement credential/channel rather than one-to-one source moves.

### 13.5 PUBLIC / PRIVATE / RESERVED SURFACE DECISIONS

| Surface | Current decision | BUILD doc/API/test action |
|---|---|---|
| Generic plugin `api.spawn` | **Reserved/future only; not a stable generic SDK API in this task.** Subagent plugin may expose its own tool/API later, but generic plugin SDKs should not promise spawn. | Remove active authoring-doc claims that `api.spawn(cmd,args)` is available. Mark historical plan examples as historical/superseded or update them. Supersede `creative-sdk-and-distro.md` §2's `api.py` `spawn` item: packaged SDK baseline is register/tool_call/event/send/log/update_tools/shutdown, not spawn. |
| `TABULA_SPAWN_TOKEN` | **Not a common runtime variable and not a public SDK contract.** If an external subagent plugin temporarily uses this literal name internally, it is subagent-private and must document expiry/replay/session semantics in subagent-specific evidence. | Remove from common environment docs. Remove public TS `tabulaSpawnToken()` helper unless the external subagent plugin explicitly retains the literal env var and scopes helper docs/tests to private child auth. Do not add Python parity. |
| Replacement child credential/channel | **Subagent-plugin-owned private contract.** Must be replay-resistant, expiring/revocable, parent/session/depth bound, cross-session denied, and cleaned on exit/shutdown. | External driver/subagent matrix row must link tests/evidence for these invariants before kernel bridge deletion. SECURITY reviews credential exposure even if private. |
| `before_spawn` / `after_spawn` | **Not kernel-emitted after bridge removal. Reserved/subagent-owned names only.** They may remain as reserved event constants if needed for compatibility, but active docs must not imply the kernel emits them generally. | Plugin subscription validation should accept only the chosen event set. If names remain accepted, docs say “reserved for subagent plugin; no kernel emission.” If the subagent plugin emits them, tests must cover timeout/deny/fail-closed semantics there. |
| Stale builtin constants (`TOOL_SHELL_EXEC`, `TOOL_PROCESS_*`) | **Deprecated compatibility strings only, not live/default advertised tools.** | Future packaged SDKs should omit them from default public contract. Temporary support dirs may keep deprecated literals only with tests proving they are not `DEFAULT_KERNEL_TOOLS` or advertised. |
| `DEFAULT_KERNEL_TOOLS` | **Empty / absent in active SDK packages.** Kernel default metadata advertisement is empty. | Change tests that assert four builtins. Add tests for empty defaults and dynamic skill/plugin tool registration as the only live tool source. |
| `before_tool_call` / existing plugin hook events | **Still kernel-owned generic hook surface.** | Keep and validate. Malformed `event_reply` for security hooks must fail closed or route to existing fail-closed timeout behavior, never default pass. |

### 13.6 IMPLEMENTATION GUIDELINES

- Kernel boundary: after replacement evidence is green, remove kernel child-spawn policy/credential issuance. Kernel should not own subagent depth or MaxChildren once subagent spawning leaves kernel builtins.
- Subagent plugin boundary: owns spawn/list/kill/cancel child lifecycle, replacement child credential/channel, depth derivation, MaxChildren accounting, parent/session authorization, and cleanup on child exit/kernel/plugin shutdown.
- SDK boundary: packaged SDKs expose plugin protocol primitives only. No generic `spawn` helper unless a future design explicitly accepts Option C-level obligations.
- Docs boundary: active docs are normative; `docs/plans/**` may be historical only if clearly marked. Common runtime docs must not list subagent-private credentials.
- Event boundary: `before_spawn`/`after_spawn` are not kernel events. Treat them as reserved/subagent-owned names or remove from active generic hook lists; do not accept malformed plugin replies as permissive hook outcomes.
- Diagnostics/security boundary: `/internal/snapshot/plugins` remains local-only and unauthenticated; any remote diagnostics require a future explicit authenticated design.
- Builtin metadata boundary: stale kernel builtin names may warn-and-empty for compatibility, but no boot path or SDK default may re-advertise removed tools.
- Manifest/protocol validation boundary: invalid `tools[].exec`, invalid plugin `register`/`update_tools`, malformed `event_reply`/`tool_result`, and invalid distro bundle skill manifests fail before advertisement/promotion or remain side-effect-free.

### 13.7 D1.11(b) DELETION CHECKLIST

BUILD may delete a row only after the replacement requirement is satisfied and linked in the external matrix or local tests. Unknown/blocker matrix rows keep the row blocked.

| Kernel surface / test class | Replacement evidence required | Disposition |
|---|---|---|
| `PolicyEngine.CanSpawn` | Subagent plugin policy tests prove at-limit denial, below-limit allowance, and per-parent/session authorization. | Delete after driver/subagent row green. |
| `Hub.MaxChildren`, `Hub.MaxSpawnDepth` | Plugin tests prove depth derivation, root/default depth handling, MaxChildren per parent/session, independence across sessions, and release-on-exit/kill. | Delete after green evidence; no kernel config should still read these as live spawn policy. |
| `SpawnTokenStore`, token one-time/expiry tests | Replacement credential/channel tests prove replay resistance, expiry/revocation, invalid/expired denial, pruning/cleanup, and parent/session binding. | Delete if replacement-auth evidence green; retain as blocker if external plugin still depends on kernel token consumption. |
| `generateSpawnToken`, `ProcessManager.Spawn` env injection | Child auth propagation test proves children receive the replacement credential/channel needed to reconnect or operate under the subagent plugin at the correct depth. | Delete kernel env injection; remove public `TABULA_SPAWN_TOKEN` docs/helpers. |
| `PolicyEngine.CanConnect` token branch / `MsgConnect` token handling | External design evidence proves either no child connection token path remains in the kernel or the residual kernel check is a generic authenticated connection gate independent of spawn policy. | Do not partially delete if child clients still need kernel token validation; document residual responsibility. |
| Kernel spawn lifecycle/list/kill/cancel tests | Subagent/gateway plugin integration tests prove stable child handles, list/kill/cancel scoping, crash/reaper notifications, and cross-session denial. | Relocate if behavior remains product behavior; otherwise mark matrix row `not-applicable` with owner rationale. |
| Shutdown child cleanup | Plugin supervisor/runtime tests prove outstanding children are terminated within timeout and no orphan remains. | Relocate to plugin tests. |
| `before_spawn` security timeout test | If subagent emits spawn events, plugin tests cover timeout/deny fail-closed behavior. If replacement policy does not emit these events, tests cover the actual policy gate fail-closed behavior. | Convert or delete based on external subagent design; kernel registry coupling removable after docs align. |
| Sender-skip self-hook for kernel `process_spawn` | Generic `before_tool_call` sender-skip coverage remains for dynamic tools. | Delete kernel-builtin-specific test. |
| `internal/kernel/skip_helpers_test.go` skip markers | All skipped tests are relocated, converted, or explicitly deleted with rationale. | Delete only after row-by-row map is complete. |

### 13.8 BUILD LANE GATES

1. **Repo-local safe hardening may proceed independently**: warn-and-empty legacy builtin metadata; local-only `/internal/snapshot/plugins`; active docs wording that removes false generic spawn/token/event promises; boot `tools[].exec` validation; plugin catalog and inbound reply validation.
2. **External evidence lane may remain incomplete**: keep updating `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`; unknown/blocker rows are honest blockers, not failures of repo-local hardening.
3. **D1.11(b) deletion is gated**: requires this selected design plus green driver/subagent evidence proving the replacement-auth invariants and lifecycle tests above. If not green, retain kernel bridge deletion tasks as open blockers rather than partially deleting policy/auth code.
4. **Phase 6 SDK/lib deletion is gated**: requires green Python wheel, TypeScript tarball, and bundle `_lib` install rows before removing `skills/_pylib`, `skills/_tslib`, distro preserve behavior, inline `_*` compatibility, script/package references, top-level docs, and contract-test references.

### 13.9 VALIDATION IMPLICATIONS FOR BUILD / SECURITY

- Kernel boot validation: reject missing/blank/duplicate skill `tools[].name` or `tools[].exec` before advertisement; dynamic skill/plugin tools are the only live tool source.
- Distro validation: validate `SKILL.md` advertised tools in staging; invalid bundle skill manifests must fail atomically without generation/lock promotion or partial runtime-surface copies.
- Plugin register/update validation: validate dynamic tool names, duplicates, deadlines, subscription event names, and selected event contract before mutating handle or dispatch maps. Invalid register is non-restartable and state-clean; invalid `update_tools` keeps prior catalog intact.
- Inbound reply fail-closed behavior: malformed or unknown `event_reply.action` for security hooks must not synthesize pass; invalid `tool_result`, `send`, or `log` payloads must be rejected or side-effect-free according to the protocol matrix.
- Docs/SDK grep gates: outside historical Memory Bank/archive and explicitly marked historical plan docs, remaining `api.spawn`, `TABULA_SPAWN_TOKEN`, `tabulaSpawnToken`, `before_spawn`, `after_spawn`, `DEFAULT_KERNEL_TOOLS`, and `TOOL_PROCESS_*` hits must align with §13.5 or be blockers.

### 13.10 OPEN BLOCKERS / ASSUMPTIONS

- Assumption: no stable generic spawn API is required for this task's repo-local hardening lane.
- Blocker: external `tabula-bundles` driver/subagent plugin evidence is required before D1.11(b) bridge deletion can be marked complete.
- Blocker: packaged SDK artifacts (`tabula_plugin_sdk-*.whl`, TS tarball) and `_lib` install evidence are required before Phase 6 legacy support-dir deletion.
- Blocker: if external subagent design insists on literal `TABULA_SPAWN_TOKEN` with kernel-side validation, BUILD must retain and document the minimal residual kernel connection responsibility instead of deleting it under the replacement-auth assumption.

## Rubric Review — 2026-04-27 Supersession Addendum

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 8
    failure_isolation: 8
    constraint_fit: 9
    simplicity: 8
  ai_slop_flags: none
  verdict: PASS
  notes: Selected design keeps generic kernel/plugin surfaces minimal while preserving hard security invariants as subagent-plugin evidence gates before bridge deletion.
```

## Rubric Review

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    surface_minimality: 8
    composability: 9
    invariant_strength: 8
    migration_safety: 9
    testability: 9
  ai_slop_flags: none
  verdict: PASS
  notes: HookSubscriber + builder pattern combination keeps NewHub signature stable (zero test churn) while cleanly separating supervision domains. Single toolExec map prevents dual-registry drift.
```

## 14. Current-Cycle Runtime Follow-up Refinement — Blocker-Preserving BUILD/SECURITY (2026-04-27)

### 14.1 PROBLEM DEFINITION

- What needs to be designed: a precise BUILD/SECURITY sequencing contract for the reopened `skill-plugin-architecture-followups` task now that repo-local hardening is already present but external `tabula-bundles` evidence remains absent.
- Constraints:
  - Preserve already-landed repo-local hardening as baseline behavior: empty live builtin metadata, warn-and-empty stale `kernel_tools`, local-only plugin diagnostics, boot/distro `tools[].exec` validation, plugin catalog/reply fail-closed validation, and active docs/SDK surface alignment.
  - Do not turn external migration matrix rows green without concrete external paths, validation commands, and linked commits/PRs or release artifacts.
  - Do not delete D1.11(b) spawn-token/MaxChildren/depth bridge code or skipped kernel spawn tests unless driver/subagent replacement evidence is green or owner-approved `not-applicable` with replacement coverage.
  - Keep `GET /internal/snapshot/plugins` local-only by default; authenticated non-local diagnostics are out of scope for this task.
  - Treat SDK/lib physical removal as gated by packaged Python wheel, TypeScript tarball, and bundle `_lib` install evidence.
- Success criteria:
  - BUILD can proceed without reopening broad architecture questions.
  - If no new external evidence exists, BUILD performs only evidence-safe verification/regression preservation and records blockers honestly.
  - SECURITY has concrete review targets for diagnostics locality, plugin fail-closed validation, matrix state changes, and any conditional bridge deletion.
- Non-functional requirements: security locality, migration safety, evidence traceability, minimal churn, and testability of all retained invariants.

### 14.2 OPTIONS

#### Option A: Delete compatibility bridges from current repo-local state

- Description: treat current repo-local hardening as sufficient and remove D1.11(b) dead code, skipped tests, and SDK support dirs immediately.
- Architecture: kernel loses remaining spawn-token/depth bridge code; distro and temporary SDK support dirs are removed in the same BUILD cycle.
- Advantages: fastest cleanup of stale code; fewer deprecated symbols remain.
- Disadvantages: violates external evidence gates; risks dropping child-auth/depth/MaxChildren invariants before the subagent plugin proves replacements; can break consumers still depending on temporary support dirs.
- Risk factors: false completion claims and security regression through unverified process-auth removal.

#### Option B: Regression-preservation plus matrix-gated conditional cleanup (selected)

- Description: treat repo-local hardening as already-landed baseline, run focused regression verification, and perform deletion only for rows whose canonical external matrix evidence turns green in this cycle.
- Architecture: kernel remains the dynamic tool/hook/plugin router with local diagnostics and fail-closed protocol validation; subagent spawn and SDK relocation cleanup remain conditional lanes keyed to matrix evidence.
- Advantages: preserves safety invariants; aligns with PLAN constraints; avoids broad refactoring; keeps external blockers visible rather than hiding them behind source deletion.
- Disadvantages: some deprecated bridge code and support dirs remain; task may end with open blockers if external artifacts are still unavailable.
- Risk factors: BUILD agents must resist interpreting archived repo-local work as current external evidence.

#### Option C: Expand scope to design authenticated remote diagnostics and package relocation now

- Description: add a secure non-local diagnostics API and in-repo package-relocation migration design even without external bundle artifacts.
- Architecture: introduces a new diagnostics auth layer plus transitional SDK package work inside this repo.
- Advantages: could unblock future non-local operators and prepare package migration.
- Disadvantages: exceeds task scope; weakens the explicit local-only default; still cannot prove external bundle consumers or package artifacts.
- Risk factors: premature public auth/API surface and additional SECURITY scope without product requirement.

### 14.3 ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Constraint fit | 5 | 1 | 5 | 2 |
| Security invariant preservation | 5 | 2 | 5 | 3 |
| Evidence traceability | 5 | 1 | 5 | 2 |
| Migration safety | 4 | 1 | 5 | 3 |
| Implementation focus | 3 | 4 | 4 | 2 |
| Future extensibility | 2 | 2 | 4 | 4 |
| **Weighted Total** | | **34** | **116** | **58** |

Scoring scale: 1–5 where 5 is best fit for this reopened follow-up.

### 14.4 DECISION

**Selected: Option B — Regression-preservation plus matrix-gated conditional cleanup.**

Justification: the current repo snapshot already contains the safe local hardening lane, while all deletion lanes that could remove compatibility or security invariants still depend on external artifacts. Option B is the only design that lets BUILD move forward honestly: verify and preserve what is landed, update the matrix only from concrete evidence, and delete only after the appropriate row is green.

Trade-offs accepted:
- D1.11(b) and Phase 6 physical SDK/lib cleanup may remain incomplete in this cycle.
- Deprecated compatibility strings, bridge symbols, and skipped tests may remain visible, but with explicit blocker rationale.
- SECURITY reviews retained bridges as intentional blockers, not as newly introduced runtime exposure.

### 14.5 BUILD SEQUENCING CONTRACT

1. **Evidence checkpoint first**
   - Read `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md` before deleting any bridge.
   - A row may move to `green` only with exact external source/target paths, validation command output or QA artifact, linked commit/PR/release artifact, and a named repo cleanup gate that is unblocked.
   - Archived Memory Bank text or target architecture prose is not evidence.

2. **Baseline regression verification**
   - Preserve `cmd/tabula/kernel.tools.json == []` and `filterKernelTools` returning an empty catalog with warnings for stale/unknown explicit `kernel_tools`.
   - Preserve `/internal/snapshot/plugins` behind loopback `RemoteAddr` plus local/loopback `Host` checks; wildcard binds must not authorize public-looking `Host`; proxy/forwarded headers remain ignored.
   - Preserve boot/distro validation for non-empty unique `tools[].name` and non-empty `tools[].exec` before advertisement/promotion.
   - Preserve plugin `register`/`update_tools` catalog validation and malformed `event_reply`/`tool_result` fail-closed or side-effect-free behavior.
   - Preserve active docs/SDK decisions from §13.5: no stable generic `api.spawn`, no common `TABULA_SPAWN_TOKEN`, empty/absent `DEFAULT_KERNEL_TOOLS`, and `before_spawn`/`after_spawn` only reserved/subagent-owned unless external evidence says otherwise.

3. **Conditional D1.11(b) deletion lane**
   - Only begin after the driver/subagent plugin matrix row is green or owner-approved `not-applicable` with replacement coverage.
   - Required evidence remains: child auth or replacement-auth invariants, replay/expiry/revocation, parent/session binding, depth derivation, MaxChildren accounting, lifecycle/list/kill/cancel, shutdown cleanup, and malformed spawn/security replies failing closed.
   - If evidence is absent, retain `SpawnTokenStore`, `generateSpawnToken`, `PolicyEngine.CanSpawn`, `ProcessManager.Spawn` env injection, `Hub.MaxChildren`, `Hub.MaxSpawnDepth`, `PolicyEngine.CanConnect` token branch where still needed, and `skipKernelBuiltinRemoved` callers.

4. **Conditional diagnostics expansion lane**
   - No authenticated non-local diagnostics are designed or implemented in this task.
   - Future remote diagnostics must be a separate design with explicit authn/authz, replay/CSRF considerations for browser-accessible GETs, no trust in `X-Forwarded-*`, audit/logging expectations, and deployment guidance.

5. **SECURITY handoff**
   - Review current local-only diagnostics assumptions and test coverage.
   - Review that invalid plugin catalogs/replies cannot mutate dispatch state or default security hooks to pass.
   - Review every matrix row changed during BUILD against the evidence contract.
   - If any bridge was deleted, review the linked replacement evidence before accepting the deletion.

### 14.6 OPEN QUESTIONS / BLOCKERS

- External driver/subagent plugin evidence is still required to decide whether residual kernel `MsgConnect` token handling can be deleted or must be retained as a minimal authenticated connection gate.
- External Python wheel, TypeScript tarball, and bundle `_lib` install evidence are still required for Phase 6 physical SDK/lib deletion; see `creative-sdk-and-distro.md` §9.
- No current requirement justifies remote authenticated plugin diagnostics; keep the endpoint local-only until a separate task requests it.

## Rubric Review — 2026-04-27 Current-Cycle Runtime Refinement

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 8
    failure_isolation: 9
    constraint_fit: 10
    simplicity: 9
  ai_slop_flags: none
  verdict: PASS
  notes: Selected sequencing keeps kernel/runtime boundaries stable and makes external evidence the only trigger for compatibility deletion, avoiding unsafe cleanup from stale archived state.
```
