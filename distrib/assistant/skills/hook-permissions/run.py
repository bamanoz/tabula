#!/usr/bin/env python3
"""Permission enforcement hook — blocks denied tool calls."""

from __future__ import annotations

import json
import os
import sys
from fnmatch import fnmatch

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.paths import skill_config_dir
from skills.lib.kernel_client import KernelConnection
from skills.lib.protocol import (
    MSG_CONNECT, MSG_HOOK, MSG_HOOK_RESULT, HOOK_PASS, HOOK_BLOCK,
    HOOK_BEFORE_TOOL_CALL, TOOL_SHELL_EXEC, TOOL_PROCESS_SPAWN,
)

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
PERMISSIONS_FILE = str(skill_config_dir("hook-permissions") / "permissions.json")


def load_rules(path: str = PERMISSIONS_FILE) -> list[dict]:
    """Load permission rules from JSON file. Returns empty list on any error."""
    if not os.path.isfile(path):
        return []
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        rules = data.get("rules", []) if isinstance(data, dict) else []
        # Validate each rule has required fields
        return [
            r for r in rules
            if isinstance(r, dict) and "tool" in r and "effect" in r
        ]
    except Exception as e:
        print(f"hook-permissions: failed to load {path}: {e}", file=sys.stderr)
        return []


def check_permission(rules: list[dict], tool_name: str, command: str = "") -> bool:
    """Check if a tool call is allowed. Returns True if allowed, False if denied.

    Specificity wins: command-level rules override tool-level rules.
    Within the same specificity, deny overrides allow.
    No matching rules = allow (default).
    """
    tool_match_deny = False
    tool_match_allow = False
    cmd_match_deny = False
    cmd_match_allow = False

    for rule in rules:
        if not fnmatch(tool_name, rule["tool"]):
            continue
        rule_cmd = rule.get("command", "")
        if rule_cmd:
            # Command-level rule — only matches if command provided and matches
            if not command or not fnmatch(command, rule_cmd):
                continue
            if rule["effect"] == "deny":
                cmd_match_deny = True
            else:
                cmd_match_allow = True
        else:
            # Tool-level rule (no command pattern)
            if rule["effect"] == "deny":
                tool_match_deny = True
            else:
                tool_match_allow = True

    # Command-level rules take precedence over tool-level
    if cmd_match_deny or cmd_match_allow:
        return not cmd_match_deny
    if tool_match_deny or tool_match_allow:
        return not tool_match_deny
    # No match = allow
    return True


def extract_command(payload: dict) -> str:
    """Extract command string from tool input for shell_exec/process_spawn."""
    tool = payload.get("tool", "")
    if tool not in (TOOL_SHELL_EXEC, TOOL_PROCESS_SPAWN):
        return ""
    raw_input = payload.get("input")
    if not raw_input:
        return ""
    try:
        if isinstance(raw_input, str):
            inp = json.loads(raw_input)
        else:
            inp = raw_input
        return inp.get("command", "")
    except (json.JSONDecodeError, AttributeError):
        return ""


def run(url: str = TABULA_URL):
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "hook-permissions",
        "sends": [MSG_HOOK_RESULT],
        "receives": [MSG_HOOK],
        "hooks": [{"event": HOOK_BEFORE_TOOL_CALL, "priority": 100}],
    })
    conn.recv()  # connected

    # No join — global subscriber, receives hooks for all sessions.

    try:
        while True:
            msg = conn.recv()
            if msg is None:
                break
            if msg.get("type") != MSG_HOOK:
                continue

            rules = load_rules()
            hook_id = msg.get("id", "")
            payload = msg.get("payload", {})
            tool_name = payload.get("tool", "")
            command = extract_command(payload)

            if check_permission(rules, tool_name, command):
                conn.send({"type": MSG_HOOK_RESULT, "id": hook_id, "action": HOOK_PASS})
            else:
                reason = f"tool '{tool_name}' denied by permissions policy"
                if command:
                    reason = f"command '{command}' denied by permissions policy"
                conn.send({
                    "type": MSG_HOOK_RESULT,
                    "id": hook_id,
                    "action": HOOK_BLOCK,
                    "reason": reason,
                })
    except KeyboardInterrupt:
        pass
    finally:
        conn.close()


if __name__ == "__main__":
    run()
