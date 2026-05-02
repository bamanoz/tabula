# Remote Runtime / Multi-Backend Execution

Status: **design locked, ready for M1**.
All §7 questions resolved. Next step is to slice M1 into issues.

This plan supersedes earlier discussions of "remote kernel" and
"workspace abstraction" by reframing them around a single primitive:
**execution backends**.

---

## TL;DR

- Today the kernel **owns the runtime**: it spawns plugin/skill
  subprocesses on its own host over `stdin/stdout` NDJSON.
- We want one kernel to be able to invoke tools on **another host**
  (developer's laptop while kernel sits in cloud, or vice versa).
- The cleanest split: the kernel keeps **transport-agnostic plugin
  protocol**, but plugin processes **connect to the kernel over
  WebSocket** instead of being spawned as kernel children with stdio.
- The thing that "spawns plugin processes and dials the kernel" is a
  **runtime daemon** — a small standalone program. The kernel doesn't
  spawn anything directly anymore; it asks a runtime (local or remote
  via SSH/WSS) to spawn or attach a plugin instance.
- This dissolves the workspace problem: `files`/`shell` are just plugins
  that happen to run on the runtime that has the FS. Local-mode = local
  runtime. Remote-mode = SSH/WSS-attached runtime on the dev's laptop.
- Multi-tenant kernel + per-tenant plugin sets = tenant decides which
  runtime backend(s) to use.

---

## 1. Inventory: how runtime is wired today

### 1.1 Plugin runtime — `tabula/internal/kernel/plugin/runtime.go`
- `Runtime.Spawn(manifest, opts)` does:
  1. `exec.CommandContext` with `cmd.Dir = manifest.RootDir`.
  2. `cmd.StdinPipe()` and `cmd.StdoutPipe()`.
  3. Wraps them in `NewReader/NewWriter` (NDJSON framing,
     `protocol.go`).
  4. Starts a `readLoop` goroutine that consumes plugin → kernel
     messages and dispatches via the supervisor/handle.
  5. Sends `init`, awaits `register`, opens long-lived bidi NDJSON.
- All plugin lifecycle, restart, SIGTERM, process group cleanup live
  in this package. Mechanics-only; semantics live in
  `internal/kernel/plugin_runtime.go`.
- Coupling to local OS: `os.Environ()`, `cmd.Dir`, OS process group,
  POSIX signals.

### 1.2 Skill exec — `tabula/internal/kernel/process_manager.go`
- `LocalProcessLauncher.CombinedOutput`: `sh -c <cmd>` with
  `cmd.Env = os.Environ() + extra`. One-shot subprocess per skill
  call. No persistent connection.
- `SkillExec.Run` is the hot path for `[[tools]]`-style skills (the
  ones whose `exec` strings appear in distro `boot.py` output).
- The `ProcessLauncher` interface already exists "to allow swapping
  local processes for remote workers in the future" (literally the
  comment at `process_manager.go:14`). We're cashing that check now.

### 1.3 Client connections — `internal/kernel/client.go`
- Clients are WebSocket peers. They handshake, declare manifests, and
  exchange JSON messages over the kernel's WS endpoint. This is how
  CLI/TUI/telegram already talk to the kernel.
- Plugins do **not** use this path. Plugins use stdio NDJSON. So we
  already have two parallel transports for "external code talking to
  the kernel".

### 1.4 Workspace-related state in kernel
- `Hub.ProjectRoot` (a single string).
- Set once from `os.Getenv("TABULA_PROJECT_ROOT")` in
  `cmd/tabula/main.go:266`.
- Exposed in `init.meta.project_root` for client UX only
  (`join_flow.go:82`).
- Skills/plugins inherit env via `os.Environ()`, so `TABULA_HOME` and
  `TABULA_PROJECT_ROOT` reach them implicitly. **No explicit kernel
  contract for workspace.**

### 1.5 What "workspace" means in practice today
- `files` skill is path-agnostic — caller passes absolute paths.
- `shell` skill defaults `workdir` to `TABULA_HOME` (not project!).
  Caller can override with explicit `workdir`.
- `code/workspace` skill exists and exposes `workspace_info` /
  `workspace_set_root`, persisting choice to
  `$TABULA_HOME/config/skills/workspace/root.json`. Read once at
  kernel boot.
- Net: workspace is convention, not architecture.

### 1.6 Conclusion
The current plugin runtime is **inseparable from the kernel** because:
- Plugin protocol uses stdio pipes attached at fork time.
- Plugin lifecycle (signals, exit codes, group reaping) is tied to
  kernel-as-parent.
- `os.Environ()` propagation is the only mechanism for context
  passing.

To get remote execution, **this binding must be cut**.

### 1.7 Vocabulary (decided, used consistently below)

These terms are load-bearing. The earlier draft conflated them.
Keep the distinction sharp.

- **Backend** — *how the kernel reaches a runtime daemon.*
  Examples: `local` (kernel forks runtime locally and dials a unix
  socket), `attach` (kernel does nothing; runtime dials kernel),
  `ssh` (kernel ssh's into a host and starts runtime there),
  `docker`/`k8s` (kernel launches runtime in a container/pod). All
  backends terminate at the same `RuntimeConn`. **Containers and
  sandboxes at the backend layer are about delivering the runtime,
  not about isolating individual tool calls.**
- **Runtime** — *the daemon process that hosts tool execution.* One
  binary (`tabula-runtime`). Always a separate OS process from the
  kernel, even in local-mode (Q1). Multi-tenant. Can attach to
  multiple kernels concurrently (N:M).
- **Runtime API** — *kernel ↔ runtime daemon RPC over WS.* High
  level. Kernel says "invoke this tool of this skill/plugin for
  this tenant"; the kernel does **not** know about workers,
  sandboxes, cold/warm. One RPC shape for plugin and skill calls.
- **Worker** — *a process inside the runtime that actually runs
  tool code.* Speaks the **worker protocol** to the runtime daemon
  over local stdio or unix socket. Never speaks to the kernel
  directly.
- **Worker protocol** — *runtime daemon ↔ worker on the same host.*
  Internal to the runtime. Same shape regardless of whether the
  worker hosts a long-lived plugin or a short-lived skill harness.
- **PluginExecPolicy** — *how the runtime spawns and isolates a
  worker.* Examples: `bare` (`os/exec`), `cgroup`, `sandbox`
  (bwrap/firejail/sandbox-exec), `nested-docker`. Lives entirely
  inside the runtime; tenants and runtime config decide policy.
- **Plugin** = manifest + worker policy "long-lived, warm,
  TTL=∞".
- **Skill** = manifest (markdown + `[[tools]]` frontmatter) +
  worker policy "short-lived, cold by default". Author surface is
  unchanged; execution unifies with plugins under the worker layer.
- **Tenant** — *logical namespace inside a kernel.* Per-kernel.
  Plugin/worker processes are never shared across tenants or across
  kernels. Per `(kernel_id, tenant_id, plugin_id|skill_id)` keying
  inside the runtime.
- **Plugin instance / Worker instance** — concrete OS process,
  bound to a single `(kernel_id, tenant_id, target_id)` triple.

Two-axis mental model:

```
                          ExecutionBackend
                  (how kernel reaches the runtime)
       local · attach · ssh · docker · k8s · firecracker · ...
                                │
                                ▼
                       ┌────────────────┐
                       │ Runtime daemon │
                       └───────┬────────┘
                               │
                       PluginExecPolicy
                  (how the runtime spawns workers)
            bare · cgroup · sandbox · nested-docker · ...
                                │
                                ▼
                       Worker (one process)
                          │
                          ▼
              executes plugin tool / skill tool
```

Backends and policies are **independent**. Docker can appear at
either layer or both: `docker` backend (runtime in a container) +
`bare` policy (plugin processes share that container), or `local`
backend (runtime on host) + `nested-docker` policy (each plugin
worker in its own sibling container).

---

## 2. The pivot: plugins over WebSocket

### 2.1 Same protocol, different transport
The plugin protocol (`internal/kernel/plugin/protocol.go`) is already
JSON messages. NDJSON framing is a transport detail. WebSocket gives
us framed JSON for free.

Move:
- **Today**: kernel forks plugin → plugin reads stdin, writes stdout.
- **Tomorrow**: plugin **dials** kernel WSS endpoint (`/plugin-ws`
  or auth-tagged variant of `/ws`), exchanges the same `init` /
  `register` / `tool_call` / etc. messages.

### 2.2 Who spawns the plugin?
A **runtime daemon** — separate program (`tabula-runtime`).
Responsibilities:
- Discover plugins it should run (from a local `runtime.toml`,
  installed by `tabula-distro`).
- Hold credentials (cert / token) for the kernel.
- For each plugin: spawn process, set env, set cwd, monitor health,
  restart on crash.
- Report itself to kernel (`runtime.attach` message): "I am runtime
  `laptop-mak`, here are the capabilities I can host: `files`,
  `shell`, `git`."
- Optional: hot-update plugin set when kernel asks.

### 2.3 Backends in the kernel

The kernel grows an `ExecutionBackend` abstraction. A backend's
**only** job is to produce a `RuntimeConn` to a `tabula-runtime`
daemon. It does not spawn plugins, skills, or workers itself.

Backends:
- `local` (default for local-mode): kernel forks `tabula-runtime`
  on its own host as a managed child process and dials a unix
  socket. See Q1 (decision: runtime is always a separate OS
  process, even locally).
- `attach`: kernel does not start anything; runtime dials the
  kernel from wherever it was started independently. Used for
  remote-mode where the laptop runtime initiates the connection
  to a cloud kernel.
- `ssh`: kernel ssh's into host H and starts `tabula-runtime`
  there (or invokes a preinstalled one). Conn is the SSH stream
  carrying runtime API frames.
- `docker` / `k8s`: kernel launches a container/pod with
  `tabula-runtime` as PID 1 and attaches to it.
- Future: `firecracker`, `gvisor`, etc. — same shape.

All backends produce the same `RuntimeConn`; the runtime API is
identical above this seam.

Sandboxing of individual tool calls is **not** a backend concern;
it belongs to the runtime's `PluginExecPolicy` (§1.7). A backend
choice and a policy choice are independent.

The kernel chooses a backend per **runtime binding** in tenant
config:

```toml
# tenants/<id>/tenant.toml
[[runtime]]
id      = "laptop-mak"
backend = "attach"          # waits for the laptop runtime to dial in

[[runtime]]
id      = "kernel-host"
backend = "local"           # kernel forks the colocated runtime

[plugins.files]
runtime = "laptop-mak"

[plugins.gateway-telegram]
runtime = "kernel-host"
```

### 2.4 Tool execution: unified worker model

All tool calls — plugin or skill — go through one Runtime API
shape. The kernel does not differentiate execution paths.

```
Kernel ──Invoke{target, tool, args, tenant_id}──▶ Runtime
Runtime ◀──InvokeResult{ok, data | error}──── Kernel
```

Inside the runtime:

1. Resolve `target` (plugin id or skill id) against the local
   manifest store.
2. Pick a `PluginExecPolicy` for `(tenant_id, target)` (config
   plus defaults).
3. Look up or spawn a **worker** for that triple.
4. Send the call to the worker over the worker protocol.
5. Return the worker's response to the kernel.

**Plugin vs skill differs only in worker lifecycle policy:**

| | Plugin | Skill |
|---|---|---|
| Author surface | plugin manifest + SDK | markdown + `[[tools]]` frontmatter |
| Default worker mode | warm (TTL=∞) | cold (kill after one call) |
| Worker harness | written by plugin author | auto-generated by runtime per language (bash, python, node, …) |
| State across calls | allowed (it's the point) | forbidden by contract; cold default enforces it |
| Warm opt-in | n/a (already warm) | `warm = true` in frontmatter or auto-promotion policy |

Skill workers are **plugin workers in disguise**: same worker
protocol, same sandbox layer, same restart logic. The only thing
generated for skills is a thin per-language harness that:

- Reads call requests from the worker protocol channel.
- Substitutes args into the `exec` template.
- Runs the substituted command (or imports the script and calls
  its `main`, depending on language).
- Writes the result back over the worker protocol.
- Exits after one call (cold) or loops (warm).

Process orchestration consequences:

- `tabula/internal/kernel/process_manager.go` (today's
  `LocalProcessLauncher` + `SkillExec`) **moves entirely into
  `tabula-runtime`**. Kernel keeps only the Runtime API client
  and a `SkillRouter` that picks the right runtime binding for a
  given `(tenant, skill)`.
- Plugin runtime logic in `internal/kernel/plugin/runtime.go`
  similarly moves into `tabula-runtime`. Kernel keeps the
  protocol types and the router.
- **One worker mechanism** in the runtime, used for both. No
  separate "skill exec" subsystem (no-legacy).

WAN-mode latency note: every tool call adds one runtime API
roundtrip (~ms on LAN, tens of ms on WAN). For hot skills, warm
workers eliminate per-call fork overhead; sandbox prewarm
amortizes sandbox setup. M2 ships cold-default for skills; warm
for skills is opt-in, not a separate execution path.

### 2.5 Client transports vs runtime transports
After the move, kernel has two WSS endpoints (or one with role
parameter):
- `/ws` — clients (CLI, TUI, telegram, IDE plugins).
- `/runtime` — runtime daemons.

Or one `/ws` that takes a `role=client|runtime` param at connect with
auth dictating allowed roles. Either is fine; pick one in detailed
design.

### 2.6 What this looks like in practice

**Local-only setup (replaces current installation):**
1. User installs kernel + runtime + distro on laptop.
2. `tabula-runtime start` (foreground or background) connects to
   localhost kernel.
3. Kernel detects runtime, registers plugins from runtime.toml.
4. Tools work as today, but every plugin is now a WS-connected
   subprocess of `tabula-runtime`, not of the kernel.

**Remote-kernel setup (the team scenario):**
1. Kernel runs in cloud.
2. Each developer installs `tabula-runtime` locally.
3. `tabula-runtime` dials cloud kernel, authenticates, presents
   `runtime_id = mak/laptop`.
4. Tenant `mak/projectA` is configured to host `files` and `shell`
   on `runtime = mak/laptop`.
5. LLM tool call → kernel routes to runtime → runtime spawns/uses
   plugin process locally → plugin reads laptop FS → response goes
   back through the same path.
6. No file sync. No FUSE. No client-callback layer. The runtime IS
   the file/exec authority.

**Hybrid:**
1. Same as remote, but `gateway-telegram` plugin is configured with
   `runtime = "kernel-host"` so it's always-on in the cloud while
   `files` runs on the laptop's runtime. Always-on services live with
   kernel; FS-bound services live with developer.

---

## 3. Why this is better than "client-callback workspace"

In the previous design (Variant 5/6 in `REMOTE_RUNTIME` discussion),
the kernel learns to treat workspace as a special protocol layer that
proxies file operations back to the connected client. That's:
- Yet another protocol to maintain.
- An ad-hoc reimplementation of "process talks to kernel" — but with
  weaker guarantees (a CLI client is a thin terminal, not a runtime).
- Coupled to client identity ("this client owns this FS"), which
  doesn't generalize (telegram client doesn't have an FS).

The runtime-daemon approach:
- Reuses the plugin protocol — one transport, not two.
- Keeps clients thin: they only display sessions, ferry user input.
- Workspace becomes a **plugin** running in a **runtime that has the
  FS** — naturally so.
- Telegram-bot-as-only-client is fine: that tenant just has no
  `runtime` for `files`/`shell`, those tools are unavailable. Clean
  failure.

It also dissolves the "workspace as kernel-side abstraction" debate.
Workspace is not a kernel concept. It is a property of which runtime
hosts which plugins.

---

## 4. Multi-tenant fit

Multi-tenant kernel + runtimes:
- Tenant config declares which runtimes it uses.
- Runtime can be assigned to one tenant (typical for dev's laptop) or
  shared (typical for kernel-host always-on services).
- **No plugin sharing** invariant remains: even when two tenants both
  use `runtime = mak/laptop`, each tenant gets its own plugin process
  inside that runtime — runtime spawns separate `files` instances per
  tenant. Runtime is the multi-tenant boundary on its host.
- Kernel is multi-tenant logically; runtime is multi-tenant on its
  process pool.

This means **a single laptop runtime can host plugins for multiple
tenants concurrently**, while kernel still keeps each tenant's plugin
processes isolated by spawning them per-tenant.

---

## 5. Changes by repo

### 5.1 `tabula` (kernel)
- Plugin protocol: keep message schema, drop hard dependency on
  stdio. Add WSS transport implementation alongside the existing
  stdio one (during migration). Then remove stdio when no longer
  needed (per AGENTS no-legacy rule).
- New `internal/runtime/` (Go): `RuntimeRegistry`,
  `RuntimeConnection`, message router that maps kernel-side plugin
  intents (spawn, call, shutdown) to runtime RPC.
- `Hub` learns about runtimes: `Hub.RegisterRuntime`,
  `Hub.RouteToolCall(plugin_id) -> runtime`.
- New WS endpoint (or role flag) for runtimes.
- New auth path for runtimes (token/mTLS).
- `cmd/tabula-runtime/` (NEW BINARY): the runtime daemon.
  - `runtime.toml` config (kernel URL, plugin search dir, auth).
  - Local plugin spawn logic moved here from
    `internal/kernel/plugin/runtime.go`.
- Skill exec: same treatment via runtime "one-shot job" RPC.
- Multi-tenant Hub work proceeds in parallel; tenant config gains
  `[plugins.<id>] runtime = "..."`.

### 5.2 `tabula-bundles`
- Plugins keep their current SDK shape, but SDK gains a transport
  switch: `if env.TABULA_PLUGIN_TRANSPORT == "wss"` then dial WSS
  instead of using stdin/stdout. (SDK already abstracts framing —
  this is mostly wiring.)
- `files`, `shell`, `code/workspace` collapse into a single
  `workspace` family of plugins (or stay separate, see Q3 below).
- New runtime examples / tests in `tabula-bundles/_lib`.

### 5.3 `tabula-distrib`
- Distro layout grows a `runtime/` section — manifests for which
  runtimes the distro expects, what plugins each hosts.
- `claw/distro.toml` declares `[runtime.local]` by default for
  developer machines.
- Bootstrap (`make agent`) installs **kernel + runtime + distro** on
  the same machine for local-mode, or **runtime only** if pointing at
  a remote kernel.

### 5.4 `tabula-distro` (installer in `tabula/tools/tabula-distro/`)
- Installer learns to install runtime artifacts to a separate path
  (so a server install can omit the runtime, and a laptop install
  can omit the kernel).
- New install topologies:
  - `kernel-only` (server)
  - `runtime-only` (laptop, points at remote kernel)
  - `kernel+runtime` (default for local dev)

---

## 6. Migration order

Sequencing reflects all §7 decisions. Each phase is a coherent
milestone; no phase straddles a "leave dead code for later" boundary
except where called out (M2 → M3 transition).

**Dependency graph:**

```
M1 ──▶ M2 ──▶ M3 ──▶ M5
        │            ▲
        ▼            │
       M4 ───────────┘
        │
        └────▶ M6
```

**M0 — Lock the design.** Resolve §7. Land this doc with answers.
**Status: DONE.**

**M1 — Runtime API skeleton + interfaces.** Pure scaffolding,
no behavior change in production.
- Define wire protocol shapes: `Invoke`, `InvokeResult`, `Cancel`,
  `cancel_ack`, `Health`, `ListCapabilities`, `Reload`.
- Define Go interfaces: `RuntimeConn`, `Backend`,
  `PluginExecPolicy`.
- Define worker protocol shapes (runtime ↔ worker).
- Add mock `RuntimeConn` for unit tests.
- Tests still pass; nothing user-visible.
- **Risk: low.** New code only.

**M2 — Extract `tabula-runtime` daemon (plugin path only).**
The big atomic change for plugins.
- New `cmd/tabula-runtime` binary.
- Plugin spawn logic moves out of
  `internal/kernel/plugin/runtime.go` into the runtime daemon.
- `local` backend implemented: `tabula serve` forks
  `tabula-runtime` as a managed child; runtime dials kernel's unix
  socket at `$TABULA_HOME/run/runtime.sock` (Q1, Q2).
- Token auth (Q4): kernel writes `runtime-token` at startup;
  runtime reads and presents it.
- `tenant_id` mandatory on Invoke from day one (Q5), even though
  the kernel is still single-tenant in M2 (always emits the same
  default tenant_id).
- **Stdio plugin transport DELETED from kernel** in this same
  change (Q10, atomic).
- **Intermediate state (Q-seq-a, accepted):** skill execution
  remains via kernel's `process_manager.go` (`sh -c`) for the
  duration of M2 → M3. Documented as temporary.
- `bootstrap.sh` updated to start kernel and confirm the managed
  runtime is attached before declaring ready.
- Testbed exercises real install path (Q10c).
- **Risk: high.** Largest single change in the program.

**M3 — Unified worker model (Q8).**
- Implement worker protocol on the runtime side.
- Generate per-language skill harnesses (bash, python, node).
- Migrate every bundle skill to the harness contract.
- **`process_manager.go` DELETED from kernel.** Kernel now has
  one and only one tool-execution code path.
- All tool calls (plugin + skill) flow through the unified
  Runtime API.
- **Risk: medium.** Many small migrations; scriptable.

**M4 — Multi-tenant kernel.**
- `Hub` becomes router with `map[tenant_id]*TenantRuntime`.
- Per-tenant FS layout under `$TABULA_HOME/tenants/<id>/`.
- Two-sided runtime/tenant whitelist (Q5c).
- Tenant config schema lands; existing single-tenant installs
  migrated to the default tenant transparently.
- Per-`(kernel,tenant)` runtime state directories
  (`$RUNTIME_HOME/kernels/<kid>/tenants/<tid>/`).
- Audit logs (Q5e).
- **Risk: medium.** Hub refactor, but contained.

**M5 — `fs` + `exec` plugins replace workspace skills.**
- New `tabula-bundles/workspace/{fs,exec}/` plugins (Q3).
- Tenant config supports `${project_root}` template variable.
- Delete `tabula-bundles/files/files/`,
  `tabula-bundles/base/shell/`, `tabula-bundles/code/workspace/`.
- Final no-legacy verification chore (former M6, folded per
  Q10d): grep for dead references, dangling imports, deprecated
  symbols across all repos.
- **Risk: low.** Clear capability swap with deletes.

**M6 — Remote backends.**
- WSS backend (kernel listens on `/runtime`, runtime dials
  remotely).
- SSH backend (out-of-process `ssh`, Q7).
- mTLS auth as opt-in second mode (Q4).
- Token onboarding flow: `tabula runtime token issue/revoke`.
- `install-service.sh` for cloud kernels (launchd / systemd).
- **Risk: low.** Additive new backends on top of stable Runtime
  API.

---

## 7. Open questions (need answers before M1)

These are the blocking decisions. Comments in this section are
acceptable; resolve before turning M1 into issues.

### Decisions locked so far

- **Runtime ↔ kernel cardinality: N:M.** One runtime daemon may
  attach to multiple kernels concurrently (e.g. local laptop runtime
  serves both a localhost kernel and a team cloud kernel). Hard
  invariants:
  - Plugin process belongs to exactly one `(kernel_id, tenant_id)`
    pair. Never shared between kernels or tenants.
  - Tenant namespace is per-kernel: tenant `acme` on kernel A and
    tenant `acme` on kernel B are unrelated.
  - Per-`(kernel_id, tenant_id)` state directory under
    `$RUNTIME_HOME/kernels/<kernel_id>/tenants/<tenant_id>/`.
  - Auth, resource limits, and failure isolation are per-kernel.
  - Runtime does not federate or route between kernels; kernels do
    not see each other through the runtime.
- **Q11 — Discovery model: push.** Runtime config (`runtime.toml`)
  lists kernels statically with their URLs and tokens. No identity
  provider / pull-discovery in M2–M3. Pull/hybrid discovery is an
  enterprise milestone, deferred.

### Q1. Default install topology — DECIDED

Three valid topologies after M2:
- (a) `kernel + runtime` on one machine (today's UX preserved).
- (b) `kernel-only` (server) + `runtime` (laptop).
- (c) `runtime-only` (laptop) attaching to existing kernel-only host.

**Decision: (a) is the default for `make agent`.** Solo-dev UX is
preserved; (b) and (c) are explicit team setups, opted into via
`tabula.project.toml`:

```toml
[kernel]
mode = "remote"
url  = "wss://kernel.team.example/runtime"
```

When `mode = "remote"`, `make agent` installs runtime + distro only;
no local kernel is started.

**Q1a — runtime is always a separate OS process**, including in
local-mode. No goroutine-mode embedding. One transport, one code
path; matches no-legacy policy.

**Q1b — process management:**
- Local-mode: `tabula serve` forks `tabula-runtime` as a managed
  child. Single `Ctrl+C` tears down the whole stack.
- Remote-mode: runtime is started independently via
  `tabula runtime start` (or a launchd/systemd unit). Lifecycles are
  independent because kernel and runtime live on different hosts.

Implications for M2 work:
- `tabula-runtime` binary must be self-contained and runnable
  standalone.
- `cmd/tabula/serve.go` (or equivalent) gains a "managed child"
  supervisor that starts and reaps the local runtime.
- Local handshake uses a token written to
  `$TABULA_HOME/run/runtime-token` at kernel startup; runtime reads
  it and dials `ws://127.0.0.1:<port>/runtime`.

### Q2. Runtime API transport — DECIDED

The Runtime API (kernel ↔ runtime daemon) uses one of two
transports, both speaking the same WebSocket-framed JSON protocol:

- **Local backend:** unix domain socket at
  `$TABULA_HOME/run/runtime.sock`. Parent dir `0700`, socket
  `0600`. FS permissions are first-line auth; per-process token is
  second factor.
- **Remote backend:** WSS (TLS over TCP). Standard 443-friendly,
  ingress-friendly. mTLS added later (see Q4).

**Q2b — Direction:** runtime **always dials** the kernel; kernel
listens. Holds for both local and remote backends. In local-mode
this means the kernel forks the runtime as a managed child (Q1)
and the runtime then connects back to the unix socket the kernel
opened at startup.

**Q2c — N:M:** a runtime attaching to multiple kernels just dials
multiple URLs (e.g. `unix://...local.sock` + `wss://team/runtime`).
Two independent connections; no special-casing in the runtime.

**Q2d — Windows:** deferred. AF_UNIX exists on Windows 10+ but
WS-stack support is uneven. We will not target Windows in M2. If
needed later, fallback is loopback TCP with a localhost-only bind
and mandatory token; treated as a separate milestone.

**Q2e — Worker protocol stays on stdio NDJSON.** The
runtime ↔ worker channel is a different layer from the Runtime API
and uses stdin/stdout NDJSON between the runtime daemon and each
worker process it spawns. Reasons:
- Worker is a child of the runtime; fd inheritance is trivial.
- No socket setup at spawn time; cold workers don't pay for a
  long-lived socket.
- Sandbox-friendly: `bwrap`/`firejail`/`sandbox-exec` propagate
  stdin/stdout transparently; sockets are harder to thread.

The no-legacy commitment to remove stdio in M6 applies to the
**kernel ↔ plugin** stdio path, which is what gets cut by moving
plugins under the runtime. Stdio between runtime daemon and its
own workers is the chosen design, not legacy.

Two-layer wire-protocol summary:

| Layer | Transport |
|---|---|
| Kernel ↔ Runtime daemon | unix socket (local) or WSS (remote) |
| Runtime daemon ↔ Worker | stdio NDJSON (always) |

### Q3. Workspace plugin shape — DECIDED

The `workspace` concept dissolves as a code-level abstraction.
There is no `workspace` plugin. Instead, two independent plugins
expose orthogonal capabilities; "workspace" survives only as a
config template that pins their roots/cwd to `${project_root}`.

**Q3 main decision: split by blast radius into two plugins, `fs`
and `exec`.** The legacy `files`, `shell`, and `code/workspace`
artifacts are removed in M5.

| Plugin | Capability | Tenant-config knobs |
|---|---|---|
| `fs` | read/list/glob/grep/stat/write/edit/mkdir/rm | `roots`, `writable` |
| `exec` | run subprocess (incl. `git`, `rg`, scripts) | `cwd_default`, sandbox policy (separate later milestone) |

Capability and scope are independent axes. Tenant configs assemble
agent personas:

```toml
# coding agent
[plugins.fs]
roots = ["${project_root}"]
writable = true
[plugins.exec]
cwd_default = "${project_root}"

# system agent (no workspace)
[plugins.fs]
roots = ["/"]
writable = true
[plugins.exec]
cwd_default = "${HOME}"

# read-only browse
[plugins.fs]
roots = ["${project_root}"]
writable = false
# no [plugins.exec]
```

**Q3a — `git`, `rg`, scripts** run via `exec` (subprocess). In-
process `grep`/`glob` lives in `fs` for speed; the CLI variants
remain available through `exec`. Both shapes are intentional.

**Q3c — `code/workspace` skill is removed.** The "selected root"
state moves into kernel/tenant config (`${project_root}` variable).
Switching root is a kernel-level operation (CLI subcommand or
tenant config edit), not a tool call.

**Q3.4 — exec is independent of `fs.roots`.** `exec` does not
inherit fs's roots. A subprocess started via `exec` can read/write
anywhere the OS user can; if hard isolation is desired, the tenant
opts into a sandbox `PluginExecPolicy` (separate later milestone).
This is honest: emulating sandboxing inside the plugin layer would
be theatre. M5 ships `bare` policy default; bwrap/sandbox-exec
are deferred.

**Q3.4 — `exec.cwd_default` has no implicit default.** Either the
tenant config sets it, or the call must pass `cwd` explicitly. We
do not repeat the legacy `shell` skill's habit of silently
defaulting workdir to `$TABULA_HOME`.

**Q3.5 — Bundle layout** in `tabula-bundles/`:

```
tabula-bundles/
  workspace/
    fs/
      plugin.toml
      scripts/run.py
      tests/
    exec/
      plugin.toml
      scripts/run.py
      tests/
  _lib/python/src/tabula_plugin_sdk/   # shared
```

Bundle name `workspace` reflects intended use (the persona that
typically pins these to `${project_root}`); a system-agent tenant
just points `fs.roots` at `/` and uses the same plugins.

**M5 cleanup deletions** (no-legacy):
- `tabula-bundles/files/files/`
- `tabula-bundles/base/shell/`
- `tabula-bundles/code/workspace/`

### Q4. Auth model for runtimes — DECIDED

Two-phase rollout:

**M2: opaque bearer tokens.** One mechanism for both transports.

- **Local backend:** unix socket `0600` is the first-line barrier
  (uid match). At kernel startup, a random token is generated and
  written to `$TABULA_HOME/run/runtime-token` (`0600`). The
  managed-child runtime reads it and presents it in the first
  Runtime API frame. Defense-in-depth against malicious processes
  running under the same uid (compromised browser extension, npm
  postinstall hook, etc.).
- **Remote backend:** token issued by `tabula tenant
  create-runtime-token <runtime_name>`, shown once, copied into
  the laptop's `runtime.toml`. WSS protects on the wire.

**M3+: mTLS as opt-in second mode** before remote backend ships
publicly. Token mode remains for simple deployments. Tenant config
chooses `auth = "token"` or `auth = "mtls"`. mTLS adds
non-transferable identity, native rotation via cert lifetime, and
auditable serials. The handshake is an interface from M2, so
adding mTLS does not refactor token logic.

**Q4a — Token format.** `rtk_<32 bytes base64url>`. Prefix style
matches GitHub PAT / Stripe key for log readability and secret-
scanning. Kernel storage at
`$TABULA_HOME/state/runtime-tokens.json`: stores `{token_hash,
runtime_name, tenant_scope, created_at, last_seen}`. Plaintext is
visible only at creation time.

**Q4b — Token scope: per-runtime, not per-tenant.** Auth answers
"are you runtime X?". Authorization (is runtime X allowed to host
tenant Y?) is a separate layer that consults tenant config on
every Invoke.

**Q4c — Revocation.** `tabula runtime revoke <runtime_name>`
removes the token from storage and disconnects active
connections. Per-token, immediate.

**Q4d — Local token refresh on restart.** Kernel regenerates the
local token on every startup; the old token is invalidated. The
managed-child runtime reads the fresh token at its own startup. An
independently-started runtime that survives a kernel restart sees
auth-fail on next message and must re-read the token file.
Documented edge case; default local-mode (managed child) avoids
it.

**Q4e — Kernel identity (runtime trusts kernel).**
- Local: implicit trust via unix socket file path under the user.
  No other process can `listen` there.
- Remote: standard TLS server cert validation. Self-signed kernels
  require a pinned CA or fingerprint in `runtime.toml`:
  ```toml
  [[kernel]]
  url     = "wss://kernel.team.example/runtime"
  token   = "rtk_..."
  ca_cert = "/etc/tabula/team-ca.pem"
  # or: fingerprint = "sha256:..."
  ```
  Public-CA kernels need nothing extra.

**Q4f — Runtime-side token storage.** Plain `runtime.toml`,
`0600`. Not ideal, but: revocable centrally (Q4c), keychain
integration deferred until prod-grade onboarding work. Documented
trade-off.

**Explicitly out of scope for M2/M3:** JWTs (signing infra without
profit), OAuth/OIDC (enterprise milestone if real IdP request
appears), at-rest encryption of tokens in kernel storage (hash is
sufficient).

### Q5. Tenant attribution end-to-end — DECIDED

Every tool call carries `tenant_id` from kernel session through
Runtime API to worker, with three independent enforcement points
(kernel router, runtime worker pool, worker process). Defense-in-
depth against cross-tenant leaks.

**Wire shape — `tenant_id` is mandatory on `Invoke`:**

```json
{
  "op": "invoke",
  "call_id": "c7f3...",
  "tenant_id": "acme",
  "target": {"kind": "plugin", "id": "fs"},
  "tool": "read_file",
  "args": {"path": "/proj/main.go"}
}
```

Missing `tenant_id` on `Invoke` is a protocol error.

**Q5a — `kernel_id` is implicit by connection.** Not in the frame.
Runtime maintains a `connection_id → kernel_id` map; the
`(kernel_id, tenant_id)` pair is formed on the runtime side. This
keeps the wire format simple and avoids spoofing
(connection-bound).

**Q5b — System ops are separate op-types, not Invoke variants.**
`Health`, `ListCapabilities`, `Reload` carry no `tenant_id` field
at all (no optional/empty hack). `Invoke` is the only tenant-
scoped op.

**Q5c — Two-sided tenant whitelist:**

Kernel-side, in tenant config:
```toml
# tenants/acme/tenant.toml
allowed_runtimes = ["laptop-mak", "kernel-host"]
```

Runtime-side, in runtime config:
```toml
[[kernel]]
id            = "team"
allow_tenants = ["acme", "wayne-corp"]
```

Either side can refuse. Kernel rejects pre-wire (does not even
send the Invoke); runtime rejects on receipt. Both checks are
mandatory.

**Q5d — Tenantless sessions are not allowed for tool calls.**
Bootstrap / pre-login states don't issue `Invoke`s; only session-
control ops. Any `Invoke` reaching kernel without a resolved
tenant is a kernel-side bug, surfaced as protocol error.

**Q5e — Per-(kernel,tenant) audit logs on runtime side:**

```
$RUNTIME_HOME/kernels/<kernel_id>/tenants/<tenant_id>/logs/invocations.jsonl
```

One JSONL line per call: `{call_id, target, tool, ts, duration_ms,
ok|err}`. Args/results not logged by default (privacy + size);
opt-in via runtime config. Kernel keeps its session-level audit
separately (today's jsonl session journal).

**Worker isolation contract.** A worker spawned for `(kernel_id,
tenant_id, target_id)` receives those three values via env
(`TABULA_KERNEL_ID`, `TABULA_TENANT_ID`, `TABULA_TARGET_ID`).
Every worker-protocol message is checked against the worker's
identity; mismatched messages are rejected with a protocol error.
Third enforcement point complements kernel + runtime checks.

**FS layout (runtime side):**

```
$RUNTIME_HOME/
  kernels/
    <kernel_id>/
      auth.token
      tenants/
        <tenant_id>/
          workers/
            <target_id>/
              run/
              state/
              logs/
```

No top-level `tenants/`; tenant is always nested under its kernel.

### Q6. Error model when runtime disconnects — DECIDED

Fail-fast in M2/M3. Reliability/idempotency-aware retry is a
later concern.

**Disconnect during a pending Invoke.** Kernel detects connection
drop; all pending invokes on that connection complete with:

```json
{
  "op": "invoke_result",
  "call_id": "c7f3...",
  "ok": false,
  "error": {
    "code": "runtime_unavailable",
    "message": "runtime 'laptop-mak' disconnected during call",
    "retryable": true
  }
}
```

The `retryable` flag hints the LLM driver about transient nature.
Other errors (`tool_not_found`, `target_not_authorized`,
`tenant_denied`) carry `retryable: false`.

Kernel-side idempotency / persistent buffered retry deferred —
requires a manifest-level idempotency model that does not exist
yet.

**Q6a — Runtime reconnect.** Exponential backoff 1s, 2s, 4s, …,
capped at 60s, retried indefinitely until explicit shutdown. On
reconnect: re-handshake (token + `ListCapabilities`). In-flight
calls from before the drop are **not** recovered — kernel already
failed them.

**Q6b — Warm workers survive disconnect.** Workers are not tied
to a kernel connection; they are owned by the runtime daemon.
After reconnect, the existing worker pool is reused, keyed by
`(kernel_id, tenant_id, target_id)`. If a kernel restart needs a
clean slate (manifest version changed), kernel issues `Reload`
before resuming Invokes; runtime then terminates stale workers
and spawns fresh ones. Reload is a separate mechanism from
disconnect handling.

**Q6c — Per-call timeout.** Default 5 minutes. Override sources:
- Plugin/skill manifest `timeout_seconds` per tool.
- Per-invoke override from kernel (LLM tool descriptor hint).

On timeout: kernel sends `Cancel{call_id}` to runtime and
completes the invoke with `error.code = "timeout"`. Runtime
SIGTERMs the worker on cold; on warm, SIGTERMs the in-flight
operation if the harness supports it, otherwise kills the worker.

**Q6d — Cancel propagation.** Kernel sends `Cancel{call_id}` on
user/driver abort. Runtime: SIGTERM → wait 5s → SIGKILL on
unresponsive workers. Runtime acks with `cancel_ack{call_id}`.
Kernel waits up to 10s for the ack; missing ack closes the call
on kernel side regardless.

**Q6e — Message ordering / loss.** WSS rides TCP; ordered and
reliable up to connection drop. No message-level retries or
sequence numbers in the protocol; drop is the only failure mode,
handled by fail-fast above.

### Q7. SSH backend ergonomics — DECIDED

Kernel uses **out-of-process system `ssh`** for the SSH backend.
No in-process SSH library.

Reasons:
- Users' existing SSH ecosystem works as-is: `~/.ssh/config`,
  ProxyJump, ControlMaster, ssh-agent (incl. 1Password / Teleport
  / yubikey), known_hosts trust — all delegated to OpenSSH.
- We do not reimplement SSH config parsing, ssh-agent integration,
  hardware-key support, or host-key verification.
- Operators debug connectivity with `ssh -v <host> echo ok`; clean
  boundary between our bugs and SSH-layer bugs.

**Sketch:**

```go
type SSHBackend struct {
    Host       string
    SSHCommand []string  // default ["ssh"]
    RemoteCmd  []string  // default ["tabula-runtime", "stdio"]
}
```

Tenant config:

```toml
[[runtime]]
id      = "build-host"
backend = "ssh"
[runtime.ssh]
host       = "build@build.team.example"
remote_cmd = ["tabula-runtime", "stdio"]
```

**Q7a — `tabula-runtime stdio` subcommand.** Runtime daemon
normally listens on a unix socket or accepts WSS connections; for
SSH it runs a stdio mode where Runtime API frames travel over its
own stdin/stdout. Kernel's SSH subprocess pipes form the
`net.Conn`.

**Q7b — Runtime install is manual bootstrap.** Kernel assumes
`tabula-runtime` is already installed on the remote host (via
`tabula install --runtime-only` over a separate SSH session). No
auto-upload via SCP in M3; complicates versioning and is not
worth the cost.

**Q7c — One SSH connection per `(kernel, runtime_binding)`.**
Multiplexing across bindings to the same host is the user's
concern via `ControlMaster` in `~/.ssh/config`.

**Q7d — Liveness via periodic `Health` op** (every 30s by
default). Missing response → kernel treats runtime as disconnected
and fails pending invokes (Q6). SSH-level keepalives
(`ServerAliveInterval`) are user config; we do not set them.

**Out of scope:** in-process SSH library, SCP-based runtime auto-
upload, parsing `~/.ssh/config`. Custom SSH options reach us via
`ssh_command = ["ssh", "-F", "/path/to/config"]` if needed.

### Q8. Tool execution model — DECIDED

All tool calls — plugin and skill — flow through a single Runtime
API on the kernel↔runtime seam: `Invoke{target, tool, args,
tenant_id}`. The kernel does not have a separate "skill exec"
path; `internal/kernel/process_manager.go` moves into the runtime
as part of M2.

Inside the runtime, plugins and skills share one worker mechanism
(see §1.7 vocabulary, §2.4). They differ only in worker lifecycle
policy:

- Plugin = long-lived warm worker (today's plugin behavior).
- Skill = short-lived cold worker by default (matches today's
  one-shot `sh -c` semantics from the author's perspective).

Skill author surface (markdown + `[[tools]]` frontmatter) does
**not** change. The runtime auto-generates a per-language worker
harness (bash, python, node, …) that wraps the `exec` template
and speaks the worker protocol.

**M2 scope explicitly includes:**
- One worker mechanism in `tabula-runtime` (no skill/plugin
  duality).
- Per-language skill harnesses for the languages used in
  `tabula-bundles` today.
- Migration of all bundle skills onto the new harness.
- Removal of `process_manager.go` from the kernel.

**M2 default:** cold for skills, warm for plugins. Warm-for-skills
is opt-in via manifest hint or auto-promotion policy; deferred
beyond M2.

This is a deliberate scope expansion accepted by the user: it
costs roughly 3–5× the bare "extract runtime daemon" work, but
buys a single execution code path and removes the skill/plugin
duality permanently (no-legacy).

### Q9. Bootstrap UX — DECIDED

Go binary stays minimal; user-facing workflow lives in shell
scripts. The Go CLI provides composable primitives only.

**Go subcommands (`tabula`, `tabula-runtime`):**

```
tabula serve                          # start kernel (foreground)
tabula runtime start                  # start runtime daemon (foreground)
tabula tenant create <name>
tabula tenant list [--json]
tabula tenant remove <name>
tabula runtime token issue <runtime>  # prints token once
tabula runtime token revoke <runtime>
tabula status [--json]                # kernel/runtime/tenants state
tabula version
```

Rules:
- One operation per command.
- `stdout` = data; `stderr` = errors; meaningful exit codes.
- `--json` for machine-readable output; default human-readable but
  decoration-free.
- No prompts, no multi-step orchestration, no installation checks
  inside Go.

**Shell scripts own the workflow:**

```
tabula/scripts/
  install.sh              # install tabula + tabula-runtime binaries
  bootstrap.sh            # provision tenant + start kernel/runtime
  tabula-init             # create tabula.project.toml + Makefile (interactive)
  install-service.sh      # opt-in launchd/systemd unit

tabula-distrib/<distro>/scripts/
  bootstrap.sh            # distro-specific extensions
```

`make agent` in a project's Makefile is a thin wrapper over
`bootstrap.sh` (or an installed `tabula-bootstrap` script on
`$PATH`). No `tabula bootstrap` Go subcommand exists.

**`bootstrap.sh` flow** (sketch, real script lives under
`tabula/scripts/`):

1. Sanity-check `tabula` is installed.
2. Read `tabula.project.toml` for `project.name` and
   `kernel.mode` (`local` or `remote`).
3. `tabula tenant list --json | jq` to check tenant exists; if
   not, `tabula tenant create <name>`.
4. Local mode: `tabula status --json` to detect a running kernel;
   if absent, `tabula serve &` (or via launchctl/systemd when
   `install-service.sh` was used). Runtime is the kernel's
   managed child (Q1) so no separate start.
5. Remote mode: `tabula runtime start &` to dial the configured
   remote kernel. Tenant is assumed to already exist on the
   kernel host (admin-provisioned, see Q9e).
6. Print "ready" with connection hints.

**Q9a — Service units are opt-in.** `install-service.sh` creates
a launchd plist (macOS) or systemd unit (Linux) for `tabula
serve`. Default install does nothing system-level; the kernel
lives only as long as the shell session that started it.

**Q9b — Tenant per project.** `bootstrap.sh` derives tenant name
from `tabula.project.toml`'s `project.name`. Conflicts (another
project on the same machine took the same name) fail with a
suggestion to set `--tenant-name` explicitly in the project
config.

**Q9c — Idempotent re-runs.** A second `bootstrap.sh` on a
project with a running local kernel does **not** restart anything:
detects via `tabula status --json`, ensures the tenant exists,
exits.

**Q9d — `tabula serve` is low-level.** Foreground kernel only,
no tenant provisioning, no installation checks. Used by
`bootstrap.sh` and by operators who want explicit control.
`tabula runtime start` is its counterpart for remote-mode.

**Q9e — Remote-mode tenants are admin-provisioned.**
`bootstrap.sh` does not create tenants on a remote kernel; that
is an admin operation on the kernel host (run via SSH on the
kernel host: `tabula tenant create <name>` + `tabula runtime
token issue <runtime>`). If the remote tenant is missing, the
script fails with "contact your admin / run `tabula tenant
create` on the kernel host".

**`tabula.project.toml` minimum shape:**

```toml
[project]
name = "myproject"
# project_root implicit = directory containing this file

[kernel]
mode = "local"   # or "remote"
# url   = "wss://kernel.team.example/runtime"   # remote only
# token = "rtk_..."                              # remote only
```

If the file is missing, `bootstrap.sh` exits with a hint to run
`tabula-init`. No magic config creation.

### Q10. Kill switch / no-legacy — DECIDED

Stdio plugin transport is **fully removed from the kernel in M2**,
in the same change that extracts `tabula-runtime`. Atomic. Per
AGENTS no-legacy policy: rename/restructure → delete the old
surface in the same change.

After M2 the kernel has exactly **one** code path for tool
execution: Runtime API to a `tabula-runtime` daemon. No
`embedded` backend, no fallback, no fork-the-plugin-myself path.

Reasoning:
- One code path is the explicit AGENTS rule and avoids long-term
  duality.
- Tests adapt to spawn a real `tabula-runtime` (matches prod
  topology). Unit tests mock `RuntimeConn`; no real `os/exec`
  needed in unit scope.
- Shim is not justified: the stdio transport is internal kernel↔
  plugin contract, not on-disk state and not a wire format with
  external systems.
- Allowing an `embedded` backend "for tests only" is a slippery
  slope.

**Q10c — Testbed uses the real install path.** After M2,
`tabula/tools/tabula-testbed/` provisions an isolated
`TABULA_HOME`, runs `bootstrap.sh`, and exercises the system
through Runtime API as a real client would. No embedded path,
no special test-only spawn. Strictly stronger guarantees than
today.

**Q10d — M6 collapses into M5.** Because the deletion is atomic
in M2, there is no separate "remove stdio in M6" phase. The
former M6 becomes a short verification chore at the end of M5
(grep for dead code, dangling imports, deprecated symbols).

---

## 8. Provisional architecture diagram

```
                ┌────────────────────────────────────────┐
                │              Kernel (Hub)              │
                │  multi-tenant, transport-agnostic      │
                └──────┬───────────────────────┬─────────┘
                       │                       │
                /ws    │                       │ /runtime
                       │                       │
        ┌──────────────┴──────────┐    ┌───────┴────────────┐
        │  Clients                │    │  Runtimes          │
        │   - cli                 │    │   - laptop-mak     │ ──spawns── files-plugin
        │   - tui                 │    │   - kernel-host    │ ──spawns── telegram-plugin
        │   - telegram-gateway    │    │   - runner-1       │ ──spawns── shell-plugin
        │   - ide                 │    └────────────────────┘
        └─────────────────────────┘             │
                                                │
                                                ▼
                                       Plugin processes
                                       (one per tenant per
                                        plugin per runtime)
```

---

## 9. Pre-build state (what we have committed already)

Before this plan we landed:
- `tabula` 9bdc868 — kernel/installer hardening, no-legacy policy,
  drop legacy gateway-api, hot-reload trigger via `run/reload.touch`.
- `tabula-distrib` d806022 — claw distro reshape, gateway-telegram is
  a plugin, gateway-api removed.
- `tabula-bundles` 750a6ed — SDK `__version__`, `plugin_run_dir`,
  bundle realignment under scripts/ layout.

These are independent and orthogonal to this plan. They are pushed
when the user invokes the push command they were given.

---

## 10. Working notes for the next session

If context is being compacted, these are the breadcrumbs:

1. The user **wants** multi-tenant kernel + remote kernel. They
   want explicit `tenant_id`, no plugin sharing, kernel auto-starts on
   `make agent`, sessions = jsonl journal (already), small-team scope
   first.
2. The user explicitly **rejected** the client-callback workspace
   model. The agreed direction is **runtime daemon split + WSS plugin
   transport + execution backends in kernel**.
3. **Workspace is not a kernel concept**. It is a plugin (or family of
   plugins) running on the runtime that owns the FS. Kernel just routes
   tool calls.
4. Today's plugin runtime in `internal/kernel/plugin/runtime.go` is
   inseparable from the kernel because of stdio coupling. The
   refactor in M1/M2 is what enables remote.
5. `ProcessLauncher` interface in `internal/kernel/process_manager.go`
   already explicitly anticipates "remote workers" — we are
   redeeming that future.
6. The user accepted the sequencing M1 → M6 informally; final
   decisions on Q1–Q10 are still pending.
7. **Do not start coding M1 until Q1–Q10 are answered.** This plan
   is the contract for what we agreed; deviations should update this
   doc first.

---

## 11. Resume checklist

When resuming after compaction:
1. Read this file end to end.
2. Read AGENTS.md in `tabula/`, `tabula-bundles/`, `tabula-distrib/`.
3. Confirm with the user which Qs from §7 they have answered, and
   record the answers inline (replace "Tentative" with "Decision").
4. Begin M1 only after §7 is fully resolved.
5. First M1 commit should be a no-behavior-change refactor that
   introduces the `PluginTransport` interface and keeps the stdio
   implementation as the only one. Nothing user-visible changes.
