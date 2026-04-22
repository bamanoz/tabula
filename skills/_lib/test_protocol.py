#!/usr/bin/env python3
"""Tests for protocol constants — ensures consistency between Go and Python sides."""

import unittest

try:
    import protocol
except ModuleNotFoundError:
    from . import protocol


class TestProtocolVersion(unittest.TestCase):
    def test_version_is_int(self):
        self.assertIsInstance(protocol.PROTOCOL_VERSION, int)

    def test_version_matches_go(self):
        """Go kernel's ProtocolVersion = 1. Python must match."""
        self.assertEqual(protocol.PROTOCOL_VERSION, 1)


class TestMessageTypes(unittest.TestCase):
    def test_all_types_are_strings(self):
        """Every MSG_* constant must be a non-empty string."""
        for name in dir(protocol):
            if name.startswith("MSG_"):
                val = getattr(protocol, name)
                self.assertIsInstance(val, str, f"{name} is not a string")
                self.assertTrue(len(val) > 0, f"{name} is empty string")

    def test_no_duplicates(self):
        """All message type values must be unique — typos cause silent failures."""
        values = []
        for name in dir(protocol):
            if name.startswith("MSG_"):
                values.append(getattr(protocol, name))
        self.assertEqual(len(values), len(set(values)), "duplicate message type values found")

    def test_all_message_types_set(self):
        """ALL_MESSAGE_TYPES should contain every MSG_* constant."""
        expected = set()
        for name in dir(protocol):
            if name.startswith("MSG_"):
                expected.add(getattr(protocol, name))
        self.assertEqual(protocol.ALL_MESSAGE_TYPES, expected)


class TestHookActions(unittest.TestCase):
    def test_hook_actions_are_strings(self):
        for name in ["HOOK_PASS", "HOOK_MODIFY", "HOOK_BLOCK", "HOOK_CLAIM"]:
            val = getattr(protocol, name)
            self.assertIsInstance(val, str)
            self.assertTrue(len(val) > 0)

    def test_no_duplicate_hook_actions(self):
        values = [protocol.HOOK_PASS, protocol.HOOK_MODIFY, protocol.HOOK_BLOCK, protocol.HOOK_CLAIM]
        self.assertEqual(len(values), len(set(values)))


class TestKernelTools(unittest.TestCase):
    def test_tool_constants(self):
        for name in ["TOOL_SHELL_EXEC", "TOOL_PROCESS_SPAWN", "TOOL_PROCESS_KILL", "TOOL_PROCESS_LIST"]:
            val = getattr(protocol, name)
            self.assertIsInstance(val, str)
            self.assertTrue(len(val) > 0)

    def test_expected_tool_names(self):
        self.assertEqual(protocol.TOOL_SHELL_EXEC, "shell_exec")
        self.assertEqual(protocol.TOOL_PROCESS_SPAWN, "process_spawn")
        self.assertEqual(protocol.TOOL_PROCESS_KILL, "process_kill")
        self.assertEqual(protocol.TOOL_PROCESS_LIST, "process_list")

    def test_no_duplicate_tool_names(self):
        values = [
            protocol.TOOL_SHELL_EXEC,
            protocol.TOOL_PROCESS_SPAWN,
            protocol.TOOL_PROCESS_KILL,
            protocol.TOOL_PROCESS_LIST,
        ]
        self.assertEqual(len(values), len(set(values)))


class TestHookEvents(unittest.TestCase):
    def test_hook_events_are_strings(self):
        for name in dir(protocol):
            if name.startswith("HOOK_") and name not in ("HOOK_PASS", "HOOK_MODIFY", "HOOK_BLOCK", "HOOK_CLAIM"):
                val = getattr(protocol, name)
                self.assertIsInstance(val, str)
                self.assertTrue(len(val) > 0)

    def test_no_duplicate_hook_events(self):
        values = []
        for name in dir(protocol):
            if name.startswith("HOOK_") and name not in ("HOOK_PASS", "HOOK_MODIFY", "HOOK_BLOCK", "HOOK_CLAIM"):
                values.append(getattr(protocol, name))
        self.assertEqual(len(values), len(set(values)))


if __name__ == "__main__":
    unittest.main()
