# Ouroboros-inspired modular capabilities

This backlog ports selected Ouroboros behavior into optional Tabula bundles and generic supporting primitives. It does not turn the kernel into an organism and does not copy Ouroboros architecture wholesale.

## Required discovery and design workflow

Every issue must begin with these steps before implementation:

1. Inspect the corresponding mechanism in the cloned Ouroboros repository at `/Users/mak/src/ouroboros`.
2. Record concrete source references and trace the mechanism end to end: state, lifecycle, failure handling, safety boundaries, recovery, and user-visible behavior.
3. Compare those findings with current Tabula contracts and ownership boundaries in `CONTEXT.md`, `docs/ARCHITECTURE.md`, `docs/adr/`, the relevant core code, and `/Users/mak/src/tabula-bundles` or `/Users/mak/src/tabula-distrib`.
4. Write a short design note before implementation. Preserve behavior worth carrying over, but redesign it around Tabula bundles, plugins, stable installed distro trees, hooks, SDKs, and dumb-kernel boundaries.
5. Identify any architecture-level decision. Add an ADR instead of silently changing kernel, installer, runtime-layout, protocol, or bundle contracts.

Do not assume prior conversation analysis is sufficient. Verify current source in both repositories. Do not add local-model invocation tools, a marketplace, or browser/media components. ClawHub remains external through `skill-issue`; browser automation remains Playwright MCP.

## Capability boundaries

- `continuity`: identity and biography.
- `activity`: curated cross-session work history.
- `reflection`: post-work synthesis.
- `initiative`: autonomous background work.
- `evolution`: controlled self-modification and external release recovery. Scope modes are `light` (analysis/proposals, no source mutation), `advanced` (extension-layer mutation), and `pro` (core/installer mutation). Activation authority is separately `manual` or `automatic`. Recovery executable is `evolution-supervisor`; existing `tabula.guardian` test distro is unrelated.
- `mempalace`: remains generalized memory.
- Tabula kernel: remains a generic router, message bus, lifecycle coordinator, and persistence boundary.

## Issues

| ID | Title | Type | Blocked by |
| --- | --- | --- | --- |
| 01 | Define modular capability architecture | HITL | None |
| 02 | Resolve transitive bundle component dependencies | AFK | 01 |
| 03 | Expose reusable subagent orchestration SDK | AFK | None |
| 04 | Expose durable task and scheduling primitives | AFK | None |
| 05 | Expose durable artifact storage API | AFK | None |
| 06 | Add structured workspace VCS component | AFK | None |
| 08 | Deliver continuity capability end to end | AFK | 01 |
| 09 | Deliver activity capability end to end | AFK | 01, 05 |
| 10 | Deliver reflection capability end to end | AFK | 03, 05 |
| 11 | Deliver initiative capability end to end | AFK | 03, 04 |
| 12 | Deliver reviewed change transactions | AFK | 03, 05, 06 |
| 13 | Deliver generic host-service bundle components | AFK | 02 |
| 14 | Deliver evolution campaigns and external recovery | AFK | 02, 12, 13 |

## Dependency graph

```text
01 -> 02 -> 13 -> 14
01 -> 08
01 -> 09
02 -> 14
03 -> 10
03 -> 11
03 -> 12 -> 14
04 -> 11
05 -> 09
05 -> 10
05 -> 12
06 -> 12
```

Each completed issue must be independently verifiable. Every implementation issue must add or update the corresponding canonical testbed suite and keep the generated testbed template in sync. Testbed checks must install and execute the affected plugin, tool, bundle, or lifecycle path; catalog-only assertions and unit tests alone are insufficient. Where the capability has failure, restart, or recovery behavior, testbed coverage must exercise that path too. Supporting primitive issues must demonstrate their generic behavior through an installed plugin or distro path, not only unit-test an internal helper.
