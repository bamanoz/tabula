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

from skills.lib.providers import (
    OpenAIChatCompletionsSession,
    OpenAISession,
    kernel_to_openai_chat_tools,
    kernel_to_openai_tools,
)


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


class TestKernelToOpenAIChatTools(unittest.TestCase):
    def test_wraps_tool_schema_under_function(self):
        tools = kernel_to_openai_chat_tools([
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

        self.assertEqual(tools[0]["type"], "function")
        self.assertEqual(tools[0]["function"]["name"], "write_file")
        self.assertTrue(tools[0]["function"]["strict"])


class TestOpenAIRestoreHistory(unittest.TestCase):
    def test_restore_history_replays_text_only(self):
        session = OpenAISession(
            system_prompt="sys",
            model="gpt-5.4",
            api_key="test-key",
            base_url="https://api.openai.com/v1",
            tools=[],
        )

        session.restore_history([
            {"role": "user", "text": "hello"},
            {"role": "assistant", "tool_use": {"id": "call_1", "name": "EXEC", "input": {"command": "pwd"}}},
            {"role": "tool", "tool_use_id": "call_1", "output": "/tmp"},
            {"role": "assistant", "text": "done"},
        ])

        self.assertEqual(
            session.pending_input,
            [
                {"role": "user", "content": "hello"},
                {"role": "assistant", "content": "done"},
            ],
        )


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


class TestOpenAIChatCompletionsSession(unittest.TestCase):
    def test_generate_reconstructs_streamed_tool_call(self):
        session = OpenAIChatCompletionsSession(
            system_prompt="sys",
            model="gpt-5.4",
            api_key="test-key",
            base_url="https://api.openai.com/v1",
            tools=[],
        )
        session.add_user_text("hello")

        response = _FakeStreamingResponse([
            'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\\"path\\":\\"IDENTITY.md\\""}}]},"finish_reason":null}]}\n',
            '\n',
            'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":", \\\"content\\\":\\\"hi\\\"}"}}]},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":3}}\n',
            '\n',
            'data: [DONE]\n',
            '\n',
        ])

        with patch("skills.lib.providers.urllib.request.urlopen", return_value=response):
            outcome = session.generate(lambda _text: None)

        self.assertEqual(len(outcome.tool_calls), 1)
        self.assertEqual(outcome.tool_calls[0].id, "call_1")
        self.assertEqual(outcome.tool_calls[0].name, "write_file")
        self.assertEqual(outcome.tool_calls[0].input, {"path": "IDENTITY.md", "content": "hi"})
        self.assertEqual(session.messages[-1]["role"], "assistant")
        self.assertEqual(session.messages[-1]["tool_calls"][0]["id"], "call_1")


if __name__ == "__main__":
    unittest.main()
