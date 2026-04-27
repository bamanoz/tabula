# Creative Phase: Plugin Runtime (Go Architecture)

Status: **FROZEN** 2026-04-26.
Scope: Go-side architecture for PluginRuntime, hook integration, Hub API.

## 1. DECISIONS SUMMARY

| Item | Decision | Source |
|------|----------|--------|
| `HookSubscriber` interface | **Adopt (option C)** | D2.16 / Agent 9 |
| `Hub.RegisterPlugin(...)` builder | **Adopt** | D2.14 / Agent 6 |
| `Hub.toolExec` unification | **Single map, source-tagged entries** | D1.7 |
| `hub.SnapshotPlugins()` | **Separate endpoint** | D2.15 / Agent 6 |
| `StartReaper` interaction | **Same reaper covers plugin processes** | D2.9 |
| `CanSpawn`/spawn-token vacuum | **Option (b) — keep as dead code with skip markers** | D1.11 / Agent 3 |
| `before_spawn`/`after_spawn` hook fate | **Keep registry entries, remove kernel dispatch** | D1.8/D1.9 |
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
