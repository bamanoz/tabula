# ADR 0019 - Tenant candidates use durable installer transactions

Date: 2026-07-30
Status: Accepted
Supersedes: nothing
Superseded by: ADR 0020

## Context

Tabula already installs immutable distro generations and atomically switches a
`current` reference. Tenant install locks and runtime links pin a tenant to one
generation. Normal install currently promotes immediately, retargets matching
tenants, refreshes runtime surfaces, and emits `run/reload.touch`.

Controlled evolution needs a generic lower-level primitive that can prepare and
validate a tenant candidate without changing live runtime, activate one tenant,
wait for health confirmation, and roll back after failure or process restart.
This must not put evolution policy or health semantics into kernel.

Ouroboros demonstrates useful failure patterns: write transaction state before
live mutation, preserve known-good identity, fail closed on corrupt markers,
re-check state under an exclusive lock, separate pre-activation smoke from
post-activation health, and deterministically recover interrupted operations.
Its Git-branch and supervisor policy are product-specific and do not transfer.

## Decision

`tabula-distro` owns a tenant-local candidate transaction state machine.

1. Installer can compose an immutable generation with promotion disabled.
2. Candidate tenant materialization occurs in a shadow tenant directory.
3. Optional distro-declared `tenant_contract.candidate_smoke` executes against
   candidate generation and shadow tenant surfaces.
4. Durable transaction state is atomically written before live tenant mutation.
5. Activation updates only selected tenant lock, links, values, generated
   config, runtime config, and reload trigger.
6. Confirmation finalizes candidate as last-known-good.
7. Explicit rollback, health timeout, or interrupted activation restores prior
   tenant snapshot and emits normal reload trigger.
8. Every transition is serialized by cross-process tenant lock and begins with
   fail-closed recovery.
9. Transaction history and current state are inspectable.

Kernel remains VCS-, candidate-, and health-policy-opaque. It only observes the
existing tenant runtime config and reload trigger.

## Transaction Phases

- `staged`
- `activating`
- `awaiting_health`
- `confirmed`
- `rolled_back`
- `failed`

Corrupt current transaction state blocks mutation. `activating` is treated as
interrupted and rolled back. Expired `awaiting_health` is rolled back.

## Consequences

Positive:

- Candidate validation cannot silently mutate active tenant runtime.
- Failed activation has explicit previous generation and tenant snapshot.
- Restart recovery is deterministic and testable.
- Evolution can consume generic installer lifecycle without kernel changes.
- Existing immediate install behavior remains available for ordinary installs.

Negative:

- Candidate work directories duplicate tenant-generated config temporarily.
- Activation touches multiple tenant-owned paths and therefore needs durable
  phase records plus idempotent restore logic rather than one filesystem rename.
- Health confirmation is caller-driven; issue 13 will add external supervision
  and activation authority policy.

## Rejected Alternatives

### Promote then undo on smoke failure

Rejected because normal install changes global and tenant pointers before smoke,
creating visible partial state and making failure recovery ambiguous.

### Put candidate lifecycle in kernel

Rejected because candidate policy, materialization, installer locks, and rollback
are not routing or message-bus responsibilities.

### Encode lifecycle only in evolution bundle

Rejected because staging and activation are generic immutable-generation
contracts needed by other controlled rollout workflows.

### Use source worktrees as candidate generations

Rejected because runtime must validate installed immutable components and pinned
locks, not source checkout shape.
