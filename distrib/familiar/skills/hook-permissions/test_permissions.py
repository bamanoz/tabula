#!/usr/bin/env python3
"""Tests for hook-permissions skill — permission rule matching."""

import json
import os
import sys
import tempfile
import unittest
from fnmatch import fnmatch

# We can't import run.py directly because it depends on kernel_client (websocket).
# Instead, copy the pure functions here and test them.
# Keep in sync with run.py.


# --- Copied from run.py (pure functions, no dependencies) ---

def load_rules(path: str) -> list[dict]:
    if not os.path.isfile(path):
        return []
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        rules = data.get("rules", []) if isinstance(data, dict) else []
        return [
            r for r in rules
            if isinstance(r, dict) and "tool" in r and "effect" in r
        ]
    except Exception:
        return []


def check_permission(rules: list[dict], tool_name: str, command: str = "") -> bool:
    tool_match_deny = False
    tool_match_allow = False
    cmd_match_deny = False
    cmd_match_allow = False

    for rule in rules:
        if not fnmatch(tool_name, rule["tool"]):
            continue
        rule_cmd = rule.get("command", "")
        if rule_cmd:
            if not command or not fnmatch(command, rule_cmd):
                continue
            if rule["effect"] == "deny":
                cmd_match_deny = True
            else:
                cmd_match_allow = True
        else:
            if rule["effect"] == "deny":
                tool_match_deny = True
            else:
                tool_match_allow = True

    if cmd_match_deny or cmd_match_allow:
        return not cmd_match_deny
    if tool_match_deny or tool_match_allow:
        return not tool_match_deny
    return True


def extract_command(payload: dict) -> str:
    tool = payload.get("tool", "")
    if tool not in ("EXEC", "SPAWN"):
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


# --- Tests ---

class TestCheckPermission(unittest.TestCase):
    def test_deny_exact(self):
        rules = [{"tool": "write", "effect": "deny"}]
        self.assertFalse(check_permission(rules, "write"))

    def test_deny_glob(self):
        rules = [{"tool": "write*", "effect": "deny"}]
        self.assertFalse(check_permission(rules, "write"))
        self.assertTrue(check_permission(rules, "read"))

    def test_command_pattern_deny(self):
        rules = [{"tool": "EXEC", "command": "rm -rf *", "effect": "deny"}]
        self.assertFalse(check_permission(rules, "EXEC", "rm -rf /"))
        self.assertTrue(check_permission(rules, "EXEC", "ls -la"))

    def test_command_pattern_no_match_without_command(self):
        rules = [{"tool": "EXEC", "command": "rm *", "effect": "deny"}]
        self.assertTrue(check_permission(rules, "EXEC"))

    def test_deny_overrides_allow(self):
        rules = [
            {"tool": "*", "effect": "allow"},
            {"tool": "EXEC", "effect": "deny"},
        ]
        self.assertFalse(check_permission(rules, "EXEC"))

    def test_allow_when_no_deny(self):
        rules = [{"tool": "*", "effect": "allow"}]
        self.assertTrue(check_permission(rules, "EXEC"))
        self.assertTrue(check_permission(rules, "write"))

    def test_default_allow_no_match(self):
        rules = [{"tool": "other_tool", "effect": "deny"}]
        self.assertTrue(check_permission(rules, "EXEC"))

    def test_empty_rules(self):
        self.assertTrue(check_permission([], "EXEC"))
        self.assertTrue(check_permission([], "write"))

    def test_force_push_deny(self):
        rules = [{"tool": "EXEC", "command": "git push *--force*", "effect": "deny"}]
        self.assertFalse(check_permission(rules, "EXEC", "git push --force origin main"))
        self.assertTrue(check_permission(rules, "EXEC", "git push origin main"))

    # --- Specificity tests ---

    def test_command_allow_overrides_tool_deny(self):
        """Command-level allow is more specific than tool-level deny."""
        rules = [
            {"tool": "EXEC", "command": "git *", "effect": "allow"},
            {"tool": "EXEC", "effect": "deny"},
        ]
        self.assertTrue(check_permission(rules, "EXEC", "git status"))
        self.assertFalse(check_permission(rules, "EXEC", "rm -rf /"))

    def test_exec_allowlist(self):
        """Only specific commands allowed, rest denied."""
        rules = [
            {"tool": "EXEC", "command": "git *", "effect": "allow"},
            {"tool": "EXEC", "command": "go *", "effect": "allow"},
            {"tool": "EXEC", "command": "python3 skills/*", "effect": "allow"},
            {"tool": "EXEC", "effect": "deny"},
            {"tool": "*", "effect": "allow"},
        ]
        self.assertTrue(check_permission(rules, "EXEC", "git status"))
        self.assertTrue(check_permission(rules, "EXEC", "go test ./..."))
        self.assertTrue(check_permission(rules, "EXEC", "python3 skills/foo/run.py"))
        self.assertFalse(check_permission(rules, "EXEC", "cat /etc/passwd"))
        self.assertFalse(check_permission(rules, "EXEC", "rm -rf /"))
        # Non-EXEC tools still allowed
        self.assertTrue(check_permission(rules, "read"))
        self.assertTrue(check_permission(rules, "write"))

    def test_command_deny_overrides_command_allow(self):
        """Among command-level rules, deny wins."""
        rules = [
            {"tool": "EXEC", "command": "git *", "effect": "allow"},
            {"tool": "EXEC", "command": "git push *--force*", "effect": "deny"},
            {"tool": "EXEC", "effect": "deny"},
        ]
        self.assertTrue(check_permission(rules, "EXEC", "git status"))
        self.assertFalse(check_permission(rules, "EXEC", "git push --force origin main"))

    def test_no_command_falls_to_tool_level(self):
        """EXEC without command uses tool-level rules (no command-level match)."""
        rules = [
            {"tool": "EXEC", "command": "git *", "effect": "allow"},
            {"tool": "EXEC", "effect": "deny"},
        ]
        # No command provided — falls to tool-level deny
        self.assertFalse(check_permission(rules, "EXEC"))

    def test_tool_deny_with_wildcard_allow(self):
        """Specific tool deny beats wildcard allow at same specificity."""
        rules = [
            {"tool": "*", "effect": "allow"},
            {"tool": "read", "effect": "deny"},
        ]
        self.assertFalse(check_permission(rules, "read"))
        self.assertTrue(check_permission(rules, "write"))


class TestExtractCommand(unittest.TestCase):
    def test_exec_command(self):
        payload = {"tool": "EXEC", "input": {"command": "ls -la"}}
        self.assertEqual(extract_command(payload), "ls -la")

    def test_spawn_command(self):
        payload = {"tool": "SPAWN", "input": {"command": "python3 run.py"}}
        self.assertEqual(extract_command(payload), "python3 run.py")

    def test_non_exec_tool(self):
        payload = {"tool": "write", "input": {"path": "/tmp/x"}}
        self.assertEqual(extract_command(payload), "")

    def test_string_input(self):
        payload = {"tool": "EXEC", "input": json.dumps({"command": "echo hi"})}
        self.assertEqual(extract_command(payload), "echo hi")

    def test_missing_input(self):
        payload = {"tool": "EXEC"}
        self.assertEqual(extract_command(payload), "")


class TestLoadRules(unittest.TestCase):
    def test_valid_file(self):
        data = {"rules": [
            {"tool": "EXEC", "effect": "deny"},
            {"tool": "*", "effect": "allow"},
        ]}
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(data, f)
            path = f.name
        try:
            rules = load_rules(path)
            self.assertEqual(len(rules), 2)
            self.assertEqual(rules[0]["tool"], "EXEC")
        finally:
            os.unlink(path)

    def test_missing_file(self):
        rules = load_rules("/nonexistent/path.json")
        self.assertEqual(rules, [])

    def test_empty_file(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            f.write("{}")
            path = f.name
        try:
            rules = load_rules(path)
            self.assertEqual(rules, [])
        finally:
            os.unlink(path)

    def test_invalid_json(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            f.write("not json")
            path = f.name
        try:
            rules = load_rules(path)
            self.assertEqual(rules, [])
        finally:
            os.unlink(path)

    def test_filters_invalid_rules(self):
        data = {"rules": [
            {"tool": "EXEC", "effect": "deny"},
            {"no_tool": True},
            {"tool": "x"},
            "not a dict",
        ]}
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(data, f)
            path = f.name
        try:
            rules = load_rules(path)
            self.assertEqual(len(rules), 1)
            self.assertEqual(rules[0]["tool"], "EXEC")
        finally:
            os.unlink(path)


if __name__ == "__main__":
    unittest.main()
