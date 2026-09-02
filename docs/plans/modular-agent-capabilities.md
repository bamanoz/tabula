# Modular Agent Capabilities

Status: Approved
Date: 2026-07-29
Approved: 2026-07-29
Issue: `issues/01-modular-capability-architecture.md`
ADR: `docs/adr/0012-modular-agent-capabilities.md`

## Purpose

Define optional `continuity`, `activity`, `reflection`, `initiative`, and
`evolution` capabilities without adding product semantics to Tabula kernel,
MemPalace, or generic base bundles.

Maintainer approved this architecture on 2026-07-29. Capability implementation
may proceed through issues 02-14 in dependency order; activation remains governed
by each capability contract.

## Source Study

### Ouroboros

Ouroboros provides behavior to study, not module boundaries to copy.

- Identity is stored as `memory/identity.md` by `ouroboros/memory.py:31-34` and
  injected into model context by `ouroboros/context.py:663-675`. Its
  constitution treats identity as a living manifesto and continuity channel in
  `BIBLE.md:24-77`. This coupling is intentionally not copied: Tabula continuity
  must remain independent from generalized memory.
- Post-task reflection is selected from errors, expensive runs, evolution, and
  workspace tasks by `ouroboros/reflection.py:175-204`. It generates structured
  memory and backlog candidates in `ouroboros/reflection.py:365-473`; identity
  changes are recorded for review instead of applied directly in
  `ouroboros/reflection.py:497-549`.
- Background consciousness owns a persistent wake loop, pause/resume around
  foreground work, budget checks, event logging, and a restricted tool set in
  `ouroboros/consciousness.py:109-251` and
  `ouroboros/consciousness.py:556-597`. `prompts/CONSCIOUSNESS.md` explicitly
  forbids shell, commits, reviews, subagent scheduling, and direct evolution
  from background mode.
- The activity dashboard is a projection, not a second scheduler. It combines
  running/queued tasks, background-consciousness status, and scheduled work
  through existing APIs in `web/modules/activity.js:1-146`. Scheduled state and
  dispatch live in `supervisor/queue.py:250-569`.
- Evolution campaigns are blocked in light mode before campaign or queue state
  is created in `supervisor/evolution_lifecycle.py:44-62`. Campaign and
  transaction state is durable in `state/evolution_campaign.json`, with
  resumable pause, terminal stop, transaction archive, and deterministic
  worktree cleanup in `supervisor/evolution_lifecycle.py:64-190`.
- Evolution dispatch waits for idle state, restart verification, owner binding,
  cost accounting, budget reserve, and circuit breakers. Three consecutive
  failures and repeated non-absorbed objectives pause execution in
  `supervisor/queue.py:1378-1581`.
- Advanced mode cannot write safety-critical, frozen-contract, or release
  invariant paths; pro mode can, but still requires review. Path normalization
  and case-insensitive checks are in `ouroboros/runtime_mode_policy.py:14-137`.
- Mutation is gated by durable reviewed commits and verification receipts.
  Repeated identical blocked diffs are capped in
  `ouroboros/tools/commit_gate.py:54-126`; multi-model review lives in
  `ouroboros/tools/review.py:1-110`; host-attested checks and receipts live in
  `ouroboros/tools/verify.py:1-91`.
- Recovery ownership is outside the mutable worker. `launcher.py:1` describes
  an immutable outer launcher. Git rescue snapshots preserve status, binary
  diffs, tracked changes as Git objects, and untracked files in
  `supervisor/git_ops.py:453-560`. Restart tries development code and falls back
  to stable after dependency and import checks in
  `supervisor/git_ops.py:997-1039`.
- Managed updates are planned in an isolated worktree, protected by a
  fail-closed transaction marker, and expose apply, rollback, smoke, and boot
  finalization stages in `supervisor/update_merge.py:37-153` and
  `supervisor/update_merge.py:160-520`. Manual rollback creates a rescue
  snapshot before reset in `supervisor/git_ops.py:1298-1345`.

### Tabula

Current Tabula contracts already provide most lifecycle seams.

- `CONTEXT.md` defines bundles as install-time units, plugins as executable
  runtime targets, tenants as project-scoped isolation namespaces, and distro
  trees as transactionally replaced installed content.
- `docs/ARCHITECTURE.md` keeps product policy in distros/bundles and describes
  tenant-pinned component surfaces under
  `$TABULA_HOME/tenants/<tenant>/...`.
- ADR 0006 makes tenant-scoped plugin workers the default and permits
  runtime-scoped workers only for explicit shared sidecars.
- ADR 0008 adds generic `before_turn`, `after_turn`, and `before_compaction`
  lifecycle hooks so bundles can own domain behavior without kernel semantics.
  The implemented hook types are declared in
  `internal/kernel/hooks/types.go:68-70` and dispatched by
  `internal/kernel/policy.go:138` and `internal/kernel/helpers.go:122-148`.
- ADR 0009 makes each project-scoped tenant pin one exact immutable distro
  generation and isolate config, sessions, state, cache, logs, and workers.
- The extensions bundle exports `tabula_client_sdk`; protocol-v4 `session.get`,
  `session.subscribe`, and `session.record.*` are the generic session read and
  auxiliary-record surfaces for bundle consumers.
- The productivity bundle already owns `cron`, `todo`, and `wait`; the async
  bundle owns deferred calls and tool-result artifacts. Their bundle manifests
  depend only on generic extension SDKs.
- MemPalace consumes `before_turn` through its own plugin in
  `tabula-bundles/mempalace/mempalace/scripts/run.py:304-306`. This proves that
  context injection does not require kernel-owned memory or identity semantics.
- Security bundles gate actor-aware tool calls through `before_tool_call`; for
  example `hook-approvals/run.py:130-131` and
  `hook-permissions/run.py` own policy outside the kernel.

## Decision Drivers

- Behavioral capabilities must be independently installable.
- MemPalace remains generalized memory. It receives no identity, biography, or
  personality policy.
- Kernel remains a generic router, lifecycle coordinator, and persistence
  boundary.
- State must be tenant-scoped and survive worker restart.
- Optional integration must improve behavior without becoming a hard dependency.
- Evolution safety must be enforced by executable host-side checks, not prompts.
- Installed-layout testbeds must execute each capability and its recovery path.

## Proposed Bundle Graph

Behavioral bundles have no dependency on one another:

```text
continuity  -> extensions
activity    -> extensions
reflection  -> extensions
initiative  -> extensions
             optional: productivity, subagents, activity

evolution   -> extensions
             required generic primitives when implemented:
               subagent orchestration SDK
               durable task/scheduling API
               artifact storage API
               workspace VCS component
               evolution-owned candidate artifact lifecycle
             optional: reflection, activity, continuity
```

`tabula_client_sdk` is the generic tenant/session protocol surface. Behavioral
capabilities reconstruct canonical transcript evidence from committed events and
store bounded opaque diagnostics with session records when needed. No behavioral
bundle may import another behavioral bundle's private package or state files.

Bundle dependency declarations are transitive install contracts, not runtime
service discovery. Optional integrations use capability discovery and degrade
cleanly when absent.

## Common Component Contract

Each capability is delivered as a tenant-scoped warm plugin plus optional
instruction-only skills. A plugin:

- owns its domain state and schema version;
- uses `tabula_plugin_sdk` path/config helpers;
- subscribes only to generic hooks/events;
- exposes explicit tools for inspection and control;
- identifies all emitted ledger records with `producer`, schema version,
  tenant, session when applicable, and correlation ID;
- treats malformed owned state as a visible error or restores a validated
  last-good snapshot; it never silently treats corruption as empty state;
- stops background children and releases locks on shutdown;
- never assumes a specific distro, model provider, gateway, workspace path, or
  MemPalace installation.

Capability state uses `tabula_plugin_sdk.plugin_state_dir(<plugin-id>)`, which
resolves under `$TABULA_HOME/tenants/<tenant>/state/plugins/<plugin-id>/`.
Paths below are relative to that plugin state directory unless stated otherwise.

## Capability Contracts

### Continuity

**Owner:** `continuity` bundle and plugin.

**Purpose:** persistent agent identity and curated biography across sessions and
restarts. Identity is product behavior, not generalized memory.

**State:**

```text
identity.md
biography.jsonl
candidates.jsonl
meta.json
last-good/
```

`identity.md` is current reviewed self-description. `biography.jsonl` is
append-only, provenance-bearing history. Candidate changes never overwrite
identity automatically.

**Hooks:**

- `before_turn`: inject bounded identity and relevant biography context.
- `after_turn`: observe candidate-worthy identity/biography events; no automatic
  identity rewrite.
- `before_compaction`: preserve continuity references needed after compaction.

**Tools:**

- `continuity_status`
- `continuity_read`
- `continuity_propose_update`
- `continuity_apply_candidate`
- `continuity_reject_candidate`
- `continuity_append_biography`

Applying an identity candidate is manual unless distro policy installs a
separate approval workflow. Evolution may propose candidates but may not bypass
this contract.

**Optional integrations:** MemPalace may index continuity records through its
public ingestion surface; continuity never writes MemPalace private state.
Reflection may submit candidates through continuity tools/SDK. Without either,
continuity remains complete.

### Activity

**Owner:** `activity` bundle and plugin.

**Purpose:** curated cross-session work history and current capability activity.
Like Ouroboros UI, it is a projection over existing lifecycle evidence, not a
scheduler or kernel task registry.

**State:**

```text
events.jsonl
checkpoints.json
meta.json
last-good/
```

**Hooks/events:** `after_turn`, `after_tool_call`, `session_start`, `session_end`,
and explicit events from optional components. Raw events are normalized into a
small domain schema such as `work.started`, `work.completed`, `work.failed`,
`initiative.cycle`, and `evolution.candidate`.

**Tools:**

- `activity_list`
- `activity_get`
- `activity_status`
- `activity_rebuild_projection`

Activity stores references to large artifacts, not duplicate payloads. It has no
cancel, schedule, or mutation authority; those remain with owning components.

**Optional integrations:** initiative and evolution may publish richer events.
Absent publishers produce a smaller but valid history.

### Reflection

**Owner:** `reflection` bundle and plugin.

**Purpose:** post-work synthesis from completed, failed, expensive, or explicitly
requested work.

**State:**

```text
reflections.jsonl
candidates.jsonl
checkpoints.json
last-good/
```

**Hooks:** `after_turn` selects eligible work and records a durable pending
reflection before model invocation. `before_compaction` may emit a compact
reflection reference, not rewrite conversation history.

**Tools:**

- `reflection_run`
- `reflection_list`
- `reflection_get`
- `reflection_retry`
- `reflection_status`

Reflection output separates:

- narrative synthesis;
- evidence references;
- reusable knowledge candidates;
- continuity candidates;
- deferred-work candidates.

It never writes continuity, MemPalace, todo, or evolution state directly.
Integrations call public tools/SDKs only. Missing integrations leave candidates
pending and visible.

### Initiative

**Owner:** `initiative` bundle and plugin.

**Purpose:** bounded autonomous observation, planning, and optional work dispatch
between foreground turns.

**State:**

```text
config.json
cycles.jsonl
pending.json
lease.json
last-good/
```

**Lifecycle:** one tenant-scoped scheduler loop with durable next-wakeup and
single-cycle lease. Startup reconciles expired leases before dispatch. Foreground
work pauses mutative initiative cycles; shutdown cancels children and persists
state. Budget, retry, repeated-failure, and repeated-no-progress breakers stop
automatic dispatch.

**Tools:**

- `initiative_status`
- `initiative_start`
- `initiative_pause`
- `initiative_resume`
- `initiative_stop`
- `initiative_run_once`
- `initiative_set_wakeup`

**Security:** initiative uses a distro-configured actor and explicit tool allowlist.
Default allowed work is read/observe, continuity proposals, activity publication,
and durable task proposal. Shell, filesystem mutation, VCS mutation, subagent
spawn, evolution activation, and proactive user messaging are denied unless the
installed distro grants each capability. Prompt text cannot widen authority.

**Optional integrations:** productivity may provide durable scheduling;
subagents may execute approved work; activity may project cycles. Without them,
`initiative_run_once` and internal durable wakeup still work, but execution is
limited to installed tools.

### Evolution

**Owner:** `evolution` bundle, evolution plugin, and separate
`evolution-supervisor` recovery component.

**Purpose:** generate, review, validate, activate, observe, and recover isolated
release candidates.

**Configuration has independent axes:**

```text
scope_mode = light | advanced | pro
activation = manual | automatic
```

A campaign may lower activation authority for a candidate but cannot raise the
configured authority.

**Scope modes:**

- `light`: analysis, diagnostics, and proposals only. No source, bundle, distro,
  generated candidate, installer, or kernel mutation.
- `advanced`: may mutate extension layer only: plugins, skills, prompts,
  subagent presets, distro config/materializers, and gateway extensions.
- `pro`: may also mutate kernel, protocol, installer, shared SDKs, and evolution
  implementation.

All modes still pass policy, review, verification, testbed, and activation
checks. Mode changes are user/distro configuration, never campaign output.

**State:**

```text
campaigns/<id>.json
transactions/<id>.json
receipts/<id>.jsonl
activation.json
health.json
last-good/

$TABULA_HOME/run/evolution-supervisor/<tenant>/  # transient supervisor markers
```

Candidate source/worktrees live in configured workspace roots outside
`TABULA_HOME`; sealed release artifacts and retained known-good releases are owned
by the evolution supervisor. Installer provides only crash-safe staged replacement
of the stable distro path; evolution does not depend on installer history,
candidate, known-good, or rollback APIs.

**Lifecycle:**

1. Capture objective, scope mode, activation ceiling, base artifact, budget,
   actor, and correlation ID.
2. Create isolated worktree through generic VCS API.
3. Produce changes through approved tools/subagents.
4. Classify every changed path against mode scope using canonicalized paths and
   component ownership. Fail closed on unknown or ambiguous ownership.
5. Record immutable diff fingerprint and verification receipts.
6. Run required review and canonical installed-layout testbeds.
7. Build and seal an immutable release artifact without changing active runtime.
8. Under manual activation, stop and request approval. Under automatic
   activation, continue only within configured authority.
9. External supervisor retains the current known-good release, validates the
   expected active release, and applies the target-specific activation adapter.
10. Distro/plugin activation uses crash-safe stable-path replacement and reload;
    Go runtime/kernel activation switches a separately built runtime release and
    restarts processes through the supervisor-owned service boundary.
11. Observe process, kernel, runtime, tenant, release identity, smoke checks, and
    crash-loop state from outside candidate-loaded code.
12. Commit the healthy release or restore and verify the retained full known-good
    release; reconcile interrupted supervisor transactions after restart.

**Tools:**

- `evolution_status`
- `evolution_start`
- `evolution_pause`
- `evolution_stop`
- `evolution_list_candidates`
- `evolution_inspect_candidate`
- `evolution_approve_candidate`
- `evolution_reject_candidate`
- `evolution_activate_candidate`
- `evolution_rollback`

**Recovery component:** `evolution-supervisor` runs outside candidate-mutated
code. It owns activation marker reconciliation, health timeout, boot-loop
counter, retained known-good release rollback, and recovery receipts. It does not own
campaign strategy or code generation. Existing test distros remain outside this
scope.

**Breakers:** evolution pauses on repeated failures, repeated identical
non-progress objectives, unavailable accounting, budget reserve, active
foreground work, missing review/verification receipt, corrupt transaction state,
or unverified restart. Automatic resume never overrides explicit owner stop.

## Generic Primitives Versus Product Behavior

Issue 01 adds no kernel contract. Later issues may add these reusable components
outside the kernel:

| Generic primitive | Generic owner | Used by |
|---|---|---|
| Transitive bundle dependency resolution | installer | all composed bundles |
| Subagent orchestration SDK | subagents bundle | initiative, evolution, other clients |
| Durable task/scheduling API | productivity/shared bundle | initiative, distros, other automation |
| Artifact storage API | async/shared bundle | reflection, activity, evolution |
| Structured workspace VCS component | workspace/shared bundle | evolution, coding workflows |
| Reviewed change transactions | evolution/shared component | evolution, other mutative automation |
| Generic host-service component | installer/materializer + bundle contract | evolution supervisor, other external services |

The table describes broadly reusable mechanics only. It must not contain
identity prompts, initiative policy, evolution modes, product UI text, default
goals, or recovery decisions specific to these capabilities.

## Optional Integration Rules

- Discover integrations through advertised tools/SDK capabilities, never by
  reading another plugin's private files.
- A missing optional integration returns `unavailable`, preserves candidate
  state, and does not fail plugin startup.
- Cross-capability writes carry source, actor, tenant, session/correlation ID,
  evidence references, and idempotency key.
- Consumers validate schema and authority. Producer identity alone does not
  authorize mutation.
- No behavioral bundle is a transitive dependency of another behavioral bundle.
- Distro materializers choose installed set and defaults. Kernel never selects a
  capability set.

## Uninstall And Reinstall

- Uninstall removes installed code, manifests, skills, default config, and app
  surfaces through normal distro replacement.
- Tenant-owned capability state is preserved by default. Explicit purge is a
  separate user action owned by the capability or installer.
- Removing a bundle removes its hooks and tools without leaving required imports
  in another behavioral capability.
- Background workers and child processes stop before component removal.
- Reinstall reads the current schema version, validates state, and either
  migrates through an explicit supported migration or stops with a recoverable
  diagnostic. Corrupt state is not silently reinitialized.
- Optional-integration candidates remain inspectable after provider removal.

## Security Boundaries

- Tenant-scoped workers and tenant paths are mandatory.
- Actor identity and activation authority are explicit inputs to mutative calls.
- Existing permission/approval hooks remain authoritative for tool calls.
- Initiative and evolution receive separate actor identities and policy grants.
- Evolution scope enforcement occurs in structured VCS/candidate transactions,
  not only in prompt instructions or filename globbing.
- Paths are canonicalized before classification; symlink escape, case-folding,
  path traversal, unknown ownership, and generated-file indirection fail closed.
- Review and verification receipts bind candidate fingerprint, base release,
  checks, results, actor, and timestamp.
- Candidate activation is owned by the external supervisor. Capability code may
  request activation but may not rewrite active release references directly.
- Installer owns only crash-safe replacement of the stable distro tree; it owns
  no candidate history, known-good policy, or operator rollback surface.
- `evolution-supervisor` runs from a retained known-good host-service artifact
  and cannot be replaced by the candidate whose activation it controls.
- Secrets and private conversation payloads are referenced or redacted in
  activity/reflection evidence, not copied into general logs.

## Failure And Recovery Matrix

| Capability | Failure | Required behavior |
|---|---|---|
| continuity | corrupt identity/meta | refuse silent empty identity; expose last-good recovery |
| activity | partial event write | append/lock or atomic checkpoint; rebuild projection idempotently |
| reflection | crash after pending record | retry same idempotency key; never duplicate applied candidates |
| initiative | restart during cycle | expire/reconcile lease before next cycle; no duplicate dispatch |
| initiative | repeated failure/no progress | pause automatically and expose reason |
| evolution | crash before activation | candidate remains inactive and inspectable |
| evolution | crash during pointer switch | supervisor reconciles atomic activation marker |
| evolution | unhealthy candidate/boot loop | deterministic retained known-good release rollback |
| evolution | corrupt transaction marker | fail closed; supervisor recovery required |

## Testbed Contract

Every implementation issue must update both:

- canonical installed-layout suite under `tools/tabula-testbed/`;
- generated testbed template used for new suites/installations.

Catalog-only assertions are insufficient. Tests must call installed tools or
exercise installed hooks through kernel/runtime/client surfaces.

Minimum capability scenarios:

- `continuity`: install alone; apply reviewed identity; start new session and
  restart runtime; verify identity/biography injection; corrupt state and verify
  visible last-good recovery; uninstall and verify preserved state/no hooks.
- `activity`: install alone; execute success and failure work across sessions;
  restart; query normalized history; rebuild projection; verify malformed event
  handling and no scheduler authority.
- `reflection`: install alone; execute eligible successful and failed work;
  verify durable synthesis and candidates; restart/retry without duplication;
  run with continuity/MemPalace absent.
- `initiative`: install alone; execute `initiative_run_once`; verify allowlist
  denial for unavailable mutation; restart during leased cycle; verify one
  recovery and breaker behavior; optional scheduler/subagent integration gets a
  separate scenario.
- `evolution`: execute installed evolution tools; prove light mode creates no
  mutation; prove advanced mode can activate an extension-only candidate but
  rejects core changes; prove a broken pro candidate triggers health failure and
  supervisor rollback across restart; cover both manual and automatic activation.
- generic primitive issues: execute installed consumer component, including
  interrupted operation and recovery, rather than only unit-testing API shape.

Each suite checks generated-template parity in the same change. Installed-layout
or recovery regressions cannot be accepted with unit tests alone.

## Explicitly Out Of Scope

- Local-model discovery, download, serving, or model-selection tools. Providers
  remain configured through existing driver/provider surfaces.
- Skill marketplace or ClawHub replacement. Existing `skill-issue` and distro
  composition remain separate concerns.
- Browser, image, audio, video, or media implementations. Existing MCP/plugin
  integrations may supply them.
- Product UI design. Activity API may later support UI, but no frontend is part
  of this architecture issue.
- Porting Ouroboros prompts, constitution, identity, default goals, or product
  language into generic Tabula components.
- Using a product-specific distro as evolution recovery infrastructure.

## Approval

Maintainer approved this design on 2026-07-29. Issues 02-14 may proceed in
recorded dependency order. Any change to bundle boundaries, evolution authority,
or recovery ownership requires a superseding ADR.
