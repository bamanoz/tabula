# Background Task Ledger For Subagents

Type: AFK

Priority: P1

Repos: `tabula-bundles`

## Parent

`docs/competitors/claude-code.md`

## What to build

Wrap async subagents in a first-class task model. A background subagent should
have a task id, owner session, status, prompt summary, result artifact or output
file, cancellation state, current activity, and completion notification.

This should build on the existing subagents plugin rather than replacing it.

## Acceptance criteria

- [ ] Spawning an async subagent creates a task ledger entry linked to the owner
      session.
- [ ] `list`, `wait`, and `kill` style operations read and update task state
      through the task model.
- [ ] Completion writes a durable result reference and emits a notification to
      the parent session.
- [ ] A resumed parent session can inspect completed and running task records.
- [ ] Tests cover spawn, progress update, completion, cancellation, and resume.

## Blocked by

- `001-session-ledger-harness-events.md`
