# Migrate the driver component and SDK to execution protocol v4

**Type:** AFK  
**Status:** completed

## What to build

Migrate the first-party driver bundle and SDK from capability-inferred WebSocket behavior to the authenticated driver execution protocol. The driver remains a replaceable bundle component and runtime worker; only its kernel interaction contract changes.

Implement registration, readiness, lease heartbeat, assignment preparation, execution permit, sequenced output, terminal outcomes, cancellation, checkpoint/recovery reporting, and stale-generation rejection.

## Acceptance criteria

- [x] Driver is declared and installed as a `driver` component without special provider knowledge in kernel.
- [x] Driver never starts provider execution before a valid permit.
- [x] Every permitted attempt produces a terminal outcome or explicit uncertain recovery state.
- [x] Driver reconnects through runtime supervision without gateway wake/rejoin logic.
- [x] Driver SDK validates generation, attempt, sequence, and correlation fields.
- [x] Provider, model, prompt, tools, and history behavior remain in driver/runtime layers.
- [x] Unit, protocol, restart, stale-event, and installed execution tests pass.

## Verification status

Driver SDK and plugin unit suites pass, including restart, permit, stale-fence,
late-response, cancellation, tool, and terminal-outcome coverage. The installed
crash-recovery suite executes the real driver across runtime and kernel restarts
and passed all 9 scenarios in two consecutive isolated runs. The full core race
suite passed 731 tests across 39 packages.

## Blocked by

Issues 07, 08, 09, 11, and 12.
