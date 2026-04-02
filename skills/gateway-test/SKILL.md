# gateway-test

Test gateway for automated testing. Sends a single hardcoded message after 2
seconds, prints any responses to stderr, then waits 30 seconds and exits.

## Usage

    SPAWN python3 skills/gateway-test/run.py

## Output

Sends "Привет, расскажи о себе" followed by an empty line delimiter.

## Notes

- Does not require a terminal — works in background/CI
- Long-running: stays alive for 30 seconds after sending
- Use SEND to deliver responses (printed to stderr)
