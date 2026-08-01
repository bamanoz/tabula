# Deliver continuity capability end to end

**Type:** AFK  
**Status:** completed

## What to build

Create independent `continuity` bundle providing curated agent identity, biography, revision history, prompt injection, and controlled restoration. It must work without MemPalace and must not modify generalized memory behavior.

## Required discovery and design

- Inspect Ouroboros `BIBLE.md`, identity files/state, biography/continuity prompts, startup injection, self-revision, and constitutional-change safeguards.
- Separate observed identity/biography behavior from Ouroboros constitution and generalized memory.
- Map it onto tenant-scoped Tabula plugin state, prompt hooks, approvals, skills, and distro-owned immutable rules.
- Write bundle design before implementation; constitution remains distro policy, not mutable continuity state.

## Acceptance criteria

- [x] Bundle installs independently and exposes current profile, history, update, milestone, and restore operations.
- [x] Current identity is injected compactly at session/prompt lifecycle boundaries.
- [x] Identity revisions are auditable and biography events are append-only.
- [x] Config can require approval for designated core profile fields.
- [x] Distro constitution/rules cannot be mutated through continuity tools.
- [x] No dependency on MemPalace, activity, reflection, initiative, or evolution exists.
- [x] Installed testbed creates identity, starts a fresh session, observes continuity, revises it, and restores a prior revision.
- [x] Bundle docs explain state ownership, limits, and optional integrations.

- [x] Canonical testbed and generated testbed template are updated; the suite executes installed continuity tools and verifies identity survives a fresh session/restart.

## Blocked by

- [01 Define modular capability architecture](01-modular-capability-architecture.md)
