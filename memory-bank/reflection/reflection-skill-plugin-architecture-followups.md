# Strategic Reflection: Tabula Skill/Plugin Architecture Follow-ups
Date: 2026-04-27

## System Summary
This Level 4 follow-up task attempted to close the post-reflection gaps from the original Tabula skill/plugin architecture migration. The intended scope grouped five concerns: stale legacy builtin metadata cleanup, locality/security hardening for `/internal/snapshot/plugins`, external `tabula-bundles` migrations, subagent plugin-side spawn/depth/MaxChildren coverage with D1.11(b) kernel bridge removal, and Phase 6 SDK/lib relocation.

The BUILD/SECURITY pipeline completed the repo-local safe hardening lane and deliberately preserved compatibility bridges whose deletion depends on external evidence. The result is a materially safer repository state, but the full grouped task is **not archive-ready** because several original success criteria remain blocked by `unknown/blocker` rows in the external migration matrix.

## 1. Overall Outcome
- **Implementation status**: Partial completion. BUILD and SECURITY phase gates are complete; SECURITY passed with no blockers. However, the active task itself is not complete because three Task Details items remain unchecked and externally gated.
- **REFLECT disposition**: This primary REFLECT can be marked `DONE` because the reflection work is complete and accurately records the partial outcome. This is **not** an archive-readiness claim. ARCHIVE must not proceed until blockers are resolved or the remaining work is formally split/descoped and reviewed.
- **Requirement coverage assessment**:
  - Legacy builtin metadata cleanup: **completed for live advertisement**. `cmd/tabula/kernel.tools.json` is now `[]`; `filterKernelTools` returns no metadata by default and warn-and-empty behavior for explicit stale names.
  - `/internal/snapshot/plugins` locality: **completed for current repo scope**. `internalDiagnosticsGuard` requires loopback `RemoteAddr` plus loopback/local `Host`; forwarded headers are not trusted; SECURITY accepted the boundary.
  - External `tabula-bundles` migrations: **not complete**. Matrix rows for hook plugins, MCP plugin, driver/subagent plugins, gateway plugins, per-call skills, Python SDK wheel, TypeScript SDK tarball, and bundle `_lib` paths remain `unknown/blocker`.
  - Subagent plugin-side spawn/depth/MaxChildren coverage and D1.11(b) deletion: **not complete**. Kernel `SpawnTokenStore`, `PolicyEngine.CanSpawn`, `Hub.MaxChildren`, `Hub.MaxSpawnDepth`, spawn-token env behavior, and skipped kernel spawn tests remain intentionally present.
  - Phase 6 SDK/lib relocation: **not complete**. `skills/_pylib`, `skills/_tslib`, distro `_pylib`/`_tslib` preserve behavior, and inline sibling `_*` support-dir compatibility remain because package artifacts are unavailable.
- **Quality attributes achieved**:
  - Reduced stale kernel builtin advertisement risk.
  - Improved diagnostics endpoint locality controls.
  - Added stronger boot/distro/plugin catalog validation and fail-closed handling for malformed plugin replies.
  - Preserved migration safety by refusing unsafe bridge deletion without external evidence.

## 2. Process Effectiveness
- **Metrics**:
  - Active date: 2026-04-27.
  - PLAN: 16 contribution passes established gates, external matrix requirements, validation scope, and SECURITY handoff.
  - CREATIVE: 1 focused supersession addendum selected replacement-auth, subagent-plugin-owned spawn policy and reserved/future generic `api.spawn`.
  - BUILD: 5 implementation passes completed repo-local hardening, followed by 4 decline passes confirming remaining deletion work was externally blocked.
  - SECURITY: 1 attempt; verdict `PASSED`; 0 blockers; 4 warnings; Python dependency audit degraded because `pip-audit` was unavailable.
- **Phase-by-phase analysis**:
  - **VAN**: Effective. Metadata is complete (`Level=4`, `Intent=implement`, `Category=deep`) and the workflow correctly included SECURITY and REFLECT.
  - **PLAN**: Strong. It separated safe repo-local work from external-evidence-gated deletion lanes and seeded `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`.
  - **CREATIVE**: Strong. `creative-plugin-runtime.md` §13 resolved the spawn/auth ownership contradiction and prevented accidental creation of a stable generic spawn API.
  - **BUILD**: Effective within the safe lane. It stopped rather than deleting bridge code when matrix rows remained `unknown/blocker`, which preserved system invariants.
  - **SECURITY**: Effective. It validated the locality guard, fail-closed plugin validation, and explicit preservation of external blockers.

## 3. Architectural Planning Review
- The architectural principle of a minimal kernel was reinforced: stale builtin metadata is no longer advertised, and dynamic skill/plugin tool registration remains the live source of tools.
- The selected replacement-auth model keeps spawn policy with the future external subagent plugin rather than reintroducing kernel-owned child-spawn semantics.
- The external migration matrix was the right planning artifact. It made evidence requirements concrete and prevented unsafe deletion of compatibility bridges.
- The main planning weakness was task granularity: repo-local hardening and external repository migrations were grouped into a single active task even though only the repo-local lane was executable in this workspace.

## 4. Creative Phase Review
- Required design clarification was executed in `creative-plugin-runtime.md` §13 and `creative-sdk-and-distro.md` supersession text.
- The selected model is coherent: generic SDKs do not expose stable `api.spawn`; `TABULA_SPAWN_TOKEN` is not a common runtime/public SDK contract; `before_spawn`/`after_spawn` are reserved/subagent-owned unless external evidence makes them concrete.
- Design-to-implementation fidelity was high for docs/SDK surface cleanup and validation gates, but intentionally deferred for D1.11(b) deletion and Phase 6 physical relocation because the required external evidence was absent.

## 5. Implementation Review
- **Completed implementation lanes**:
  - `cmd/tabula/kernel.tools.json` live metadata is empty.
  - `filterKernelTools` parses embedded metadata for packaging sanity but never re-advertises stale builtins.
  - `/internal/snapshot/plugins` is guarded by locality checks and documented as internal diagnostics metadata.
  - Boot skill descriptors and distro `SKILL.md` advertised `tools[]` entries validate non-empty names/execs before advertisement/promotion.
  - Plugin `register` and `update_tools` catalogs validate names, duplicates, deadlines, and subscriptions before unsafe mutation.
  - Malformed/unknown `event_reply` and ambiguous/empty `tool_result` payloads no longer synthesize permissive outcomes.
  - Active docs and temporary SDK surfaces no longer claim stable generic `api.spawn`, common `TABULA_SPAWN_TOKEN`, or non-empty `DEFAULT_KERNEL_TOOLS`.
- **Incomplete implementation lanes**:
  - No external `tabula-bundles` migration evidence was supplied.
  - D1.11(b) spawn bridge code/tests remain by design.
  - Phase 6 SDK/lib support directories and distro/script compatibility remain by design.
- **Integration challenges**: The repo-local changes were feasible, but deletion lanes depend on external artifacts (subagent plugin tests, SDK wheel/tarball, bundle `_lib` install evidence) that are not present in this repository.

## 6. Testing Review
- **Test coverage adequacy for completed safe lane**: Adequate.
  - BUILD logs report `go test ./cmd/tabula ./internal/kernel`, `go test ./cmd/tabula`, `go test ./internal/kernel/plugin ./internal/kernel`, `python3 -m unittest skills/_pylib/test_protocol.py`, `bun test` in `skills/_tslib`, and distro unittest execution with `PYTHONPATH="tools/tabula-distro/src" python3 -m unittest tools/tabula-distro/tests/test_install.py`.
  - SECURITY reviewed artifacts under `memory-bank/security/artifacts/skill-plugin-architecture-followups/attempt-1/`.
- **Issues found at each stage**:
  - BUILD found that external matrix rows remained unknown and correctly declined deletion work.
  - SECURITY found no blockers but reported warnings for existing `.opencode` npm audit findings, unavailable `pip-audit`, trusted-boundary logging surfaces, and external evidence blockers.
- **Testing process improvements needed**:
  - External rows need CI/job links or PR evidence before they can turn green.
  - D1.11(b) skipped kernel tests need a row-by-row replacement map to plugin-side tests when the external subagent plugin lands.
  - Phase 6 needs packaged SDK contract-test commands before in-repo support directories can be removed.

### Security Review Integration
- SECURITY report: `memory-bank/security/security-skill-plugin-architecture-followups.md`.
- Verdict: `PASSED` on attempt 1; blocking findings: 0; warning findings: 4.
- Key accepted controls:
  - `/internal/snapshot/plugins` is local-only by loopback caller and Host checks.
  - Plugin registration/update validation avoids installing unsafe registry/dispatch/hook state.
  - Malformed plugin replies are rejected or released rather than mapped to permissive security-hook behavior.
- Key warnings to carry forward:
  - Existing `.opencode` moderate npm audit findings unrelated to BUILD dependency changes.
  - `pip-audit` unavailable.
  - Plugin/boot logging remains an unredacted trusted boundary.
  - External matrix blockers remain and must not be hidden during archive decisions.

## 7. Successes with Evidence
1. **Legacy builtin metadata no longer re-enters the live advertised catalog** — Evidence: `cmd/tabula/kernel.tools.json` contains `[]`; `cmd/tabula/main.go::filterKernelTools` returns empty defaults and warns for explicit stale names.
2. **Plugin diagnostics exposure was reduced from assumption-based to guard-enforced locality** — Evidence: `cmd/tabula/main.go::internalDiagnosticsGuard` / `isLocalInternalDiagnosticsRequest`; SECURITY `auth-review.md` verdict `OK` for `/internal/snapshot/plugins`.
3. **Dynamic tool and plugin protocol boundaries became fail-closed before mutation** — Evidence: `internal/kernel/plugin/catalog_validation.go`, `internal/kernel/plugin_tools.go`, and SECURITY `findings.md` checklist items for input validation and untrusted-data boundaries.
4. **The team avoided unsafe deletion under uncertainty** — Evidence: external matrix rows remain `unknown/blocker`; BUILD Agents 6–9 declined further deletion and SECURITY warning 4 accepted preservation of bridges rather than premature cleanup.

## 8. Challenges with Solutions
1. **Challenge: grouped task mixed executable repo-local work with external migrations** → **Solution applied**: PLAN/BUILD split work into safe hardening, external evidence, D1.11(b), and Phase 6 lanes → **Outcome**: safe work landed, but archive remains blocked until external rows are green or descoped.
2. **Challenge: D1.11(b) bridge deletion would remove spawn/depth/MaxChildren invariants without replacement coverage** → **Solution applied**: CREATIVE selected subagent-plugin-owned replacement auth and BUILD preserved kernel bridge code/tests → **Outcome**: invariant risk is contained but still unresolved.
3. **Challenge: Phase 6 physical SDK/lib deletion lacks package artifacts** → **Solution applied**: docs/temporary SDK public surfaces were aligned while physical support directories and distro compatibility were retained → **Outcome**: false public contracts are reduced, but relocation is not complete.
4. **Challenge: SECURITY dependency checks were partially degraded** → **Solution applied**: SECURITY documented no dependency manifest changes and recorded `.opencode` / `pip-audit` caveats → **Outcome**: not blocking for this task, but release CI auditing remains a follow-up.

## 9. Strategic Technical Insights
- Evidence-gated architecture migrations should make external rows first-class acceptance criteria, not informal caveats.
- A local-only guard for sensitive diagnostics is preferable to relying on operator documentation; it converts an operational assumption into testable code.
- Public SDK surfaces must be cleaned before bridge deletion, not after, because stale helpers (`api.spawn`, spawn-token accessors, non-empty default builtin lists) can freeze accidental APIs.
- Intentional retention of dead-code migration bridges is acceptable only when the deletion gate, replacement evidence, and skipped tests are all explicit.

## 10. Process Improvement Insights
- Split future L4 follow-up tasks by executable ownership: repo-local hardening, external bundle migration, D1.11(b) deletion, and Phase 6 SDK relocation should be separate active tasks or separately archived milestones.
- Require concrete external artifact availability before starting a BUILD phase whose acceptance criteria include deleting compatibility bridges.
- Add an explicit Memory Bank “archive readiness: blocked/ready” field for cases where REFLECT is complete but the active task is not archive-ready.
- Ensure dependency-audit tools are installed before SECURITY starts to avoid degraded checks.

## 11. Business Impact
- **Value delivered**: The repository is safer and clearer for plugin architecture adoption: stale builtin tool advertisement was removed, diagnostics exposure was reduced, dynamic catalog validation hardened plugin boundaries, and active docs/SDK surfaces avoid false spawn-token/default-tool contracts.
- **Business metrics affected**:
  - Security posture: improved locality and fail-closed validation.
  - Developer experience: clearer docs around dynamic tools, plugin-owned subagents, and reserved spawn surfaces.
  - Migration safety: compatibility bridges remain until external replacement evidence exists.
  - Delivery completeness: full external plugin migration and SDK relocation value is not yet realized.

## 12. Strategic Action Items
- **Priority 1**: Obtain or create external `tabula-bundles` PR/artifact evidence for all matrix rows, starting with driver/subagent plugin replacement-auth coverage.
- **Priority 2**: Reopen BUILD only after green or owner-approved not-applicable matrix rows allow D1.11(b) bridge deletion and Phase 6 physical SDK/lib removal.
- **Priority 3**: Decide whether to split unresolved external deliverables into new active tasks/backlog items before attempting archive.

## 13. Follow-up Tasks
<!-- FOLLOW_UP_START -->
- [ ] Fill the external migration matrix with concrete `tabula-bundles` source paths, target paths, validation commands, and linked PRs for hook, MCP, driver/subagent, gateway, and per-call skill rows | Priority: high | Source: skill-plugin-architecture-followups
- [ ] After the driver/subagent row is green, relocate spawn/depth/MaxChildren/auth/lifecycle coverage to plugin-side tests and remove D1.11(b) kernel bridge code plus skipped kernel spawn tests | Priority: high | Source: skill-plugin-architecture-followups
- [ ] After Python wheel, TypeScript tarball, and bundle `_lib` rows are green, remove `skills/_pylib`, `skills/_tslib`, distro preserve behavior, inline sibling `_*` compatibility, and script/test compatibility references | Priority: high | Source: skill-plugin-architecture-followups
- [ ] Add release CI dependency auditing for Go, Python SDK artifacts, and JS/TS bundle lockfiles; include a fallback plan when `pip-audit` is unavailable | Priority: medium | Source: skill-plugin-architecture-followups
- [ ] Define plugin/boot stderr and plugin `log` redaction guidance or runtime redaction controls for sensitive operator environments | Priority: medium | Source: skill-plugin-architecture-followups
- [ ] Formally split or descope unresolved external deliverables before archive if external evidence will not be available in this task cycle | Priority: high | Source: skill-plugin-architecture-followups
<!-- FOLLOW_UP_END -->

## Archive Readiness Notes
- **Archive readiness**: **NO / BLOCKED**.
- **Reason**: BUILD and SECURITY phase gates are complete, but the active task's external migration, D1.11(b) bridge deletion, and Phase 6 SDK/lib relocation requirements remain incomplete and explicitly blocked by `unknown/blocker` external matrix rows.
- **Second-opinion readiness**: The primary reflection is self-contained for an L4 second-opinion audit. The second-opinion subagent should audit the conclusion that REFLECT is complete but ARCHIVE is blocked.
- **Lessons**: Structured lesson append to `memory-bank/systemPatterns.md` is deferred until the L4 second-opinion step approves this primary reflection, per CR-B.
