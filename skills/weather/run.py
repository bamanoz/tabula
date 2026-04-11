#!/usr/bin/env python3
"""Weather tool-skill — calls wttr.in via curl."""

from __future__ import annotations

import json
import subprocess
import sys


def tool_get_weather(params: dict) -> str:
    location = params.get("location", "")
    if not location:
        return json.dumps({"error": "location is required"})

    fmt = params.get("format", "summary")
    loc = location.replace(" ", "+")

    if fmt == "today":
        url = f"wttr.in/{loc}?0"
    elif fmt == "forecast":
        url = f"wttr.in/{loc}"
    else:
        url = f"wttr.in/{loc}?format=%l:+%c+%t+(feels+like+%f),+%w+wind,+%h+humidity"

    try:
        result = subprocess.run(
            ["curl", "-s", url],
            capture_output=True, text=True, timeout=10,
        )
        output = result.stdout.strip()
        if not output:
            return json.dumps({"error": "no response from wttr.in"})
        return output
    except subprocess.TimeoutExpired:
        return json.dumps({"error": "wttr.in request timed out"})
    except FileNotFoundError:
        return json.dumps({"error": "curl not found"})


TOOLS = {
    "get_weather": tool_get_weather,
}


def main():
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        tool_name = sys.argv[2]
        handler = TOOLS.get(tool_name)
        if not handler:
            print(f"ERROR: unknown tool {tool_name}")
            sys.exit(1)
        params = json.load(sys.stdin)
        result = handler(params)
        print(result)
    else:
        print("Usage: run.py tool <tool_name>", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
