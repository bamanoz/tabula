# Deliver explicit turn assignment, preparation, and execution permits

**Type:** AFK  
**Status:** completed

## What to build

Replace implicit `message.user` delivery as the execution boundary with an explicit driver protocol:

```text
turn.assign -> turn.prepared -> turn.permit -> turn.output -> terminal
```

Kernel owns the state transitions and driver fencing. Driver owns provider/model execution and cannot start side-effecting work before `turn.permit`.

## Acceptance criteria

- [x] Assignment identifies session, turn, attempt, driver generation, and sequence/cursor context.
- [x] Driver can reject or fail preparation without making the attempt uncertain.
- [x] Kernel issues one execution permit per attempt and rejects duplicate/stale acknowledgements.
- [x] Driver receives the prepared input/context without gateway-specific metadata hacks.
- [x] Driver cannot legally emit output or terminal state for an unpermitted attempt.
- [x] Crash before permit creates a safely retryable attempt.
- [x] Crash after permit creates an uncertain/recovery-required outcome rather than an automatic duplicate execution.
- [x] Existing tool/provider calls are mapped without moving provider policy into kernel.
- [x] Focused protocol, state, race, and driver SDK tests pass.

## Blocked by

Issues 06 and 08.
