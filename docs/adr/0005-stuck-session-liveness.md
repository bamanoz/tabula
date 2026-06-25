# ADR 0005 — Stuck Session Liveness

Date: 2026-06-25
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Tabula persists session snapshots under the tenant state directory, but the
kernel currently treats them as observational state. A kernel/runtime process can
restart while a session has an active turn or pending tool calls. If the same
session is recreated and the same driver resumes automatically, a poisoned turn
can repeat across restarts without a clear diagnostic or stop condition.

Hermes has a stuck-loop detector that suspends sessions after repeated active
observations across gateway restarts. Tabula needs equivalent protection, but it
must remain generic kernel lifecycle behavior. It must not depend on distro
policy, provider type, prompt shape, tool names, gateway type, or driver internals.

## Decision

Add kernel-owned stuck-session liveness tracking based on persisted session
snapshots.

When a session is created, the kernel may inspect the previous persisted session
snapshot for the same tenant/session. If that snapshot says the session was
active in a turn or had active tool calls, the kernel records a restart
observation. After a small threshold, the session becomes `suspended_stuck` and
the kernel refuses to start automatic turns for that session until the user or a
client explicitly cancels/resets the session.

The first implementation uses the existing tenant session state path:

```text
$TABULA_HOME/tenants/<tenant>/state/sessions/<session>.json
```

The persisted snapshot records only generic lifecycle fields:

- whether a turn was in flight;
- active tool-call count;
- pending input count;
- restart observation count;
- stuck/suspended flag.

## Boundaries

Kernel owns:

- session liveness state;
- restart observation count;
- stuck suspension state;
- refusing automatic turn start for suspended sessions;
- clearing suspension on cancel/reset/completion.

Drivers own:

- provider retry/recovery;
- malformed tool-call repair;
- provider-history repair;
- partial stream recovery;
- turn-exit diagnostics.

Distro and bundles must not encode stuck-session policy.

## Consequences

Positive:

- Repeated restarts cannot silently resume the same poisoned session forever.
- The detector works across providers, gateways, and distros.
- Existing session snapshot storage is reused.

Negative:

- False positives are possible if a process repeatedly restarts during a
  legitimate long-running turn. The threshold must be greater than one and the
  suspended state must be recoverable.
- The session snapshot becomes more semantically important, so tests must cover
  missing/corrupt snapshots and atomic persistence behavior.

## Verification

Tests must cover:

- active persisted session snapshots increment restart observations when the
  session is recreated;
- inactive snapshots do not increment restart observations;
- sessions below threshold can still start turns;
- sessions at threshold are marked `suspended_stuck` and refuse automatic turns;
- cancel/reset clears stuck state;
- missing/corrupt snapshots do not brick a session.

Installed testbed coverage must verify the liveness state helpers through the
installed kernel/testbed layout.
