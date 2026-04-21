#!/usr/bin/env python3
"""Unit tests for provider helpers."""

from __future__ import annotations

import os
import sys
import unittest
from types import SimpleNamespace
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


class _FakeError(Exception):
    def __init__(self, *, body=None, response=None, message="request failed"):
        super().__init__(message)
        self.body = body
        self.response = response
        self.message = message


class _FakeResponseStream:
    def __init__(self, events, final_response=None):
        self._events = list(events)
        self._final_response = final_response
        self.closed = False

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()

    def __iter__(self):
        return iter(self._events)

    def get_final_response(self):
        return self._final_response

    def close(self):
        self.closed = True


class _FakeResponsesAPI:
    def __init__(self, *, stream=None):
        self._stream = stream

    def stream(self, **kwargs):
        if isinstance(self._stream, Exception):
            raise self._stream
        return self._stream


class _FakeChatCompletionsAPI:
    def __init__(self, *, stream=None):
        self._stream = stream

    def create(self, **kwargs):
        if isinstance(self._stream, Exception):
            raise self._stream
        return self._stream


class _FakeOpenAIClient:
    def __init__(self, *, response_stream=None, chat_stream=None):
        self.responses = _FakeResponsesAPI(stream=response_stream)
        self.chat = SimpleNamespace(completions=_FakeChatCompletionsAPI(stream=chat_stream))


class TestKernelToOpenAITools(unittest.TestCase):
    def test_keeps_strict_for_fully_required_schema(self):
        tools = kernel_to_openai_tools([
            {
                "name": "write",
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
                "name": "read",
                "description": "Read file",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "offset": {"type": "integer", "description": "Offset"},
                },
                "required": ["path"],
            }
        ])

        self.assertNotIn("strict", tools[0])

    def test_preserves_nested_array_item_schema(self):
        tools = kernel_to_openai_tools([
            {
                "name": "multiedit",
                "description": "Apply edits",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "edits": {
                        "type": "array",
                        "description": "Edit operations",
                        "items": {
                            "type": "object",
                            "properties": {
                                "old_string": {"type": "string", "description": "Old"},
                                "new_string": {"type": "string", "description": "New"},
                                "replace_all": {"type": "boolean", "description": "Replace all"},
                            },
                            "required": ["old_string", "new_string"],
                        },
                    },
                },
                "required": ["path", "edits"],
            }
        ])

        edits = tools[0]["parameters"]["properties"]["edits"]
        self.assertEqual(edits["type"], "array")
        self.assertEqual(edits["items"]["type"], "object")
        self.assertEqual(set(edits["items"]["properties"].keys()), {"old_string", "new_string", "replace_all"})
        self.assertEqual(edits["items"]["required"], ["old_string", "new_string"])
        self.assertNotIn("strict", tools[0])


class TestKernelToOpenAIChatTools(unittest.TestCase):
    def test_wraps_tool_schema_under_function(self):
        tools = kernel_to_openai_chat_tools([
            {
                "name": "write",
                "description": "Write file",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "content": {"type": "string", "description": "Content"},
                },
                "required": ["path", "content"],
            }
        ])

        self.assertEqual(tools[0]["type"], "function")
        self.assertEqual(tools[0]["function"]["name"], "write")
        self.assertTrue(tools[0]["function"]["strict"])

    def test_preserves_nested_array_item_schema(self):
        tools = kernel_to_openai_chat_tools([
            {
                "name": "multiedit",
                "description": "Apply edits",
                "params": {
                    "path": {"type": "string", "description": "Path"},
                    "edits": {
                        "type": "array",
                        "description": "Edit operations",
                        "items": {
                            "type": "object",
                            "properties": {
                                "old_string": {"type": "string", "description": "Old"},
                                "new_string": {"type": "string", "description": "New"},
                            },
                            "required": ["old_string", "new_string"],
                        },
                    },
                },
                "required": ["path", "edits"],
            }
        ])

        edits = tools[0]["function"]["parameters"]["properties"]["edits"]
        self.assertEqual(edits["type"], "array")
        self.assertEqual(edits["items"]["type"], "object")
        self.assertEqual(set(edits["items"]["properties"].keys()), {"old_string", "new_string"})
        self.assertEqual(edits["items"]["required"], ["old_string", "new_string"])
        self.assertNotIn("strict", tools[0]["function"])


class TestOpenAIRestoreHistory(unittest.TestCase):
    def test_restore_history_replays_text_only(self):
        with patch("skills.lib.providers._openai_client", return_value=_FakeOpenAIClient()):
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
    def test_generate_includes_error_body_message(self):
        err = _FakeError(body={"error": {"message": "internal error"}})

        with patch("skills.lib.providers._openai_client", return_value=_FakeOpenAIClient(response_stream=err)):
            session = OpenAISession(
                system_prompt="sys",
                model="gpt-5.4",
                api_key="test-key",
                base_url="https://api.openai.com/v1",
                tools=[],
            )
        session.add_user_text("hello")

        with self.assertRaises(RuntimeError) as ctx:
            session.generate(lambda _text: None)

        self.assertIn("internal error", str(ctx.exception))


class TestOpenAIStreamingState(unittest.TestCase):
    def test_generate_reconstructs_function_calls_when_completed_output_is_empty(self):
        final_response = SimpleNamespace(
            id="resp_123",
            output=[],
            usage=SimpleNamespace(input_tokens=1, output_tokens=1),
        )
        stream = _FakeResponseStream(
            [
                SimpleNamespace(type="response.created", response=SimpleNamespace(id="resp_123")),
                SimpleNamespace(
                    type="response.output_item.added",
                    item=SimpleNamespace(type="function_call", id="fc_1", call_id="call_1", name="shell_exec", arguments=""),
                    item_id="fc_1",
                    output_index=0,
                ),
                SimpleNamespace(type="response.function_call_arguments.done", item_id="fc_1", arguments='{"command":"pwd"}'),
                SimpleNamespace(
                    type="response.output_item.done",
                    item=SimpleNamespace(type="function_call", id="fc_1", call_id="call_1", name="shell_exec", arguments='{"command":"pwd"}'),
                    output_index=0,
                ),
                SimpleNamespace(type="response.completed", response=final_response),
            ],
            final_response=final_response,
        )

        with patch("skills.lib.providers._openai_client", return_value=_FakeOpenAIClient(response_stream=stream)):
            session = OpenAISession(
                system_prompt="sys",
                model="gpt-5.4",
                api_key="test-key",
                base_url="https://api.openai.com/v1",
                tools=[],
            )
        session.add_user_text("hello")

        outcome = session.generate(lambda _text: None)

        self.assertEqual(len(outcome.tool_calls), 1)
        self.assertEqual(outcome.tool_calls[0].id, "call_1")
        self.assertEqual(outcome.tool_calls[0].name, "shell_exec")
        self.assertEqual(outcome.tool_calls[0].input, {"command": "pwd"})
        self.assertEqual(
            session.last_response_output,
            [{
                "type": "function_call",
                "id": "fc_1",
                "call_id": "call_1",
                "name": "shell_exec",
                "arguments": '{"command":"pwd"}',
                "status": "completed",
            }],
        )


class TestOpenAIChatCompletionsSession(unittest.TestCase):
    def test_generate_reconstructs_streamed_tool_call(self):
        stream = [
            SimpleNamespace(
                choices=[
                    SimpleNamespace(
                        delta=SimpleNamespace(
                            content="",
                            tool_calls=[
                                SimpleNamespace(
                                    index=0,
                                    id="call_1",
                                    function=SimpleNamespace(name="write", arguments='{"path":"IDENTITY.md"'),
                                )
                            ],
                        )
                    )
                ],
                usage=None,
            ),
            SimpleNamespace(
                choices=[
                    SimpleNamespace(
                        delta=SimpleNamespace(
                            content="",
                            tool_calls=[
                                SimpleNamespace(
                                    index=0,
                                    id=None,
                                    function=SimpleNamespace(name=None, arguments=', "content":"hi"}'),
                                )
                            ],
                        )
                    )
                ],
                usage=SimpleNamespace(prompt_tokens=10, completion_tokens=3),
            ),
        ]

        with patch("skills.lib.providers._openai_client", return_value=_FakeOpenAIClient(chat_stream=stream)):
            session = OpenAIChatCompletionsSession(
                system_prompt="sys",
                model="gpt-5.4",
                api_key="test-key",
                base_url="https://api.openai.com/v1",
                tools=[],
            )
        session.add_user_text("hello")

        outcome = session.generate(lambda _text: None)

        self.assertEqual(len(outcome.tool_calls), 1)
        self.assertEqual(outcome.tool_calls[0].id, "call_1")
        self.assertEqual(outcome.tool_calls[0].name, "write")
        self.assertEqual(outcome.tool_calls[0].input, {"path": "IDENTITY.md", "content": "hi"})
        self.assertEqual(session.messages[-1]["role"], "assistant")
        self.assertEqual(session.messages[-1]["tool_calls"][0]["id"], "call_1")


if __name__ == "__main__":
    unittest.main()
