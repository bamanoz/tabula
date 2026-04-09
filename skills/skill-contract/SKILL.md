---
name: skill-contract
description: "Skill format spec. Use `EXEC cat skills/skill-contract/SKILL.md` to read. To discover skills: `EXEC ls skills/`"
---
# Tabula Skill Format

This document defines how skills work in Tabula. Read it to understand how to
discover, use, and create skills.

## What is a skill?

A skill is a directory under `./skills/` containing at least:

- `SKILL.md` — this file format. Describes what the skill does, how to run it,
  and what arguments it accepts.
- An executable entry point (e.g., `run.py`, `run.sh`, `run` binary).

## SKILL.md format

Every skill directory must have a `SKILL.md` with this structure:

```
# Skill Name

One-line description of what this skill does.

## Usage

How to run this skill via SPAWN:

    SPAWN <command>

## Arguments

How to pass arguments (via command line args, stdin, or env vars).

## Output

What the skill writes to stdout (auto-piped back to you).

## Examples

Concrete usage examples.
```

## Discovering skills

To see available skills, run:

    EXEC ls skills/

To read a skill's documentation:

    EXEC cat skills/<name>/SKILL.md

## Running a skill

Use the SPAWN tool with the skill's entry point:

    SPAWN python3 skills/<name>/run.py [args]
    SPAWN sh skills/<name>/run.sh [args]

The skill runs as a separate process. Its stdout is automatically piped back to
you. Use SEND to write to its stdin. Use KILL to stop it.

## Long-running vs one-shot skills

- **One-shot**: runs, produces output, exits. Example: a skill that reads a file.
- **Long-running**: stays alive, exchanges messages over stdin/stdout. Example:
  a gateway that relays user messages.

## Creating new skills

To create a new skill:

1. Create the directory: `EXEC sh -c 'mkdir -p skills/<name>'`
2. Write the entry point: `EXEC sh -c 'cat > skills/<name>/run.py << "SCRIPT"\n...\nSCRIPT'`
3. Write SKILL.md: `EXEC sh -c 'cat > skills/<name>/SKILL.md << "DOC"\n...\nDOC'`

You can create skills at any time to extend your own capabilities.
