#!/usr/bin/env python3
"""Unit tests for provider helpers."""

from __future__ import annotations

import io
import os
import sys
import unittest
import urllib.error
from unittest.mock import patch


ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.providers import OpenAISession, kernel_to_openai_tools


class TestKernelToOpenAITools(unittest.TestCase):
    def test_keeps_strict_for_fully_required_schema(self):
        tools = kernel_to_openai_tools([
            {
                "name": "write_file",
                "description": "Write file",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "content": {"type": "string", "description": "Content"},
                },
                "required": ["path", "content"],
            }
        ])

        self.assertTrue(tools[0]["strict"])

    def test_omits_strict_for_optional_params_schema(self):
        tools = kernel_to_openai_tools([
            {
                "name": "read_file",
                "description": "Read file",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "offset": {"type": "integer", "description": "Offset"},
                },
                "required": ["path"],
            }
        ])

        self.assertNotIn("strict", tools[0])


class TestOpenAIErrorReporting(unittest.TestCase):
    def test_generate_includes_http_error_body_message(self):
        session = OpenAISession(
            system_prompt="sys",
            model="gpt-5.4",
            api_key="test-key",
            base_url="https://api.openai.com/v1",
            tools=[],
        )
        session.add_user_text("hello")

        err = urllib.error.HTTPError(
            url="https://api.openai.com/v1/responses",
            code=400,
            msg="Bad Request",
            hdrs=None,
            fp=io.BytesIO(b'{"error":{"message":"internal error"}}'),
        )

        with patch("skills.lib.providers.urllib.request.urlopen", side_effect=err):
            with self.assertRaises(RuntimeError) as ctx:
                session.generate(lambda _text: None)

        self.assertIn("internal error", str(ctx.exception))


class _FakeStreamingResponse:
    def __init__(self, chunks: list[str]):
        self._chunks = [chunk.encode("utf-8") for chunk in chunks]

    def __iter__(self):
        return iter(self._chunks)

    def close(self):
        return None


class TestOpenAIStreamingState(unittest.TestCase):
    def test_generate_reconstructs_function_calls_when_completed_output_is_empty(self):
        session = OpenAISession(
            system_prompt="sys",
            model="gpt-5.4",
            api_key="test-key",
            base_url="https://api.openai.com/v1",
            tools=[],
        )
        session.add_user_text("hello")

        response = _FakeStreamingResponse([
            'event: response.created\n',
            'data: {"type":"response.created","response":{"id":"resp_123"}}\n',
            '\n',
            'event: response.output_item.added\n',
            'data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"EXEC","arguments":""}}\n',
            '\n',
            'event: response.function_call_arguments.done\n',
            'data: {"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\\"command\\":\\"pwd\\"}"}\n',
            '\n',
            'event: response.output_item.done\n',
            'data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"EXEC","arguments":"{\\"command\\":\\"pwd\\"}"}}\n',
            '\n',
            'event: response.completed\n',
            'data: {"type":"response.completed","response":{"id":"resp_123","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}\n',
            '\n',
        ])

        with patch("skills.lib.providers.urllib.request.urlopen", return_value=response):
            outcome = session.generate(lambda _text: None)

        self.assertEqual(len(outcome.tool_calls), 1)
        self.assertEqual(outcome.tool_calls[0].id, "call_1")
        self.assertEqual(outcome.tool_calls[0].name, "EXEC")
        self.assertEqual(outcome.tool_calls[0].input, {"command": "pwd"})
        self.assertEqual(
            session.last_response_output,
            [{
                "type": "function_call",
                "id": "fc_1",
                "call_id": "call_1",
                "name": "EXEC",
                "arguments": '{"command":"pwd"}',
                "status": "completed",
            }],
        )


if __name__ == "__main__":
    unittest.main()
