# Claude Code Harness Comparison

This note compares the local Claude Code source snapshot at
`/Users/mak/src/claude-code` with the current Tabula stack across:

- `tabula` kernel and installer
- `tabula-bundles` shared drivers, gateways, tools, memory, and subagents
- `tabula-distrib` distro policy and prompt materializers

The goal is not to clone Claude Code's product surface. The goal is to identify
agent-harness properties that make Claude Code feel capable and resilient, then
separate them from Anthropic-specific implementation details.

## Executive Summary

Tabula already has a strong generic architecture: a small kernel, runtime-owned
plugins, distro-defined policy, WebSocket sessions, hook-based approvals,
driver-side compaction, durable driver history, memory tools, MCP, web/CLI/ACP
gateways, and plugin-owned subagents.

Claude Code's differentiators are mostly not individual tools. They are harness
behaviors around long-running autonomous work:

1. **Context is actively managed as a first-class runtime resource.** Claude
   Code has token accounting, `/context` visualization, automatic full
   compaction, session-memory compaction, microcompaction of old tool results,
   prompt-too-long recovery, and cache-aware forked summarizers.
2. **Tool results and file reads are treated as expensive state, not inert log
   text.** Large results are persisted to disk with previews; duplicate file
   reads and old tool outputs are tracked and compacted.
3. **Subagents are rich task objects.** They have isolated mutable context,
   background lifecycle, progress summaries, output files, notifications,
   optional worktree isolation, remote variants, and permission bubbling.
4. **Git/diff state is continuously surfaced to the harness.** Startup context
   includes a git snapshot. The UI has current diff and per-turn diff views.
   File edits create structured patch data and file-history checkpoints.
5. **Permissions are a central decision pipeline.** Rules, modes, hooks,
   classifiers, bridge callbacks, and swarm-worker delegation all flow through a
   single permission result model before the tool executes.
6. **Session transcripts are append-only operational logs.** They store messages,
   metadata, sidechain agent transcripts, file history, attribution state,
   worktree state, todos, cost, and compaction boundaries.
7. **Remote/IDE/task integrations are part of the harness, not external demos.**
   IDE bridge, remote session manager, direct connect, background task registry,
   and remote agent tasks are integrated with permission and transcript flows.

The biggest gaps for Tabula are therefore:

- no Claude-Code-grade context budget manager or context visualizer;
- no microcompaction/persistent-large-tool-result layer;
- no first-class task object for background subagents;
- no automatic per-task worktree isolation;
- no per-turn diff/file-history subsystem;
- no kernel/runtime-native permission capability model;
- no IDE bridge;
- less integrated telemetry/tracing around tool and context behavior.

## Comparison Matrix

| Capability | Claude Code | Tabula Today | Gap |
|---|---|---|---|
| Startup project context | Injects git snapshot, `CLAUDE.md`, current date | Distro prompt builders inject project files and workspace context | Tabula lacks built-in git snapshot and context health checks |
| Context accounting | Token windows, warning state, `/context`, source breakdown | Usage events and simple percent from providers | Tabula lacks detailed per-source accounting |
| Full compaction | Manual/auto, hooks, summaries, prompt-too-long retry | Driver-side threshold/overflow compaction | Tabula compaction is simpler and driver-local |
| Microcompaction | Clears old tool results and API cache edits | Prunes old tool outputs during compaction only | Tabula lacks continuous tool-result pressure management |
| Large tool results | Persisted to session files with previews | Plugin outputs return through kernel replies/history | Tabula lacks generic result persistence and replacement records |
| Subagents | Async/background, fork, teams, remote, progress, summaries | Plugin-owned subagents, async/sync, send/wait/list/kill | Tabula lacks task lifecycle richness and isolation defaults |
| Worktree isolation | Built-in worktree creation/resume/cleanup and agent worktree notices | No automatic per-agent workspace copy | Major gap for concurrent coding agents |
| Diff awareness | `/diff`, current diff stats, per-turn diff extraction | Structured git/review tools | Tabula lacks ambient/per-turn diff UI/state |
| File history | Pre-edit checkpoints and restore state | No generic file-history subsystem | Major safety/UX gap |
| Permissions | Central allow/deny/ask pipeline, modes, bridge/swarm callbacks | Hook plugins normalize and ask/deny | Tabula policy is powerful but optional/plugin-enforced |
| Session restore | Rich JSONL transcript with metadata and sidechain logs | Driver history plus session SDK transcript/meta | Tabula restore is split and shallower |
| IDE/remote | VS Code/MCP bridge, direct connect, remote session manager | ACP/web/CLI, SSH/WSS runtime ops | Tabula lacks IDE-grade bridge and remote agent UX |
| Telemetry | Analytics, diagnostics JSONL, OTel/Perfetto spans | status/snapshots/log tails | Tabula lacks end-to-end traces and context/tool metrics |

## Claude Code Capabilities Worth Understanding

### 1. Startup Context Snapshot

Claude Code prepends cached context to each conversation:

- `src/context.ts:getGitStatus()` captures branch, default branch, short git
  status, recent commits, and git user, truncated to 2k chars.
- `src/context.ts:getSystemContext()` injects that git snapshot unless disabled
  or in remote mode.
- `src/context.ts:getUserContext()` loads discovered `CLAUDE.md`/memory files
  and current date, with bare-mode and disable switches.

Tabula has distro prompt builders and `before_prompt_build` hooks:

- `internal/kernel/policy.go` supports `session_start` and
  `before_prompt_build` context mutation.
- `tabula-distrib/code-immune/_lib/python/src/code_immune_prompt/builder.py`
  and `claw/_lib/python/src/claw_prompt/builder.py` inject workspace/project
  files such as `AGENTS.md`.

Gap: Tabula treats this as distro prompt assembly. Claude Code treats it as a
standard harness affordance, including git state and context diagnostics. Tabula
could add a generic prompt-context plugin that contributes a bounded git snapshot
and context-health warnings without teaching the kernel about git.

### 2. Context Budget Management

Claude Code has several layers:

- `src/utils/context.ts` resolves context window and max output token limits,
  including 1M-context variants and output reservations.
- `src/services/compact/autoCompact.ts` computes effective window, warning,
  error, auto-compact, and blocking thresholds.
- `src/utils/contextAnalysis.ts` breaks tokens down by human messages,
  assistant messages, local command outputs, tool requests/results,
  attachments, and duplicate reads.
- `src/commands/context/context.tsx` renders what the model actually sees after
  compact/collapse/microcompact transforms.
- `src/query/tokenBudget.ts` can continue generation until a token budget is
  meaningfully used, with diminishing-return stop logic.

Tabula has:

- provider usage events and context percent in
  `tabula_drivers.driver_runtime.DriverRuntime._send_usage_update`;
- rough token estimation and threshold compaction in
  `tabula_drivers.compaction`;
- web gateway display normalization for `usage.update` and compaction events.

Gap: Tabula does not expose a reliable answer to "what is occupying context?"
or "what will the next request send?" It has compaction, but not context
budget introspection. This is likely one reason Claude Code can feel like it
"remembers" better: it has explicit machinery to observe and control the
context envelope.

### 3. Full, Session, and Micro Compaction

Claude Code compaction is multi-tiered:

- `src/services/compact/compact.ts:compactConversation()` creates compact
  boundaries, runs pre/post hooks, summarizes old messages, preserves recent
  segments, reinjects plan/skills/MCP/deferred-tool attachments, logs telemetry,
  and retries prompt-too-long by dropping old API-round groups.
- `src/services/compact/autoCompact.ts:autoCompactIfNeeded()` has recursion
  guards, circuit breaking after repeated failures, and session-memory-first
  fallback.
- `src/services/compact/sessionMemoryCompact.ts` keeps a bounded preserved
  segment while maintaining tool-use/tool-result and thinking-block invariants.
- `src/services/compact/microCompact.ts` clears or API-cache-edits old
  compactable tool results before full compaction is necessary.
- `src/services/AgentSummary/agentSummary.ts` periodically forks a subagent
  transcript for a short progress summary while preserving prompt-cache shape.

Tabula has:

- `tabula_drivers.compaction` with threshold-based summarization, previous
  summary reuse, tail-turn preservation, and old tool output pruning;
- `DriverRuntime._do_process_turn()` emitting `compaction.start/end/error`;
- retry after context overflow in driver tests and behavior.

Gap: Tabula's compaction is effective but narrow. It happens inside the driver
provider session and does not yet carry a first-class compact boundary with
rich metadata, post-compact restoration of attachments, duplicate-read analysis,
or microcompaction. A Tabula-native context manager should probably live as
driver/runtime library code plus a context diagnostics tool, not in the kernel.

### 4. Tool Result Storage and Result Pressure

Claude Code treats large tool results as session artifacts:

- `src/utils/toolResultStorage.ts` persists oversized tool results under the
  session's `tool-results/` directory and replaces the prompt-visible content
  with a preview and file reference.
- The same module enforces aggregate per-message tool-result budgets and tracks
  replacement records for compaction/resume.
- `src/services/compact/microCompact.ts` can clear old results from compactable
  tools such as read, shell, grep, glob, web fetch, edit, and write.

Tabula currently routes tool results as ordinary kernel replies and driver
history entries. Individual plugins may truncate or structure output, but there
is no generic tool-result persistence layer.

Gap: This is one of the most important missing harness properties. Long shell,
grep, test, and file-read outputs can silently dominate model context. Tabula
should consider a generic result-storage contract in the driver/runtime layer:
large tool outputs become durable artifacts with previews, and the model can
read the artifact explicitly when needed.

### 5. Subagents as Task Objects

Claude Code subagents are deeply integrated:

- `src/tools/AgentTool/AgentTool.tsx` supports sync and async agents,
  backgrounding, model override, named teammates, team context, worktree/remote
  isolation, cwd override, and permission mode.
- `src/utils/forkedAgent.ts` creates cache-safe forked contexts and isolates
  mutable state such as file-read cache and content replacement state.
- `src/tools/AgentTool/forkSubagent.ts` implements implicit fork agents that
  inherit full parent context with byte-stable placeholder tool results for
  prompt-cache sharing.
- `src/tasks/LocalAgentTask/LocalAgentTask.tsx` tracks progress, token counts,
  recent activity, pending messages, retained/disk-loaded state, and emits
  `<task-notification>` results.
- `src/services/AgentSummary/agentSummary.ts` generates periodic short progress
  summaries.

Tabula has a capable subagent bundle:

- `tabula-bundles/subagents/subagents/run.py` owns spawning, listing, waiting,
  killing, steering, result registry, child limits, and allowed-tools hooks.
- `tabula_drivers.subagent_runtime` runs child sessions, writes history, queues
  follow-up messages, and can deliver async results to the parent.

Gap: Tabula subagents are sessions/processes with tools. Claude Code subagents
are also UI/task/runtime objects with progress state, summaries, output files,
notifications, isolated mutable context, and optional worktree/remote execution.
Tabula should add a generic task model around subagents rather than putting more
behavior into the kernel session concept.

### 6. Worktree Isolation

Claude Code has first-class worktree support:

- `src/utils/worktree.ts` validates names, creates/resumes `.claude/worktrees/*`,
  fetches base branch, supports sparse checkout, copies `.worktreeinclude`
  gitignored files, copies local settings, configures hooks, symlinks configured
  directories, and cleans up or keeps worktrees.
- `src/tools/AgentTool/forkSubagent.ts:buildWorktreeNotice()` tells child agents
  that inherited paths refer to the parent cwd and must be reread in the
  isolated worktree.

Tabula does not currently create per-subagent or per-task worktrees. It has
workspace roots, fs policy, and exec cwd controls, but subagents normally share
the configured workspace.

Gap: This is a major concurrency and safety difference. Without worktree
isolation, multiple Tabula agents can step on each other's edits. A good Tabula
version would be a reusable workspace-isolation plugin or subagent option that
creates a git worktree and passes the adjusted workspace root into the child
driver/tool policy.

### 7. Git and Diff Awareness

Claude Code uses git in three ways:

- Context snapshot: `src/context.ts:getGitStatus()` injects current branch,
  status, recent commits, and default branch at conversation start.
- Current diff UI: `src/utils/gitDiff.ts` fetches bounded stats and hunks,
  skips transient git states, includes untracked file names, and avoids large
  diffs.
- Per-turn diff UI: `src/hooks/useTurnDiffs.ts` extracts file edits from
  `FileEditTool` and `FileWriteTool` structured patch outputs and groups them
  by user turn.

Tabula has structured git/review skills in `tabula-bundles/code` that can
produce status, diffs, commits, and reviews on demand. The agent can run
`git diff` through tools, but the harness does not continuously track per-turn
edits or present ambient diff state.

Gap: Tabula's git is tool-centric; Claude Code's git is also state-centric.
Adding structured edit metadata and per-turn diff state would improve review,
rollback, commit-message generation, and user trust.

### 8. File History and Edit Safety

Claude Code has file checkpointing:

- `src/utils/fileHistory.ts` tracks pre-edit file state and records snapshots in
  session state.
- `src/utils/sessionRestore.ts` restores file-history state from transcript on
  resume.
- edit/write tool outputs include structured patch information consumed by diff
  UI.

Tabula's file tools can edit files and history captures tool outputs, but there
is no generic pre-edit checkpoint ledger or restore path.

Gap: This is a practical safety feature. Tabula should consider a generic file
operation hook that records pre-edit checksums/content previews and post-edit
structured patches for any tool that mutates workspace files.

### 9. Permission Pipeline

Claude Code centralizes permission decisions:

- `src/hooks/useCanUseTool.tsx` is the common allow/deny/ask gateway.
- `src/utils/permissions/permissions.ts` merges allow/deny/ask rules from
  settings, CLI args, command, and session; handles permission modes,
  bypass-immune safety checks, classifier decisions, denial tracking, and
  headless/background behavior.
- Permission requests can route through interactive UI, coordinator handler,
  swarm-worker handler, bridge callbacks, or remote channels.

Tabula has strong hook/plugin policy:

- `tabula_plugin_sdk.tool_policy` normalizes resources and operations for fs,
  shell, pairing, memory, MCP, subagents, cron/timer, gateway, session, question,
  and todo tools.
- `hook-permissions` and `hook-approvals` enforce allow/ask/deny and remember
  approvals through `exchange.approve`.
- The kernel's `PolicyEngine` dispatches before-tool-call hooks.

Gap: Tabula permissions are powerful but convention/plugin dependent. If the
permission plugins are absent or misconfigured, the kernel dispatch layer is not
a capability system. Claude Code's model is closer to a mandatory permission
gateway before tool execution. Tabula could preserve kernel genericity while
making runtime tool dispatch require an explicit policy decision from a
configured policy target, with safe defaults per distro.

### 10. Tool Pool Management and Deferred Tools

Claude Code keeps model-visible tools stable and searchable:

- `src/tools.ts:getAllBaseTools()` is the source of truth for built-ins and
  feature-gated tools.
- `src/tools.ts:assembleToolPool()` merges built-ins and MCP tools, filters
  denied tools, deduplicates, and sorts built-ins/MCP for prompt-cache stability.
- `src/tools/ToolSearchTool/ToolSearchTool.ts` lets the model search deferred
  tools instead of seeing every possible tool schema up front.
- Compaction carries deferred tool and MCP instruction deltas forward.

Tabula has dynamic tool catalogs from runtime workers and MCP, and visible-tools
filtering for agents. However, model-visible tool selection is mostly distro and
plugin policy, not a global cache-aware tool-pool strategy.

Gap: As Tabula gains more tools, prompt bloat will grow. A deferred tool-search
layer, plus deterministic tool ordering and compact-boundary tool metadata,
would improve cache stability and context use.

### 11. Memory Systems

Claude Code combines several memory channels:

- `CLAUDE.md` discovery in `src/context.ts`.
- typed memory directory in `src/memdir/memdir.ts`, including `MEMORY.md`
  truncation and taxonomy guidance;
- agent-specific memory in `src/tools/AgentTool/agentMemory.ts`;
- session-memory compaction and extraction services;
- team memory synchronization in gated code paths.

Tabula has:

- project prompt files through distro builders;
- MemPalace bundles for persistent memory;
- skills and persistent memory tools;
- driver-side compaction summaries.

Gap: Tabula memory is available as tools and prompt files, but less integrated
with context budgeting and session compaction. Claude Code's memory system is
more explicit about what is always loaded, what should be searched, and what is
session-only versus cross-session.

### 12. Session Persistence and Resume

Claude Code uses append-only JSONL transcripts as a shared operational log:

- `src/utils/sessionStorage.ts` records messages, metadata, sidechain agent
  transcripts, compact boundaries, content replacement records, worktree state,
  tags/titles, and PR links.
- `src/utils/sessionRestore.ts` restores file history, attribution, context
  collapse state, todos, session agent/model, cwd, and worktree state.
- `src/cost-tracker.ts` restores cost state per session.

Tabula persistence is split by authority:

- kernel SQLite stores canonical session aggregates, committed events, and auxiliary session records;
- plugins own their durable domain state under tenant plugin state;
- subagents own registry/result/log files;
- gateways reconstruct durable transcript from protocol-v4 committed events and merge only transient live display state.

Gap: a resumed agent may still have fewer standardized harness projections to
restore. New projections should consume committed events, auxiliary records, and
component-owned APIs rather than introduce another session persistence format.

### 13. Remote and IDE Integrations

Claude Code integrates remote/IDE into the harness:

- remote session manager handles WebSocket/HTTP send/receive and permission
  requests;
- direct connect bridges stream-json sessions;
- VS Code MCP bridge sends file-update notifications;
- remote agent tasks have eligibility checks and completion notifications.

Tabula has ACP, web, CLI, Telegram, SSH runtime, WSS runtime attach, and status
snapshots. These are strong operator/runtime integrations, but there is no
first-class VS Code/JetBrains style IDE bridge in the current repos.

Gap: IDE integration is a major product surface for coding agents. The ACP
gateway gives Tabula a path, but a dedicated IDE bridge could share permission,
diff, file-history, and session-task state with editors.

### 14. Telemetry and Diagnostics

Claude Code has detailed harness telemetry:

- diagnostics JSONL through `src/utils/diagLogs.ts`;
- analytics event facade with PII marker types;
- context usage metrics and duplicate-read metrics;
- OTel/Perfetto tracing for sessions/tool permissions;
- doctor warnings for large memory files, agent descriptions, MCP tools, and
  unreachable rules.

Tabula has health/status endpoints, runtime snapshots, gateway status/log tails,
and structured runtime logs. It does not yet have end-to-end trace correlation
across kernel, runtime, driver, gateway, plugins, and tool calls.

Gap: Tabula should add correlation IDs and structured event streams before the
system becomes harder to debug. This can stay optional and local-first.

## Prioritized Opportunities for Tabula

### P0: Context Visibility and Tool-Result Pressure

Add a generic context diagnostics capability that answers:

- current estimated context use by source;
- top tool-result contributors;
- duplicate file reads;
- compact boundaries and last compaction summary;
- what will be sent to the provider on the next turn.

Pair this with a driver/runtime tool-result artifact layer:

- persist large outputs under session state;
- replace prompt-visible content with bounded previews;
- let the agent explicitly read full artifacts;
- carry replacement metadata through resume and compaction.

This would directly address the user's suspicion about "special context
retention". Claude Code does not have magic memory; it has aggressive accounting
and pressure management.

### P1: First-Class Task Model for Subagents

Keep sessions as routing boundaries, but add a task abstraction around long
running work:

- task id, description, prompt, owner session, status;
- result file and notification protocol;
- progress counters and current activity summary;
- cancellation and retention policy;
- child permission mode and allowed tool set;
- optional workspace/worktree binding.

Tabula's existing subagents plugin can become the first producer of these task
objects.

### P1: Worktree Isolation

Add a reusable workspace-isolation component:

- create/resume/cleanup git worktrees under Tabula state or workspace-local
  metadata;
- copy configured gitignored files;
- pass isolated workspace roots to fs/exec policy;
- inject a child-agent notice explaining inherited context paths;
- expose keep/cleanup controls.

This is the clearest safety improvement for concurrent coding agents.

### P1: Structured Edit and Diff Ledger

Extend file mutation tools or hooks to emit standard edit events:

- file path, before checksum, after checksum;
- structured patch hunks, added/removed line counts;
- turn id/session id/task id;
- whether the file was created, updated, deleted, or binary.

Use this for per-turn diff UI, commit summaries, rollback, and review.

### P2: Mandatory Permission Gateway Shape

Preserve plugin-defined policy, but make tool dispatch explicitly policy-aware:

- every tool call should have a policy decision object;
- absence of policy can be a distro choice, but not accidental;
- decisions should be logged and replayable;
- headless/background agents should have deterministic behavior when a prompt
  would otherwise be required.

### P2: Deferred Tool Discovery

Introduce a generic tool-search/deferred-tool mechanism for large tool catalogs:

- model sees a small stable base set plus `deferred_tool_search`;
- tool schemas are loaded on demand;
- compaction boundaries preserve selected/deferred tool state;
- prompt-cache stability becomes an explicit design goal.

### P2: Session Projection Unification

Define shared projections over protocol-v4 committed events, auxiliary session
records, and component-owned APIs without adding another persistence format:

- messages and stream reconstruction;
- compaction boundaries;
- artifact references;
- file edits;
- task/subagent state;
- titles/tags/labels;
- provider usage and cost;
- resume metadata.

This preserves Tabula's kernel boundary while giving products a common resume
substrate backed by canonical authority.

### P3: IDE Bridge

Build on ACP or add a dedicated plugin/gateway for editor integration:

- file-open and file-updated notifications;
- inline diff/review handoff;
- editor-side permission prompts;
- session/task browser;
- diagnostics and context meter.

## What Not To Copy Directly

- Do not move product policy into the Tabula kernel. Claude Code is a monolith;
  Tabula's kernel/distro/plugin split is a strength.
- Do not hardcode a Claude-specific memory file name or Anthropic-only cache
  behavior into the kernel. Implement generic hooks and driver capabilities.
- Do not make every feature a built-in tool. Tabula's runtime-owned plugin
  model is better for reusable distros.
- Do not add compatibility aliases or legacy surfaces while porting concepts.
  Tabula's one-name-per-concept rule should hold.

## Suggested Implementation Shape

A Tabula-native version of the useful Claude Code harness properties could be
split like this:

| Area | Likely Home |
|---|---|
| Context accounting, compact summaries, result artifacts | `tabula-bundles/_lib/python/src/tabula_drivers` plus a reusable plugin |
| Context visualizer tool | `tabula-bundles/code` or `base/context` plugin |
| Large tool-result persistence | driver/runtime library, with plugin opt-in metadata |
| Task model | `tabula-bundles/subagents` plus auxiliary session records |
| Worktree isolation | `tabula-bundles/workspace` plugin or subagents companion plugin |
| Structured edit events | fs/edit plugin plus `edit.diff` session records |
| Per-turn diff UI | gateway-web committed events plus `edit.diff` records |
| Mandatory policy decision logging | hook-permissions + kernel/runtime dispatch metadata |
| IDE bridge | new gateway/plugin, probably ACP-adjacent |

## Bottom Line

Claude Code's most important agentic properties are not hidden model prompts or
secret memory. They are operational harness features: context pressure control,
large-result persistence, cache-aware subagent forking, task lifecycle state,
worktree isolation, structured diff/file history, and a unified permission
pipeline.

Tabula already has a cleaner extension architecture. The next step is to add
these harness affordances as reusable runtime and bundle capabilities, while
keeping the kernel generic.
