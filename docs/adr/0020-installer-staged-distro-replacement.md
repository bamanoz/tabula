# ADR 0020: Installer Owns Staged Distro Replacement Only

- Status: Accepted
- Date: 2026-08-01
- Supersedes: ADR 0019
- Superseded by: none

## Context

ADR 0019 put candidate activation, health confirmation, known-good state, history,
and rollback into `tabula-distro`. That boundary duplicated policy required by
the planned evolution supervisor and made ordinary installation carry a release
orchestration lifecycle.

Installer still needs crash-safe mutation. A failed or interrupted install must
not leave a partially composed distro tree or tenant/runtime surfaces pointing
at inconsistent content.

## Decision

`tabula-distro` owns only transactional replacement of one installed distro tree.

Installed content has one stable location:

```text
$TABULA_HOME/distrib/<name>/
```

During installation, private transaction scratch lives under:

```text
$TABULA_HOME/run/install/<name>/staging/
$TABULA_HOME/run/install/<name>/previous/
$TABULA_HOME/run/install/<name>/transaction.json
```

The installer serializes mutation, recovers an interrupted transaction, composes
and validates `staging`, fingerprints it, moves the current installed tree to
`previous`, replaces the stable installed path, refreshes runtime and tenant
surfaces, and emits the normal reload trigger. Failure restores `previous` and
the prior runtime surfaces. Success removes transaction scratch.

Tenant install locks use version 2 and identify the stable installed path:

```json
{"version": 2, "distro": {}, "path": "distrib/<name>"}
```

Tenant component links target that stable tree. Installer exposes no generation
IDs, `current` pointer, history, pruning, promotion, candidate commands, health
confirmation, known-good state, or operator rollback command.

Candidate assembly, observation, activation authority, known-good retention,
rollback policy, and recovery across releases belong to an external evolution
supervisor. Kernel remains unaware of both installer transactions and evolution
policy.

## Consequences

- Ordinary installs have one visible distro path and one internal crash-recovery transaction.
- Installer rollback is failure handling inside the active install operation, not a public lifecycle.
- Tenant locks identify current installed content, not immutable historical content.
- Evolution must preserve and activate its own candidate artifacts rather than relying on installer generations.
- Runtime and permission policy can deny distro internals broadly while allowing installed skill reads through `distrib/**/skills/**`.
- Existing generation and candidate installer surfaces are removed without compatibility aliases.
