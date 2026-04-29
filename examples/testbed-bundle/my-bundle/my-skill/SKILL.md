---
name: my-skill
description: Example external bundle skill for testbed docs.
tools:
  - name: my_echo
    description: "Echo text for example tests."
    params:
      text: { type: string, description: "Text to echo." }
    required: []
    exec: "python3 skills/my-skill/run.py tool my_echo"
---

# My Skill

Example skill used by `docs/TESTBED.md`.
