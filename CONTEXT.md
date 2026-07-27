# Tabula Context

This file names the domain concepts agents should use when working in this repo. It is implementation-first: prefer current code and tests over older prose when they disagree. ADRs remain decision history and should be checked for architectural intent.

Last reviewed: 2026-06-26.

## Repository Identity

Tabula is an environment for building and living with agents: a small Go kernel, a plain-files runtime home, runtime-hosted plugins, prompt skills, distro installers, and testbed tooling.

This repo owns the generic Tabula core:

- Go kernel and CLI entrypoints.
- Runtime daemon protocol and local runtime host.
- Distro installer and app materializer tooling.
- Testbed runner and SDK used by installed test suites.
- Core docs, ADRs, and shared development tooling.

Do not put concrete distro/product policy here. Distro policy belongs in `tabula-distrib`. Bundle, plugin, and skill implementations belong in `tabula-bundles` unless they are minimal fixtures or core tests.

## Trust Order

Use this order when facts conflict:

1. Current implementation and tests.
2. Current mutable docs such as `README.md`, `docs/ARCHITECTURE.md`, and package docs.
3. ADRs in `docs/adr/` for accepted intent and historical decisions.
4. Older plans, issue notes, or comments.

When implementation contradicts an ADR, say so explicitly instead of silently rewriting terms.

## Implementation Anchors

- `cmd/tabula/main.go` is the main CLI entrypoint; command wiring lives under `internal/cli/tabula/`.
- `cmd/tabula-runtime/main.go` starts the local runtime daemon.
- `internal/kernel/` owns hub state, sessions, WebSocket client protocol adapters, tool routing, runtime registry, session persistence, and liveness state. Kernel support packages include `internal/kernel/clientauth/`, `internal/kernel/clientmeta/`, `internal/kernel/hooks/`, `internal/kernel/process/`, and `internal/kernel/toolstate/`; CLI composition lives outside the hub. Runtime registry TOML loading lives in `internal/runtime/registryconfig/`.
- `internal/runtime/wire/` defines the kernel-to-runtime Runtime API frames.
- `internal/runtime/worker/wire/` defines the runtime-to-worker protocol used after the runtime host starts a worker.
- `internal/runtime/host/` owns runtime daemon config, manifest discovery, execution policy, warm worker pool, and Runtime API request handling.
- `internal/layout/` centralizes `TABULA_HOME` path conventions for Go code.
- `internal/tenant/` defines tenant metadata and filesystem layout.
- `tools/tabula-distro/` owns distro and tenant install/materialization.
- `tools/tabula-testbed/` owns installed-layout testbed orchestration and the Python test client.
- `docs/adr/` records architecture decisions. Add new ADRs rather than rewriting old ones.

## Glossary

### Agent Profile

A logical agent configuration shared conceptually across projects: distro plus distro-owned values. A profile is not a kernel object. One profile can serve many projects through separate project-scoped tenants.

### Bundle

A distributable package of surfaces such as plugins, skills, clients, Python packages, tests, or templates. Bundle metadata lives in `bundle.toml` and is interpreted by `tools/tabula-distro/`.

Use `bundle` for distribution units. Do not use it for the runtime worker process itself; that is a plugin or worker.

### Client

A WebSocket participant connected to the kernel. Clients can send and receive protocol messages according to policy. Examples include gateways, drivers, test clients, and local development tools.

Client protocol version is currently `ProtocolVersion = 3` in `internal/kernel/protocol.go`.

### Distro

A reusable agent product/package. A distro owns boot policy, prompt policy, templates, bundled surfaces, app materialization, and product defaults.

Kernel code must stay distro-agnostic. If a rule names a concrete product, workspace convention, or app policy, it probably belongs outside core kernel code.

### Generation

An atomically switchable installed distro tree under `$TABULA_HOME/distrib/<name>/generations/<id>/`, with `current` pointing at the active generation. The installer stages a full generation, validates it, then flips the symlink.

Use `generation` for installed distro snapshots, not for source bundles.

### Hub

The in-process kernel coordinator (`internal/kernel.Hub`). It owns client registry, session registry, process supervision state, policy engine, tool service, runtime registry, tenant store, session store, and tool dispatch table. It accepts parsed runtime registry configuration; CLI code loads TOML and tenant bindings before calling it.

Use `kernel hub` or `Hub` only for this coordinator, not for the whole CLI.

### Hook

A kernel event subscription and reply mechanism used for policy gates, approvals, and side-channel interaction. Hooks are not general tool calls. Hook subscribers have priorities and optional timeouts. Hook engine/model code lives in `internal/kernel/hooks/`; `internal/kernel` adapts clients/runtime targets and records session ledger audit events.

### Kernel

The Go runtime that owns sessions, routing, tool dispatch, Runtime API attachment, hooks, policy checks, and protocol messages. The kernel does not execute plugin code directly in the current local-runtime architecture; tool execution is delegated to attached runtimes.

Use `kernel` for the Go coordination process. Do not use it for distro installers, runtime workers, or bundles.

### Kernel Config

Installer-written kernel transport/config state under `TABULA_HOME`, primarily `$TABULA_HOME/config/kernel.toml` and related runtime config. Keep kernel config generic and transport-oriented.

Do not put plugin runtime config blocks in `plugin.toml`. Runtime config surfaces are `config/global.toml`, user-owned `config/plugins/<plugin-id>/config.toml`, and tenant overlays under `tenants/<tenant>/config/plugins/<plugin-id>/config.toml`.

### Runtime API

The JSON-frame protocol between kernel and runtime daemon, defined in `internal/runtime/wire/`. It covers runtime hello/authentication, capability listing, invocation, cancellation, and related runtime operations.

Use `Runtime API` for kernel-to-runtime communication. Use `worker protocol` for runtime-to-worker communication.

### Runtime Daemon

The process started by `cmd/tabula-runtime`. It reads runtime config, discovers plugin and skill manifests, attaches to the kernel, exposes runtime capabilities, and manages worker execution through a pool.

Current local startup uses an external/sibling runtime process, commonly launched by runner/orchestration, not a kernel-owned child lifecycle.

### Runtime Host

The implementation inside `internal/runtime/host/` that backs the daemon: config loader, manifest store, policy, daemon handler, dialer, harnesses, and worker pool.

Use `runtime host` for this implementation layer, not as synonym for the kernel.

### Runtime Registry

Kernel-side read model of attached runtimes (`internal/kernel.RuntimeRegistry`). It is transport-agnostic and tracks runtime capabilities/targets known to the kernel.

### Session

A tenant-scoped conversation/execution context tracked by the kernel. Sessions have lifecycle state such as `active`, `idle`, `closing`, and `suspended_stuck`. Session snapshots are persisted under tenant state and used for liveness/diagnostics.

Use `session` for kernel lifecycle contexts, not for process IDs or UI tabs unless backed by kernel session state.

### Stuck Session

A session marked `suspended_stuck` when liveness rules detect unsafe resume conditions after restart or repeated poisoned turns. Stuck sessions should stop automatic replay and surface diagnostics.

### Skill

A prompt/instruction artifact rooted at a directory with `SKILL.md` frontmatter. The runtime manifest layer normalizes skills as `skill:<name>` targets so names cannot collide with plugin targets.

Current implementation does not let skills publish executable tool capabilities. Runtime handler tests assert skill target invocations are rejected with `target_forbidden`. Use `skill` for instructions and agent behavior, not for executable tools.

### Plugin

An executable runtime target discovered from plugin manifests. Plugins publish tools and run as workers managed by the runtime host. A plugin belongs to a bundle at distribution time but is invoked as a runtime target at execution time.

Use `plugin` for executable tool providers. Do not call prompt-only skills plugins.

### Tool

An LLM-callable operation advertised by a runtime target and routed by the kernel dispatch table. Tool calls are tenant/session-aware, produce lifecycle events, and return rendered tool results.

Use `tool` for callable operations, not for CLI commands or helper scripts unless exposed through runtime capabilities.

### Tool Lifecycle

Persistent event stream for tool runs, approvals, cancellation, resumed state, and status transitions. It supports recovery and diagnostics for pending or interrupted tool work.

### Project

A user-facing workspace context. Workspace-backed agents use one project-scoped tenant per project; project identity and workspace policy stay outside kernel semantics.

### Tenant

A project-scoped runtime instance of an agent profile and the isolation namespace for config, sessions, runtime state, cache, logs, workers, filesystem policy, and installed component surfaces. Tenant IDs must match the grammar in `internal/tenant` and cannot use reserved IDs such as `admin`, `kernel`, `runtime`, or `system`.

A tenant is not a globally unique agent identity, authorization, billing, or quota object. Tenant component surfaces pin one exact distro generation and do not follow `distrib/active`.

### `TABULA_HOME`

The runtime/config/state root. It is not the user's workspace. Go path helpers live in `internal/layout/` and should be used instead of hardcoded paths.

Do not hardcode `~/.tabula` in user-facing text except when documenting the default value of `TABULA_HOME`.

### Testbed

The installed-layout integration test system under `tools/tabula-testbed/`. Testbeds install bundles into a temporary or target `TABULA_HOME`, start the relevant kernel/runtime/client surfaces, and assert behavior through installed tools and clients.

Use testbed coverage for installer fan-out, runtime layout, plugin execution, and end-to-end protocol bugs. Unit tests alone are not enough for installed-layout regressions.

### Worker

A runtime-managed process that speaks `internal/runtime/worker/wire/` after being spawned by the runtime host. Workers execute plugin tools under runtime policy and return invocation results.

Use `worker` for runtime child execution processes, not for kernel clients.

## Runtime Layout Terms

Use these path names precisely:

- `$TABULA_HOME/config/global.toml` for global runtime config.
- `$TABULA_HOME/config/kernel.toml` for kernel transport config.
- `$TABULA_HOME/config/runtime.toml` for runtime daemon config produced by install tooling.
- `$TABULA_HOME/config/plugins/<plugin-id>/config.toml` for user-owned plugin config.
- `$TABULA_HOME/tenants/<tenant>/config/plugins/<plugin-id>/config.toml` for tenant plugin config.
- `$TABULA_HOME/distrib/<name>/generations/<id>/` for installed generation content.
- `$TABULA_HOME/distrib/<name>/current` for active generation symlink.
- `$TABULA_HOME/run/reload.touch` for best-effort live reload notification after installer switch.

## Boundary Rules

- Keep kernel changes generic.
- Keep distro/product semantics in distro boot/materializer code.
- Keep reusable runtime helpers in shared bundle libraries only when multiple surfaces need them.
- Do not add legacy aliases or compatibility shims unless there is a concrete persisted-data or installed-layout migration requirement.
- Do not add new runtime `plugin.toml` config blocks.
- Keep `tabula-guide` current when runtime layout, config semantics, tool access, skills, plugins, tenants, installation, or troubleshooting behavior changes.

## Naming Guidance

Prefer these terms:

- `runtime daemon`, not `plugin runner`, when discussing `tabula-runtime`.
- `Runtime API`, not `plugin stdio`, for kernel-to-runtime communication.
- `worker protocol`, not `Runtime API`, for runtime-to-worker communication.
- `generation`, not `release`, for installed distro snapshots.
- `tenant`, not `user`, for filesystem/config isolation.
- `tenant`, not `app`, when talking about a concrete installed project binding.
- `skill`, not `plugin`, for prompt/instruction artifacts.
- `plugin`, not `skill`, for executable tool providers.
- `TABULA_HOME`, not `workspace`, for runtime state root.

## ADR Map

- ADR 0001: runtime daemon and execution backends. Accepted, partly superseded by ADR 0010 for local managed runtime ownership.
- ADR 0002: historical agent application manifests, superseded by ADR 0009.
- ADR 0003: kernel WebSocket protocol v3 envelopes and topics.
- ADR 0004: historical app binding targets, superseded by ADR 0009.
- ADR 0005: stuck session liveness.
- ADR 0010: managed local stack entrypoint; `tabula serve` owns local runtime lifecycle.

When changing architecture-level contracts, add a new ADR and update this context if vocabulary changes.
