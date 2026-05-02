# ADR 0001 — Runtime daemon, execution backends, unified worker model

Date: 2026-05-02
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

The kernel today owns tool execution end-to-end:

- It forks plugin processes as its own children and speaks NDJSON
  over their stdio pipes (`internal/kernel/plugin/runtime.go`).
- It runs skill `[[tools]]` as one-shot `sh -c` subprocesses via
  `internal/kernel/process_manager.go`.
- It propagates context to those processes through `os.Environ()`
  inheritance — no explicit contract for `TABULA_HOME`,
  `TABULA_PROJECT_ROOT`, or workspace.
- Workspace is a convention (a single `Hub.ProjectRoot` string set
  from env), not architecture.

This binding makes several intended deployments impossible:

1. **Remote kernel.** A team kernel in the cloud cannot drive
   plugins that need a developer's laptop FS. Stdio pipes are
   local by definition; an SSH or WSS hop would require a
   process-host on the laptop side.
2. **Multi-host topologies.** GPU drivers on one host, project
   files on another, always-on services on a third — no place
   to put per-host policy without smearing it across the kernel.
3. **Sandboxing.** Per-tenant or per-skill policies (cgroups,
   bwrap, network namespaces) belong somewhere host-local; the
   kernel is the wrong place for them.
4. **Multi-tenant kernel.** Each tenant needs its own plugin
   instances (no plugin sharing). The current model only knows
   one tenant.

Earlier discussion considered a "client-callback workspace"
design where clients (CLI, TUI, telegram) would proxy file
operations back to themselves. That approach was rejected: it
adds a second protocol parallel to the plugin protocol, couples
workspace to client identity (telegram-only tenants have no FS),
and weakens guarantees relative to a real process host.

## Decision

Introduce a **runtime daemon** as the process host for tools, and
make **execution backends** the kernel's only way to reach it.
Adopt a **unified worker model** so plugin and skill execution
share one mechanism end-to-end.

### 1. Runtime daemon (`tabula-runtime`)

A separate binary, always a separate OS process from the kernel
(including in local-mode). Responsibilities:

- Spawn and supervise plugin/skill **workers** locally.
- Speak the **Runtime API** (kernel ↔ runtime daemon) over
  WebSocket-framed JSON.
- Speak the **worker protocol** (runtime ↔ worker) over stdio
  NDJSON to each worker child.
- Multi-tenant on its process pool, keyed by `(kernel_id,
  tenant_id, target_id)`. No worker is ever shared across that
  triple.
- Attach to **multiple kernels concurrently** (N:M). Each
  attachment has its own auth, state directory, and tenant
  whitelist.

The runtime daemon is the only thing that does `os/exec` for
tools. The kernel never spawns plugins or skills.

### 2. Execution backends (kernel-side)

A backend's only job is to produce a `RuntimeConn` to a
`tabula-runtime`. Backends do not host or sandbox tools.

- `local`: kernel forks `tabula-runtime` as a managed child and
  dials a unix socket at `$TABULA_HOME/run/runtime.sock`.
- `attach`: kernel listens; runtime dials in (used for remote
  team setups where the laptop initiates the connection).
- `ssh`: kernel runs `ssh user@host tabula-runtime stdio`
  (out-of-process, system `ssh`).
- `docker` / `k8s` / future sandboxes: kernel launches a
  container/pod with the runtime as PID 1 and attaches.

All backends terminate at the same `RuntimeConn`. The Runtime API
above the seam is identical regardless of backend.

### 3. PluginExecPolicy (runtime-side)

How the runtime spawns and isolates a worker is independent of
how the kernel reached the runtime:

- `bare` (`os/exec`), the M2 default.
- `cgroup`, `sandbox` (bwrap / firejail / sandbox-exec),
  `nested-docker` — opt-in, deferred past M5.

Containerization can appear at the backend layer, the policy
layer, both, or neither. Tenants choose policy via runtime config;
the kernel does not know.

### 4. Unified worker model

Plugins and skills differ only in **worker lifecycle policy**:

- Plugin = long-lived warm worker (TTL=∞).
- Skill = short-lived cold worker by default.

The Runtime API exposes a single `Invoke{target, tool, args,
tenant_id}` op for both. Inside the runtime, a worker is spawned
or reused per `(kernel_id, tenant_id, target_id)` and speaks the
worker protocol. Skills get a runtime-generated per-language
harness (bash, python, node, …) that wraps the `[[tools]] exec`
template; skill authors keep writing markdown frontmatter
unchanged.

`process_manager.go` (kernel-side `sh -c`) is removed in M3.

### 5. Workspace dissolves as a code concept

There is no `workspace` plugin or kernel abstraction. Two
independent plugins expose orthogonal capabilities:

- `fs` (read/list/glob/grep/write/edit) with `roots` config.
- `exec` (subprocess, including `git`, `rg`, scripts) with
  `cwd_default` config.

Capability and scope are independent axes. Tenant configs assemble
"workspace personas" via templates substituting `${project_root}`
into both plugins. `code/workspace`, `files`, and `shell` skills
are removed in M5.

### 6. Multi-tenant invariants

- Tenant namespace is per-kernel. `team:acme` and `local:acme`
  are unrelated.
- `tenant_id` is mandatory on every `Invoke`; `kernel_id` is
  implicit by connection.
- Three independent enforcement points for tenant isolation:
  kernel router (authorization), runtime worker pool (keying),
  worker process (env match check).
- Two-sided runtime/tenant whitelist (kernel-side
  `allowed_runtimes`, runtime-side `allow_tenants`).

### 7. Auth

- M2: bearer tokens (`rtk_<random>`) for both local (file-based)
  and remote (config-based). Hashed at rest in the kernel.
- M3+: mTLS as opt-in second mode for remote; token mode remains.
- Discovery (Q11): static push-config in `runtime.toml`. Pull /
  hybrid IdP discovery is enterprise milestone, deferred.

### 8. Failure semantics

- Fail-fast on disconnect: pending invokes complete with
  `runtime_unavailable` (`retryable: true` hint).
- Runtime reconnects with exponential backoff (1s → 60s),
  indefinitely.
- Warm workers survive disconnects; reload is a separate op that
  evicts them deliberately.
- Default per-call timeout 5 min, per-tool override via manifest.
- Cancel: SIGTERM → 5s → SIGKILL with `cancel_ack`.

### 9. CLI shape

Go binaries expose composable primitives only (`tabula serve`,
`tabula tenant create`, `tabula runtime token issue`,
`tabula status --json`, …). All workflow / orchestration /
prompts / installation checks live in shell scripts (`install.sh`,
`bootstrap.sh`, `tabula-init`, `install-service.sh`). No
`tabula bootstrap` Go subcommand.

### 10. No-legacy commitment

Stdio plugin transport is removed from the kernel atomically in
M2 (the same change that extracts `tabula-runtime`). No
`embedded` backend, no fallback, no fork-the-plugin-myself path
in production code. Tests exercise the real install path through
`bootstrap.sh`. Skill exec via kernel-side `process_manager.go`
is a documented intermediate state during M2 only and is removed
in M3.

## Consequences

### Positive

- Remote kernel becomes a clean configuration choice, not a
  protocol redesign. Same Runtime API everywhere.
- Workspace as a problem dissolves: it becomes a config template,
  not a code abstraction.
- Multi-tenant kernel + N:M runtime support fall out naturally
  from explicit `(kernel_id, tenant_id)` keying.
- Single tool-execution code path in the kernel (one `Invoke`
  RPC) versus today's two (plugin stdio + skill `sh -c`).
- Sandbox policy can be added per-tenant without touching kernel
  code.
- Telegram-only tenants are honestly unsupported for FS/exec
  tools (no runtime → tools unavailable) instead of pretending
  through client callbacks.

### Negative

- Local-mode gains an extra OS process (`tabula-runtime` as
  managed child). Operational footprint slightly larger; one
  added integration point in `bootstrap.sh`.
- Per-call latency adds one Runtime API roundtrip. On unix
  socket: ~0.1ms (negligible). On WAN WSS: tens of ms (noticeable
  for chatty skills; warm workers + future PluginExecPolicy
  sandbox prewarm mitigate).
- Unified worker model requires per-language harnesses (bash,
  python, node initially) and a one-time migration of every
  bundle skill onto the harness contract — roughly 3–5× the cost
  of a bare daemon extract. Accepted to avoid permanent
  skill/plugin duality.
- macOS sandboxing degrades to "path validation only" until a
  proper `sandbox-exec` or container-based PluginExecPolicy is
  implemented. Documented limitation; users wanting hard
  isolation on Mac run a Linux runtime in Lima/UTM and attach.
- Stdio kill in M2 is genuinely atomic: if the daemon extract
  has bugs, plugin execution is broken until fixed. No safety
  net `embedded` backend.

### Neutral

- Worker protocol stays on stdio NDJSON (between runtime daemon
  and worker process). The "no-legacy stdio" rule applies to
  kernel ↔ plugin stdio, not to runtime ↔ worker stdio, which is
  the chosen design.
- Skill author surface (markdown + `[[tools]]` frontmatter)
  does not change. Only the execution mechanism behind it does.

## Alternatives considered

### A. Client-callback workspace
The client (CLI/TUI/telegram) proxies FS calls back to itself.
Rejected because (i) yet another protocol parallel to plugins,
(ii) couples workspace to client identity, (iii) telegram-only
tenants cannot have FS — exposes an architecturally awkward
asymmetry.

### B. Kernel-per-project / kernel-per-tenant processes
Run a separate kernel per tenant. Rejected because it does not
solve remote execution (still needs a process host on the FS
side), and operational cost scales linearly with tenants.

### C. In-process SSH library for the SSH backend
Use `golang.org/x/crypto/ssh` instead of system `ssh`. Rejected
because it requires reimplementing `~/.ssh/config`, ssh-agent
integration, hardware-key support, and known_hosts trust — months
of work for marginal benefit. Out-of-process `ssh` delegates all
of this to OpenSSH.

### D. Sandbox at the backend layer (kernel decides containers)
Make `docker` / `k8s` backends spawn each plugin in its own
container directly, without a runtime daemon. Rejected because
it duplicates supervisor logic, breaks the N:M model (one
container per plugin = no shared host resources across kernels),
and forces sandbox policy decisions onto the kernel that belong
locally.

### E. Keep stdio plugin transport as `embedded` backend for tests
Allow tests to spawn plugins inline without a runtime daemon.
Rejected because (i) AGENTS no-legacy rule, (ii) "test only"
exceptions historically migrate into prod paths, (iii) the
testbed already runs realistic install topologies and gains
strictly stronger guarantees by exercising the real Runtime API.

### F. Big-bang skill→plugin unification with warm workers from day one
Make every skill a long-lived warm worker in M2. Rejected as
premature: skill authors today rely on cold semantics ("process
exits, no state leaks"), warm workers require sandbox layer that
isn't ready, and there's no concrete latency pressure yet.
Decision: M3 unifies the execution mechanism with cold default;
warm-for-skills is a later opt-in.

## Implementation reference

See `docs/plans/REMOTE_RUNTIME.md` for the rolling implementation
plan, milestone breakdown (M1–M6), and outstanding details. This
ADR captures the durable architectural choice; the plan is the
working document for sequencing and issue creation.
