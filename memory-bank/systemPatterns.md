# System Patterns

Patterns and architectural decisions that apply across active workstreams.

## Plugin/Subagent Spawn Ownership Pattern (2026-04-27)

- Kernel owns dynamic tool registration, generic hook routing, plugin process supervision, local diagnostics, and protocol validation.
- Subagent plugin owns child-spawn API shape, child-auth credential/channel, depth derivation, MaxChildren accounting, lifecycle/list/kill/cancel semantics, and child cleanup.
- Generic SDK packages do not expose stable `api.spawn` or common `TABULA_SPAWN_TOKEN` helpers until a future design accepts the full process/auth/security test burden.
- Kernel spawn bridge deletion is evidence-gated by plugin-side tests or linked external `tabula-bundles` matrix rows, not by source deletion alone.
- Reserved spawn event names (`before_spawn`, `after_spawn`) are not kernel-emitted after bridge removal; active docs must state reserved/subagent-owned semantics or omit them from generic event lists.

## Evidence-Gated Compatibility Removal Pattern (2026-04-27)

- Repo-local hardening and external migration completion are separate states: tests/docs may prove the local kernel is safer without proving external `tabula-bundles` migrations are complete.
- `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md` is the canonical gate for deleting compatibility bridges tied to external plugins, subagent spawn ownership, and SDK/lib relocation.
- A matrix row changes state only from concrete external paths, validation commands or QA artifacts, and linked commits/PRs/release artifacts; target architecture prose and archived Memory Bank entries are not sufficient evidence.
- If evidence is absent, retain compatibility bridges with explicit comments/tests/docs rather than performing partial deletion.
- Local-only diagnostics are the default security posture for internal plugin snapshots; remote diagnostics require a separate authenticated design, not a proxy/header trust shortcut.

## Lessons Learned
<!-- LESSONS_START -->
### 2026-04-27 skill-plugin-architecture-followups L4 pattern
- **Context**: Level 4 plugin architecture migration combined repo-local hardening with external bundle and SDK deletion lanes.
- **Lesson**: Treat external artifact matrix rows as hard acceptance gates; complete safe local hardening while retaining compatibility bridges until linked paths, validation commands, and PR/release evidence prove replacement coverage.
- **Applies-to**: implement, L4, BUILD/REFLECT, plugin migrations, compatibility deletion
<!-- LESSONS_END -->
