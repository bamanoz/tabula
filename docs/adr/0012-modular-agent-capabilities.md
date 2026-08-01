# ADR 0012 - Optional agent behavior belongs in independent bundles

Date: 2026-07-29
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Ouroboros combines persistent identity, work activity, post-task reflection,
background initiative, and self-evolution in one product runtime. Tabula is a
composable agent platform with a generic kernel, tenant-scoped plugins,
instruction-only skills, distro-owned product policy, and immutable installed
generations.

Copying Ouroboros as one subsystem would couple unrelated behavior, put product
semantics into generic components, and make identity depend on generalized
memory. It would also make recovery code part of the mutable candidate it must
recover.

Current Tabula hooks already expose generic turn lifecycle seams. Tenant-pinned
distro generations already provide isolation and immutable installed component
surfaces. Missing reusable mechanics such as structured VCS transactions or
candidate activation should be designed as generic components, not embedded in
behavioral policy or added to the kernel by default.

Detailed source study and proposed contracts are recorded in
`docs/plans/modular-agent-capabilities.md`.

## Decision

Tabula will model Ouroboros-inspired behavior as five independently installable
bundles:

- `continuity` owns persistent identity, biography, and reviewed identity
  candidates.
- `activity` owns a curated projection of cross-session work evidence; it does
  not own scheduling or cancellation.
- `reflection` owns post-work synthesis and produces explicit candidates for
  other systems instead of writing their private state.
- `initiative` owns bounded autonomous wake cycles, durable leases, breakers,
  and an explicit tool allowlist.
- `evolution` owns campaign policy, scope modes, candidate review requests,
  activation requests, health observation policy, and recovery reporting.

No behavioral bundle depends on another behavioral bundle. Optional integration
uses advertised tools or public SDKs and must degrade cleanly when absent.

MemPalace remains generalized memory. It owns no identity, biography,
personality, initiative, reflection, or evolution policy. A continuity bundle
may optionally publish records to MemPalace through a public ingestion surface.

Kernel remains unchanged by this decision. Capabilities use generic hooks,
runtime tools, tenant isolation, session ledgers, and installed-distro
contracts. Product defaults and installed capability sets belong to distro
configuration/materializers.

Reusable missing mechanics will be implemented separately and remain free of
Ouroboros-specific behavior:

- transitive bundle dependency resolution;
- subagent orchestration SDK;
- durable task and scheduling API;
- artifact storage API;
- structured workspace VCS component;
- supervisor-owned release artifact activation and recovery;
- reviewed change transactions.

Evolution configuration has two independent axes:

- scope mode: `light | advanced | pro`;
- activation authority: `manual | automatic`.

`light` permits analysis and proposals but no mutation. `advanced` permits
extension-layer mutation only. `pro` may also mutate kernel, protocol,
installer, shared SDKs, and evolution implementation. Campaigns may lower but
never raise configured activation authority.

Candidate work occurs in isolated worktrees. Activation targets immutable
candidate generations, passes review and installed-layout testbed gates, uses an
atomic installer-owned switch, enters a health-observation window, and rolls
back deterministically on failure.

Recovery is owned by a separately packaged `evolution-supervisor` that runs
outside candidate-mutated code. It reconciles interrupted activation, detects
boot loops or health timeout, and restores the previous generation. The
`tabula.guardian` distro is unrelated and is not recovery infrastructure.

Capability state is tenant-scoped and preserved by default when a bundle is
removed. Purge is explicit. Corrupt state must not be silently interpreted as
empty state.

Every capability and generic primitive implementation must add canonical
installed-layout testbed coverage and synchronize the generated testbed
template. Tests must execute installed components and cover relevant failure,
restart, and recovery behavior; catalog-only checks are insufficient.

Local-model tooling, skill marketplaces, browser/media implementations, product
UI, and Ouroboros-specific prompts/constitution/default identity are outside
this decision.

## Consequences

- Distros can install any subset of the five behaviors.
- Removing continuity does not remove generalized memory; removing MemPalace
  does not remove identity.
- Capability integrations require explicit schemas and authority checks rather
  than private file coupling.
- Initiative and evolution policy remain inspectable and replaceable outside the
  kernel.
- Evolution requires several generic prerequisite issues before full
  implementation.
- `evolution-supervisor` packaging and candidate-generation ownership become
  explicit architecture work rather than hidden plugin details.
- More bundles and contracts exist, but each has one state owner and can be
  tested, installed, upgraded, and removed independently.
