# Define modular capability architecture

**Type:** HITL  
**Status:** completed

## What to build

Define architecture and ownership boundaries for optional `continuity`, `activity`, `reflection`, `initiative`, and `evolution` capabilities. Establish which parts are new bundles and which missing generic primitives belong in existing Tabula components. Kernel must remain generic and unchanged unless a separately justified architecture decision proves a missing generic contract.

## Required discovery and design

- Inspect `/Users/mak/src/ouroboros` for identity/biography, activity, reflection, background consciousness, evolution, runtime modes, protected surfaces, and recovery.
- Cite concrete files and trace state, lifecycle, failure, safety, and recovery behavior.
- Map findings onto Tabula bundle/plugin/distro/installer boundaries and existing ADRs.
- Produce a proposed bundle graph, component contracts, optional integration rules, state ownership, and security boundaries.
- Review design with maintainer before implementation issues rely on it.

## Acceptance criteria

- [x] Design note cites relevant current source in both repositories.
- [x] Each capability can be installed without the other behavioral capabilities.
- [x] `mempalace` remains generalized memory and gains no identity policy.
- [x] Kernel remains dumb; distro policy and product semantics stay outside it.
- [x] Required generic primitive changes are distinguished from Ouroboros-specific behavior.
- [x] State paths, hooks, tools, SDKs, optional integrations, and uninstall behavior are specified.
- [x] Local-model tools, marketplace, and browser/media implementations are explicitly out of scope.
- [x] Architecture-level decisions are captured in a new ADR and approved by maintainer.
- [x] Design specifies canonical testbed coverage for every capability, including installed execution, failure/restart/recovery scenarios, and generated-template synchronization.

## Blocked by

None - can start immediately.
