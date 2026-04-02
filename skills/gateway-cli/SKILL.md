# gateway-cli

Interactive CLI gateway. Reads user input from the terminal and sends it to
stdout (auto-piped to LLM). Receives responses on stdin via SEND.

## Usage

    SPAWN python3 skills/gateway-cli/run.py

## Output

Each user message is sent as text lines followed by an empty line (delimiter).

## Notes

- Requires an interactive terminal (/dev/tty)
- Long-running: stays alive for the entire session
- Use SEND with this process's PID to deliver responses to the user
