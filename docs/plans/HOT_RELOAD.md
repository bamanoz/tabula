# Hot Reload Architecture Note

Status: future design note. Not implemented.

## Problem

Tabula currently builds runtime metadata and the static tool catalog when the
kernel starts. New sessions receive the current in-memory `init`, but joining a
new session does not rerun `boot.py` or rescan installed skills.

This means changes such as newly installed skills, external Agent Skills,
workspace prompt files, or updated distro metadata require a kernel restart
before drivers can see them. Even if the kernel sent fresh metadata, an existing
driver provider would still need to rebuild its system prompt and tool snapshot.

## Core Idea

Introduce a versioned `InitSnapshot` owned by the kernel.

`InitSnapshot` contains the runtime view sent to clients and drivers:

- `revision`
- `tools`
- `meta`
- `context_blocks`
- `agents`
- `slash_commands`
- runtime/generation info

Reload changes the kernel `InitSnapshot`. The kernel then broadcasts an
`init_update`. Drivers treat that update as provider-invalidating and rebuild
the provider before the next LLM turn.

Key rule:

```text
reload changes kernel InitSnapshot
init_update tells driver
driver rebuilds provider before next turn
```

## Non-Goals For The First Implementation

- Do not rescan disk on every `join`.
- Do not silently mutate provider system prompts in place.
- Do not reuse `session_start` hooks for reload context.
- Do not make shared drivers know distro-specific workspace or external-skill
  policy.

## Reload Pipeline

Reload should be transaction-like:

1. Acquire reload lock.
2. Build a new candidate snapshot without mutating the active one.
3. Rerun active `boot.py` for distro metadata.
4. Rescan installed skills and rebuild static skill tools.
5. Collect context blocks.
6. Refresh plugin dynamic tools where supported.
7. Validate the candidate snapshot.
8. Atomically swap the active `InitSnapshot`.
9. Broadcast `init_update`.
10. Return reload result with old/new revisions, changed domains, and warnings.

If boot or snapshot validation fails, keep the previous snapshot active.

## Reload Domains

Reload requests should support domains instead of a single vague mode:

- `boot`
- `skills`
- `plugins`
- `clients`
- `context`
- `tools`
- `all`

Default `/reload` should eventually cover boot metadata, installed skills,
context blocks, and reloadable dynamic tools. Plugin/client process lifecycle can
be added incrementally.

## Context Blocks

Replace one flat context string with ordered blocks:

```json
{
  "id": "boot:external-skills",
  "source": "boot",
  "kind": "external_skills",
  "priority": 50,
  "revision": 7,
  "content": "## External Agent Skills\n..."
}
```

Possible sources:

- `boot:workspace`
- `boot:external-skills`
- `plugin:sessions:cross-session`
- `plugin:caveman:mode`
- `plugin:memory:wakeup`
- `distro:templates`

Drivers sort/merge blocks into the final system prompt. Bundle-specific context
belongs in that bundle's plugin/context provider, not in shared driver code.

## Plugin Context And Reload

Do not rerun `session_start` just to refresh context; it may have side effects.
Add a side-effect-free context collection hook/protocol such as:

```text
init_context
```

or:

```json
{"type": "collect_context", "session": "main", "revision": 43}
```

Plugins may also declare reload strategy:

```toml
[reload]
strategy = "none" # none | protocol | restart | signal
```

Plugins that do not support reload remain running and produce warnings when a
requested domain cannot be fully refreshed without restart.

## Driver Behavior

Driver handles `init_update` by:

1. Updating tools, meta, workspace path, and context blocks.
2. Storing the new init revision.
3. Marking provider state dirty.
4. Rebuilding provider before the next model turn.
5. Replaying compacted history into the new provider instance.

Provider identity should include init revision:

```text
(agent_name, provider, model, init_revision)
```

Do not try to mutate the provider's system prompt in place.

## Protocol Sketch

Reload request:

```json
{
  "type": "reload_request",
  "id": "reload-123",
  "domains": ["boot", "skills", "context"],
  "reason": "user"
}
```

Reload result:

```json
{
  "type": "reload_result",
  "id": "reload-123",
  "ok": true,
  "old_revision": 43,
  "new_revision": 44,
  "changed": {"tools": true, "context": true, "meta": true},
  "warnings": []
}
```

Broadcast:

```json
{
  "type": "init_update",
  "revision": 44,
  "snapshot": {}
}
```

Initial `init` and `init_update` should carry the same snapshot shape.

## CLI UX

Add:

```text
/reload
/reload status
```

Useful output:

```text
Reloaded init snapshot.
Revision: 43 -> 44
Tools: 21 -> 22
External skills: 11 -> 12
Context blocks changed: boot:external-skills
Provider will rebuild before next turn.
```

Reload should be permission-gated. Local CLI can allow it; remote gateways should
require explicit admin capability.

## Generation Awareness

The snapshot should include runtime generation metadata:

- distro id
- active generation
- boot path
- loaded_at

Clients can warn when installed active generation is newer than the running
snapshot and suggest `/reload` or restart.

## Tests

Kernel tests:

- reload increments init revision;
- reload failure leaves the old snapshot active;
- adding a `SKILL.md` after startup appears after reload;
- removing/changing a skill updates the snapshot;
- joined clients receive `init_update`;
- existing dynamic plugin tools are preserved unless plugin reload changes them.

Driver tests:

- `init_update` marks provider dirty;
- provider key includes init revision;
- next turn rebuilds provider;
- updated tools and external-skill context reach the rebuilt prompt;
- history replay does not duplicate the current user message.

Testbed scenario:

- start kernel without an external skill;
- add the external skill under the distro workspace root;
- send reload request;
- assert `init_update.meta.external_skills` includes the new skill;
- verify a rebuilt driver can mention the skill.
