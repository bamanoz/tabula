# Kernel Tool Naming Notes

Current built-in tool names:

- `EXEC`
- `SPAWN`
- `KILL`
- `LIST`

## Problem

They are short, but not very self-descriptive outside the Tabula codebase.

- `EXEC` can mean shell command, arbitrary executable, or even code execution.
- `SPAWN` is clearer, but still generic.
- `KILL` sounds harsh and Unix-specific.
- `LIST` is the least specific; it only makes sense if you already know it lists spawned processes.

## Constraints

- These names are part of the LLM-facing tool surface.
- They are also part of the kernel wire/runtime contract.
- Renaming them is not just a docs change; it affects:
  - embedded kernel tool metadata
  - tests
  - prompts / skill docs
  - any third-party runtime or prompt assumptions

## Good rename goals

- more descriptive at first glance
- still short enough for prompts and tool calls
- easy to group mentally as process/runtime built-ins
- avoids colliding with likely future skill tool names

## Option A: Verb-Noun names

- `RUN_COMMAND`
- `START_PROCESS`
- `STOP_PROCESS`
- `LIST_PROCESSES`

Pros:

- highly explicit
- easy for new users and models to infer

Cons:

- verbose in prompt/tool calls
- feels less like a compact system primitive set

## Option B: Compact but clearer process/shell names

- `SHELL`
- `SPAWN`
- `TERMINATE`
- `PROCESSES`

Pros:

- more readable than current names
- still compact

Cons:

- asymmetry between shell and process operations
- `PROCESSES` is still a noun, not an action

## Option C: Namespace-style names

- `PROC_EXEC`
- `PROC_SPAWN`
- `PROC_KILL`
- `PROC_LIST`

Pros:

- grouped clearly as kernel/process built-ins
- minimal semantic ambiguity

Cons:

- more mechanical / less natural
- still inherits `EXEC` and `KILL` ambiguity somewhat

## Option D: Keep wire names, improve descriptions

Keep:

- `EXEC`
- `SPAWN`
- `KILL`
- `LIST`

Improve only:

- metadata descriptions
- prompt wording
- docs wording

Pros:

- zero migration cost
- preserves existing ecosystem/tests/contracts

Cons:

- names stay somewhat opaque

## Recommendation

If we want minimal churn right now: choose **Option D**.

If we want a real rename later: **Option B** is the best balance between clarity
and prompt ergonomics.

Suggested future set:

- `SHELL`
- `SPAWN`
- `TERMINATE`
- `PROCESSES`

That said, a rename should be treated as a deliberate wire-contract migration,
not a casual cleanup.
