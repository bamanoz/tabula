---
name: gateway-test
description: "Test gateway for automated testing"
---
# gateway-test

Test gateway for automated testing. Sends a single hardcoded message after 2
seconds, prints any responses to stderr, then waits 30 seconds and exits.

This is a test/dev runtime skill. In the repository it lives under
`testing/skills/`, but installed/runtime examples may expose it through a flat
`skills/` command surface in test homes.

## Usage

    SPAWN python3 skills/gateway-test/run.py

## Output

Sends "Привет, расскажи о себе" followed by an empty line delimiter.

## Notes

- Does not require a terminal — works in background/CI
- Long-running: stays alive for 30 seconds after sending
- Use SEND to deliver responses (printed to stderr)
