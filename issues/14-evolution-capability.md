# Deliver evolution campaigns and external recovery

**Type:** AFK  
**Status:** completed

## What to build

Complete the independent `evolution` bundle with campaign control, planner/builder/verifier/reviewer profiles, source registry, immutable release-candidate assembly, and an external `evolution-supervisor` delivered through the generic host-service component. Adding this bundle to a compatible distro must enable controlled self-evolution without requiring continuity, activity, reflection, initiative, or MemPalace.

Evolution changes declared source repositories, never installed runtime trees. It produces sealed artifacts and activation requests. `evolution-supervisor` is the external activation and recovery authority: it quiesces the current runtime, activates candidates, starts the resulting runtime, observes health from outside candidate code, commits healthy releases, and restores retained known-good releases after failure.

Existing `tabula.guardian` test distro is unrelated and must not be used as this recovery component.

## Required discovery and design

- Deeply inspect Ouroboros `supervisor/evolution_lifecycle.py`, queue/state breakers, runtime mode policy, startup checks, reviewed commit flow, Git rescue, managed update/rollback/smoke paths, events, restart control, launcher lifeboat, and stable fallback.
- Trace normal campaign, failed build, stale review, failed restart, boot loop, interrupted update, explicit stop, rollback, and launcher/package update boundaries from source.
- Compare with Tabula reviewed change transactions, staged distro replacement, plugin reload behavior, service lifecycle, bundle dependency resolution, security hooks, subagents, Go build/release process, and installed runtime layout.
- Write an ADR defining source versus installed ownership, release artifact format, activation authority, supervisor protocol, runtime release layout, health receipts, known-good retention, interrupted-transaction recovery, and supervisor self-update boundary.
- Supersede stale ADR 0012 generation/installer activation wording where it conflicts with ADR 0020 and this design. Installer must remain limited to crash-safe staged distro replacement and must not gain public candidate/history/known-good/rollback policy APIs.

## Implementation plan

### 1. Establish contracts and durable state

1. Define a source registry for approved `tabula`, `tabula-bundles`, `tabula-distrib`, and optional external repositories. Development may bind existing local checkouts; managed production sources use configured mirrors/worktrees outside `TABULA_HOME`.
2. Define campaign and activation state machines. Persist objective, actor, scope mode, activation authority ceiling, budget, base release, source transactions, candidate, evidence, activation request, supervisor receipt, and outcome.
3. Define target descriptors for identity/state, skill/prompt, plugin, distro composition, Python runtime package, Go runtime, Go kernel, installer, and evolution implementation. Unknown ownership fails closed.
4. Define a sealed release manifest containing candidate ID, parent release, exact source revisions, bundle/distro locks, protocol compatibility, payload hashes, verification/review evidence digests, restart class, and required external health probes.

### 2. Build the evolution controller

5. Implement `light | advanced | pro` scope policy independently from `manual | automatic` activation authority.
6. Implement planner, builder, verifier, and reviewer profiles with least-privilege tools. Source mutation must go through issue 12 reviewed change transactions.
7. Implement campaign budgets, cooldown, repeated-failure/no-op breakers, owner pause/stop, foreground-work exclusion, and restart recovery.
8. Expose tools to start, inspect, pause, stop, list candidates, inspect evidence, approve/reject, request activation, and inspect activation outcome. Controller cannot switch releases or declare health.

### 3. Assemble immutable candidates

9. Build extension-only candidates from reviewed bundle/distro commits first. Resolve exact transitive dependencies, assemble installed payload in isolation, run canonical installed-layout testbeds, and seal manifest/evidence without touching active installation.
10. Add core candidates that build `tabula` and `tabula-runtime` from exact reviewed Go revisions, assemble Python/runtime dependencies, and verify protocol and distro compatibility.
11. Store candidate artifacts outside source worktrees. Any payload or manifest change creates a new candidate ID and invalidates prior reviews and verification receipts.

### 4. Deliver the external supervisor

12. Package `evolution-supervisor` in the `evolution` bundle as a generic host-service component from issue 13. Its executable runs outside kernel, plugin runtime, tenant workers, and active distro.
13. Implement a narrow local protocol over a protected local socket plus durable request/journal/receipt files. Requests bind candidate ID and expected active release to prevent stale activation.
14. Keep supervisor logic mechanical: validate artifact/evidence policy, serialize activation, retain known-good material, quiesce current runtime, invoke target adapter, start runtime, observe host-side probes, commit, or roll back. Campaign strategy remains in the plugin.

### 5. Activate extension and distro candidates

15. For reload-safe plugin/config changes, use staged distro replacement and `reload.touch`, then probe expected installed capability through the live runtime.
16. For worker-restart changes, drain affected work, restart the runtime worker/process through the external service boundary, and verify expected candidate/revision.
17. Retain the previous full release descriptor and distro artifact in supervisor-owned storage. Rollback reapplies the retained artifact through the ordinary crash-safe installer; installer itself owns no history policy.

### 6. Activate Go runtime and kernel candidates

18. Introduce supervisor-owned runtime release directories containing compatible `tabula`, `tabula-runtime`, Python/runtime payload, and release manifest. Do not overwrite the running binaries in place.
19. Make the managed agent service launch binaries through the supervisor-selected active runtime release. Activation installs a candidate beside known-good, checkpoints durable work, drains/stops old processes, switches the active release reference, and starts the candidate.
20. Verify process liveness, kernel health, runtime attachment, tenant readiness, expected release ID, protocol compatibility, required capability presence, installed smoke-tool execution, and crash-free observation window.
21. On timeout, crash, protocol mismatch, failed smoke, or boot-loop threshold, stop the candidate, restore previous runtime release and distro artifact, restart known-good, verify recovery, and emit a failure receipt.

### 7. Close the lifecycle safely

22. A campaign becomes `absorbed` only after supervisor commits a health receipt for the exact candidate. Reviewed, built, installed, or started states are not success.
23. Reconcile crashes after every durable activation phase. Corrupt or ambiguous journals fail closed and keep known-good available.
24. Prohibit a campaign from modifying and activating the supervisor that controls its rollback. Supervisor upgrades require a separate trusted update transaction, retained previous executable, external service handoff, and owner-approved policy.
25. Add end-to-end installed testbeds for successful extension evolution, light/advanced/pro policy boundaries, failed verification, stale activation request, kernel rebuild/restart, interrupted activation, boot loop, deterministic rollback, explicit owner stop, and supervisor restart recovery.

## Acceptance criteria

- [x] Bundle resolves all generic supporting components, including `change-control` and generic `host-service`, but no unrelated behavioral bundles.
- [x] Campaign state persists objective, budget, phase, source transactions, candidate, evidence, activation request, supervisor receipt, and outcome across plugin, worker, kernel, and supervisor restarts.
- [x] Approved source repositories and worktrees are explicit and outside `TABULA_HOME`; active installed distro, plugin, runtime, and binary trees are never mutated as authoring source.
- [x] Planner, builder, verifier, and reviewers run with enforceable least-privilege tool/worktree policies and issue 12 reviewed-commit gates.
- [x] Failure/no-op/rollback budgets, cooldown, owner stop, foreground-work exclusion, and global pause circuit breakers are enforced.
- [x] `light` permits analysis/proposals only; `advanced` permits declared extension-layer mutation; `pro` additionally permits core/runtime/installer/evolution mutation.
- [x] Activation authority is separate `manual | automatic`; campaign may lower but never raise configured mode or authority.
- [x] Candidate is an immutable sealed artifact bound to exact source revisions, dependencies, payload hashes, reviews, tests, compatibility, parent release, restart class, and health profile.
- [x] Candidate changes invalidate prior evidence and produce a new candidate ID.
- [x] Extension-only candidate can activate through staged distro replacement/reload without a Go kernel rebuild when compatibility permits.
- [x] Core candidate builds new Go `tabula` and `tabula-runtime` binaries and activates them from a separate runtime release directory rather than overwriting running binaries.
- [x] `evolution-supervisor` is delivered as a host service, runs from retained known-good code, serializes activation, validates expected active release, and never depends on candidate-loaded plugins.
- [x] Supervisor observes health externally and candidate code cannot mark itself healthy or delete known-good artifacts.
- [x] Interrupted activation and supervisor restart reconcile deterministically from durable journal state.
- [x] Timeout, crash, smoke failure, protocol mismatch, and boot loop restore and verify the retained full known-good release.
- [x] Campaign reaches `absorbed` only after exact-candidate health receipt is committed.
- [x] Supervisor cannot be modified and activated in the same campaign whose rollback it controls; supervisor self-update is documented as a separate trusted lifecycle.
- [x] Security limits of same-user execution are documented; production-hardening path supports isolated workers and separately protected supervisor identity.
- [x] No kernel product semantics, local-model tools, marketplace, browser/media component, or installer candidate/history/rollback API is added.
- [x] Installed testbed executes successful extension evolution and Go kernel evolution, then forces broken and interrupted candidates and proves external rollback to retained known-good across restart.
- [x] Canonical testbed, generated testbed template, ADRs, runtime/install docs, and installed `tabula-guide` remain synchronized.

## Review status

Completed end-to-end reviewed-source to sealed-candidate activation for protected directory, distro, and runtime targets. Flow includes attested transaction evidence, trusted-context campaign/change actors and manual approval, strict transaction worktree binding, durable budgets, supervisor-owned staging/rehash, external health checks, retained known-good bytes, deterministic journal recovery, and boot-loop protection. Distro activation uses ordinary crash-safe installer/reload and rollback from retained source. Core candidates build real Go `tabula` and `tabula-runtime` binaries, activate from separate immutable runtime release directories, reject exact protocol mismatch before switching, and restore known-good after broken or interrupted activation. Canonical/generated testbeds are synchronized; focused unit, lint, and isolated installed evolution suites pass.

## Blocked by

- [02 Resolve transitive bundle component dependencies](02-transitive-bundle-dependencies.md)
- [12 Deliver reviewed change transactions](12-reviewed-change-transactions.md)
- [13 Deliver generic host-service bundle components](13-host-service-components.md)
