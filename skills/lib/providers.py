#!/usr/bin/env python3
"""Provider adapters for Anthropic and OpenAI."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from abc import ABC, abstractmethod
from dataclasses import dataclass


@dataclass
class ToolCall:
    id: str
    name: str
    input: dict


@dataclass
class ToolResult:
    tool_use_id: str
    output: str


@dataclass
class TurnOutcome:
    final_text: str
    tool_calls: list[ToolCall]
    usage: dict | None = None


def iter_sse_events(resp):
    """Yield parsed SSE events from an HTTP response."""
    data_lines: list[str] = []
    event_type = ""

    for raw_line in resp:
        line = raw_line.decode("utf-8", errors="replace").rstrip("\r\n")
        if not line:
            if not data_lines:
                event_type = ""
                continue
            payload = "\n".join(data_lines)
            if payload != "[DONE]":
                try:
                    data = json.loads(payload)
                except json.JSONDecodeError:
                    data = {"type": event_type or "message", "data": payload}
                if event_type and "type" not in data:
                    data["type"] = event_type
                yield data
            data_lines = []
            event_type = ""
            continue
        if line.startswith("event: "):
            event_type = line[7:]
            continue
        if line.startswith("data: "):
            data_lines.append(line[6:])

    if data_lines:
        payload = "\n".join(data_lines)
        if payload != "[DONE]":
            data = json.loads(payload)
            if event_type and "type" not in data:
                data["type"] = event_type
            yield data


def kernel_to_anthropic_tools(kernel_tools: list[dict]) -> list[dict]:
    result = []
    for tool in kernel_tools:
        result.append(
            {
                "name": tool["name"],
                "description": tool["description"],
                "input_schema": {
                    "type": "object",
                    "properties": {
                        key: {"type": value["type"], "description": value["description"]}
                        for key, value in tool.get("params", {}).items()
                    },
                    "required": tool.get("required", []),
                },
            }
        )
    return result


def kernel_to_openai_tools(kernel_tools: list[dict]) -> list[dict]:
    result = []
    for tool in kernel_tools:
        result.append(
            {
                "type": "function",
                "name": tool["name"],
                "description": tool["description"],
                "strict": True,
                "parameters": {
                    "type": "object",
                    "properties": {
                        key: {"type": value["type"], "description": value["description"]}
                        for key, value in tool.get("params", {}).items()
                    },
                    "required": tool.get("required", []),
                    "additionalProperties": False,
                },
            }
        )
    return result


def normalize_api_base(base_url: str, version_prefix: str) -> str:
    base = base_url.rstrip("/")
    if base.endswith(version_prefix):
        return base[: -len(version_prefix)]
    return base


class ProviderSession(ABC):
    def __init__(self, system_prompt: str):
        self.system_prompt = system_prompt
        self._current_resp = None

    @abstractmethod
    def add_user_text(self, text: str):
        raise NotImplementedError

    @abstractmethod
    def add_tool_results(self, results: list[ToolResult]):
        raise NotImplementedError

    @abstractmethod
    def generate(self, on_text_delta) -> TurnOutcome:
        raise NotImplementedError

    def abort(self):
        resp = self._current_resp
        if resp:
            try:
                resp.close()
            except Exception:
                pass

    def record_aborted_turn(self):
        """Keep provider state coherent after a cancelled turn."""

    def restore_history(self, entries: list[dict]):
        """Replay history entries to rebuild conversation state."""

    def needs_compact(self) -> bool:
        """Check if compaction is needed (without performing it)."""
        return False

    def compact(self, logger=None) -> str:
        """Compact conversation history if needed. Returns summary text or empty string."""
        return ""


class AnthropicSession(ProviderSession):
    def __init__(self, *, system_prompt: str, model: str, api_key: str, base_url: str, tools: list[dict]):
        super().__init__(system_prompt)
        self.model = model
        self.api_key = api_key
        self.base_url = normalize_api_base(base_url, "/v1")
        self.api_url = f"{self.base_url}/v1/messages"
        self.tools = kernel_to_anthropic_tools(tools)
        self.messages: list[dict] = []

    def add_user_text(self, text: str):
        self.messages.append({"role": "user", "content": text})

    def add_tool_results(self, results: list[ToolResult]):
        self.messages.append(
            {
                "role": "user",
                "content": [
                    {
                        "type": "tool_result",
                        "tool_use_id": result.tool_use_id,
                        "content": result.output,
                    }
                    for result in results
                ],
            }
        )

    def record_aborted_turn(self):
        self.messages.append(
            {
                "role": "assistant",
                "content": [{"type": "text", "text": "[cancelled]"}],
            }
        )

    def restore_history(self, entries: list[dict]):
        for entry in entries:
            role = entry.get("role")
            if role == "user":
                self.messages.append({"role": "user", "content": entry["text"]})
            elif role == "assistant" and "text" in entry:
                self.messages.append({"role": "assistant", "content": [{"type": "text", "text": entry["text"]}]})
            elif role == "assistant" and "tool_use" in entry:
                tu = entry["tool_use"]
                self.messages.append({
                    "role": "assistant",
                    "content": [{"type": "tool_use", "id": tu["id"], "name": tu["name"], "input": tu.get("input", {})}],
                })
            elif role == "tool":
                self.messages.append({
                    "role": "user",
                    "content": [{"type": "tool_result", "tool_use_id": entry["tool_use_id"], "content": entry.get("output", "")}],
                })

    def needs_compact(self) -> bool:
        from .compaction import should_compact
        return should_compact(self.messages, self.model, self.system_prompt)

    def compact(self, logger=None) -> str:
        from .compaction import should_compact, compact_messages_anthropic
        if not should_compact(self.messages, self.model, self.system_prompt):
            return ""
        new_messages, summary = compact_messages_anthropic(
            api_key=self.api_key,
            api_url=self.api_url,
            model=self.model,
            system_prompt=self.system_prompt,
            messages=self.messages,
            logger=logger,
        )
        if summary:
            self.messages = new_messages
        return summary

    def generate(self, on_text_delta) -> TurnOutcome:
        body = {
            "model": self.model,
            "max_tokens": 4096,
            "system": self.system_prompt,
            "messages": self.messages,
            "stream": True,
        }
        if self.tools:
            body["tools"] = self.tools

        req = urllib.request.Request(
            self.api_url,
            data=json.dumps(body).encode(),
            headers={
                "Content-Type": "application/json",
                "x-api-key": self.api_key,
                "anthropic-version": "2023-06-01",
            },
        )

        resp = urllib.request.urlopen(req, timeout=300)
        self._current_resp = resp
        sock = resp.fp.raw._sock if hasattr(resp.fp, "raw") and hasattr(resp.fp.raw, "_sock") else None
        if sock:
            sock.settimeout(600)

        content_blocks: list[dict] = []
        tool_calls: list[ToolCall] = []
        text_parts: list[str] = []
        current_text = ""
        current_tool = None
        usage: dict = {"input_tokens": 0, "output_tokens": 0}

        try:
            for data in iter_sse_events(resp):
                event_type = data.get("type")

                if event_type == "message_start":
                    msg_usage = data.get("message", {}).get("usage", {})
                    usage["input_tokens"] = (
                        msg_usage.get("input_tokens", 0)
                        + msg_usage.get("cache_creation_input_tokens", 0)
                        + msg_usage.get("cache_read_input_tokens", 0)
                    )

                elif event_type == "message_delta":
                    delta_usage = data.get("usage", {})
                    if delta_usage.get("output_tokens"):
                        usage["output_tokens"] = delta_usage["output_tokens"]

                elif event_type == "content_block_start":
                    block = data["content_block"]
                    if block["type"] == "text":
                        current_text = ""
                    elif block["type"] == "tool_use":
                        current_tool = {
                            "id": block["id"],
                            "name": block["name"],
                            "input_json": "",
                        }

                elif event_type == "content_block_delta":
                    delta = data["delta"]
                    if delta["type"] == "text_delta":
                        current_text += delta["text"]
                        text_parts.append(delta["text"])
                        on_text_delta(delta["text"])
                    elif delta["type"] == "input_json_delta" and current_tool:
                        current_tool["input_json"] += delta["partial_json"]

                elif event_type == "content_block_stop":
                    if current_tool:
                        try:
                            input_data = json.loads(current_tool["input_json"]) if current_tool["input_json"] else {}
                        except json.JSONDecodeError:
                            input_data = {}
                        block = {
                            "type": "tool_use",
                            "id": current_tool["id"],
                            "name": current_tool["name"],
                            "input": input_data,
                        }
                        content_blocks.append(block)
                        tool_calls.append(ToolCall(id=block["id"], name=block["name"], input=block["input"]))
                        current_tool = None
                    elif current_text:
                        content_blocks.append({"type": "text", "text": current_text})
                        current_text = ""

        finally:
            self._current_resp = None
            try:
                resp.close()
            except Exception:
                pass

        if not content_blocks and text_parts:
            content_blocks.append({"type": "text", "text": "".join(text_parts)})

        self.messages.append({"role": "assistant", "content": content_blocks})
        return TurnOutcome(final_text="".join(text_parts), tool_calls=tool_calls, usage=usage)


class OpenAISession(ProviderSession):
    def __init__(self, *, system_prompt: str, model: str, api_key: str, base_url: str, tools: list[dict]):
        super().__init__(system_prompt)
        self.model = model
        self.api_key = api_key
        self.base_url = normalize_api_base(base_url, "/v1")
        self.api_url = f"{self.base_url}/v1/responses"
        self.tools = kernel_to_openai_tools(tools)
        self.previous_response_id: str | None = None
        self.pending_input: list[dict] = []
        self.last_response_output: list[dict] = []

    def add_user_text(self, text: str):
        self.last_response_output = []
        self.pending_input.append({"role": "user", "content": text})

    def add_tool_results(self, results: list[ToolResult]):
        if self.last_response_output:
            self.pending_input.extend(self.last_response_output)
            self.last_response_output = []
        for result in results:
            self.pending_input.append(
                {
                    "type": "function_call_output",
                    "call_id": result.tool_use_id,
                    "output": result.output,
                }
            )

    def record_aborted_turn(self):
        self.pending_input = []
        self.last_response_output = []

    def restore_history(self, entries: list[dict]):
        for entry in entries:
            role = entry.get("role")
            if role == "user":
                self.pending_input.append({"role": "user", "content": entry["text"]})
            elif role == "assistant" and "text" in entry:
                self.pending_input.append({"role": "assistant", "content": entry["text"]})
            elif role == "assistant" and "tool_use" in entry:
                tu = entry["tool_use"]
                self.pending_input.append({
                    "type": "function_call",
                    "id": tu["id"],
                    "name": tu["name"],
                    "arguments": json.dumps(tu.get("input", {})),
                })
            elif role == "tool":
                self.pending_input.append({
                    "type": "function_call_output",
                    "call_id": entry["tool_use_id"],
                    "output": entry.get("output", ""),
                })

    def needs_compact(self) -> bool:
        from .compaction import should_compact
        return should_compact(self.pending_input, self.model, self.system_prompt)

    def compact(self, logger=None) -> str:
        from .compaction import should_compact, compact_messages_openai
        if not should_compact(self.pending_input, self.model, self.system_prompt):
            return ""
        new_input, summary = compact_messages_openai(
            api_key=self.api_key,
            api_url=self.api_url,
            model=self.model,
            system_prompt=self.system_prompt,
            pending_input=self.pending_input,
            logger=logger,
        )
        if summary:
            self.pending_input = new_input
        return summary

    def generate(self, on_text_delta) -> TurnOutcome:
        body = {
            "model": self.model,
            "instructions": self.system_prompt,
            "input": self.pending_input,
            "parallel_tool_calls": True,
            "stream": True,
        }
        if self.tools:
            body["tools"] = self.tools
        if self.previous_response_id:
            body["previous_response_id"] = self.previous_response_id

        req = urllib.request.Request(
            self.api_url,
            data=json.dumps(body).encode(),
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self.api_key}",
            },
        )

        resp = urllib.request.urlopen(req, timeout=300)
        self._current_resp = resp
        sock = resp.fp.raw._sock if hasattr(resp.fp, "raw") and hasattr(resp.fp.raw, "_sock") else None
        if sock:
            sock.settimeout(600)

        response_id = None
        completed_output: list[dict] = []
        text_parts: list[str] = []
        tool_state: dict[str, dict] = {}
        tool_order: list[str] = []
        usage: dict = {"input_tokens": 0, "output_tokens": 0}

        try:
            for data in iter_sse_events(resp):
                event_type = data.get("type")

                if event_type == "response.created":
                    response_id = data.get("response", {}).get("id")
                elif event_type == "response.output_text.delta":
                    delta = data.get("delta", "")
                    if delta:
                        text_parts.append(delta)
                        on_text_delta(delta)
                elif event_type == "response.output_item.added":
                    item = data.get("item", {})
                    if item.get("type") == "function_call":
                        item_id = item.get("id") or data.get("item_id") or str(data.get("output_index"))
                        if item_id not in tool_state:
                            tool_order.append(item_id)
                        tool_state[item_id] = {
                            "call_id": item.get("call_id", ""),
                            "name": item.get("name", ""),
                            "arguments": item.get("arguments", "") or "",
                        }
                elif event_type == "response.function_call_arguments.delta":
                    item_id = data.get("item_id")
                    if item_id in tool_state:
                        tool_state[item_id]["arguments"] += data.get("delta", "")
                elif event_type == "response.function_call_arguments.done":
                    item_id = data.get("item_id")
                    if item_id in tool_state and data.get("arguments") is not None:
                        tool_state[item_id]["arguments"] = data["arguments"]
                elif event_type == "response.output_item.done":
                    item = data.get("item", {})
                    if item.get("type") == "function_call":
                        item_id = item.get("id") or str(data.get("output_index"))
                        if item_id not in tool_state:
                            tool_order.append(item_id)
                            tool_state[item_id] = {}
                        tool_state[item_id].update(
                            {
                                "call_id": item.get("call_id", tool_state[item_id].get("call_id", "")),
                                "name": item.get("name", tool_state[item_id].get("name", "")),
                                "arguments": item.get("arguments", tool_state[item_id].get("arguments", "")),
                            }
                        )
                elif event_type == "response.completed":
                    response = data.get("response", {})
                    response_id = response.get("id", response_id)
                    completed_output = response.get("output", [])
                    resp_usage = response.get("usage", {})
                    if resp_usage:
                        usage["input_tokens"] = resp_usage.get("input_tokens", 0)
                        usage["output_tokens"] = resp_usage.get("output_tokens", 0)
                    for item in completed_output:
                        if item.get("type") == "function_call":
                            item_id = item.get("id") or item.get("call_id")
                            if item_id and item_id not in tool_state:
                                tool_order.append(item_id)
                                tool_state[item_id] = {
                                    "call_id": item.get("call_id", ""),
                                    "name": item.get("name", ""),
                                    "arguments": item.get("arguments", "") or "",
                                }
                        elif item.get("type") == "message":
                            for content in item.get("content", []):
                                if content.get("type") == "output_text":
                                    text = content.get("text", "")
                                    if text and not text_parts:
                                        text_parts.append(text)
                elif event_type == "error":
                    message = data.get("error", {}).get("message") or data.get("message") or "OpenAI stream error"
                    raise RuntimeError(message)
        finally:
            self._current_resp = None
            try:
                resp.close()
            except Exception:
                pass

        self.pending_input = []
        if response_id:
            self.previous_response_id = response_id
        self.last_response_output = completed_output

        tool_calls: list[ToolCall] = []
        for item_id in tool_order:
            tool = tool_state[item_id]
            try:
                input_data = json.loads(tool.get("arguments") or "{}")
            except json.JSONDecodeError:
                input_data = {}
            tool_calls.append(
                ToolCall(
                    id=tool.get("call_id") or item_id,
                    name=tool.get("name", ""),
                    input=input_data,
                )
            )

        return TurnOutcome(final_text="".join(text_parts), tool_calls=tool_calls, usage=usage)


# ---------------------------------------------------------------------------
# Mock provider — deterministic, no LLM calls
# ---------------------------------------------------------------------------

import enum
import os
import re
import shlex
import sys


def _venv_python() -> str:
    """Return path to venv python, falling back to current interpreter."""
    tabula_home = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
    venv = os.path.join(tabula_home, ".venv", "bin", "python3")
    if os.path.isfile(venv):
        return venv
    return sys.executable


class _MockState(enum.Enum):
    IDLE = "idle"
    SPAWN = "spawn"
    TOOLS_SENT = "tools_sent"
    ENTER_COLLECTION = "enter_collection"
    FINISH = "finish"
    BUSY_REPLY = "busy_reply"


@dataclass
class MockConfig:
    subagent_count: int = 3
    mock_turns: int = 5
    mock_sleep_ms: int = 25
    default_waves: int = 1
    default_fanouts: list[int] | None = None


class MockProvider(ProviderSession):
    """Deterministic provider for testing subagent orchestration."""

    def __init__(self, *, system_prompt: str, tools: list[dict], config: MockConfig):
        super().__init__(system_prompt)
        self.config = config
        self.tools_spec = tools

        # Extract session name from system prompt (injected by driver_runtime)
        session_match = re.search(r"Your session name is `([^`]+)`", system_prompt)
        self._session = session_match.group(1) if session_match else "main"

        self._state = _MockState.IDLE
        self._turn_no = 0
        self._wave_no = 0
        self._wave_counts: list[int] = []
        self._user_text = ""
        self._output_buffer: list[str] = []
        self._all_results: dict[str, str] = {}
        self._current_wave_ids: list[str] = []
        self._active = False

    def _parse_request(self, text: str) -> tuple[str, list[int]]:
        fanouts_match = re.search(r"fanouts=([0-9,]+)", text)
        wave_match = re.search(r"waves=(\d+)", text)
        subagent_match = re.search(r"subagents=(\d+)", text)
        if fanouts_match:
            wave_counts = [max(1, int(p)) for p in fanouts_match.group(1).split(",") if p.strip()]
        else:
            waves = max(1, int(wave_match.group(1))) if wave_match else self.config.default_waves
            per_wave = max(1, int(subagent_match.group(1))) if subagent_match else self.config.subagent_count
            if self.config.default_fanouts and not wave_match and not subagent_match:
                wave_counts = list(self.config.default_fanouts)
            else:
                wave_counts = [per_wave] * waves
        cleaned = re.sub(r"\s*waves=\d+\s*", " ", text)
        cleaned = re.sub(r"\s*subagents=\d+\s*", " ", cleaned)
        cleaned = re.sub(r"\s*fanouts=[0-9,]+\s*", " ", cleaned)
        cleaned = " ".join(cleaned.split()) or text
        return cleaned, wave_counts

    def _build_wave_agent_ids(self) -> list[str]:
        count = self._wave_counts[self._wave_no - 1]
        if len(self._wave_counts) == 1:
            return [f"mock_{self._turn_no}_{i}" for i in range(1, count + 1)]
        else:
            return [f"mock_{self._turn_no}_w{self._wave_no}_{i}" for i in range(1, count + 1)]

    def _build_spawn_tool_calls(self, agent_ids: list[str]) -> list[ToolCall]:
        calls = []
        for index, agent_id in enumerate(sorted(agent_ids), start=1):
            tool_id = f"spawn_{agent_id}"
            command = " ".join([
                _venv_python(), "skills/subagent-mock/run.py",
                "--id", shlex.quote(agent_id),
                "--parent-session", self._session,
                "--task", shlex.quote(self._user_text),
                "--index", str(index),
                "--max-turns", str(self.config.mock_turns),
                "--sleep-ms", str(self.config.mock_sleep_ms),
            ])
            calls.append(ToolCall(id=tool_id, name="SPAWN", input={"command": command}))
        return calls

    def _build_wave_agent_ids_for(self, wave_no: int) -> list[str]:
        count = self._wave_counts[wave_no - 1]
        if len(self._wave_counts) == 1:
            return [f"mock_{self._turn_no}_{i}" for i in range(1, count + 1)]
        else:
            return [f"mock_{self._turn_no}_w{wave_no}_{i}" for i in range(1, count + 1)]

    def _build_aggregation(self) -> str:
        total_expected = sum(self._wave_counts)
        ordered_ids = sorted(self._all_results)
        lines = [
            "mock driver: aggregated subagent results",
            f"request: {self._user_text}",
            f"waves: {len(self._wave_counts)}",
            f"received: {len(self._all_results)}/{total_expected}",
        ]
        for agent_id in ordered_ids:
            lines.append(f"- {agent_id}: {self._all_results[agent_id]}")
        missing = []
        for w in range(1, len(self._wave_counts) + 1):
            for agent_id in self._build_wave_agent_ids_for(w):
                if agent_id not in self._all_results:
                    missing.append(agent_id)
        if missing:
            lines.append(f"missing: {', '.join(sorted(missing))}")
        return "\n".join(lines) + "\n"

    def add_user_text(self, text: str):
        # Flushed subagent results from DriverRuntime in XML format
        if self._active and "<subagent_result " in text:
            for part in text.split("\n\n---\n\n"):
                m = re.match(r'<subagent_result id="([^"]+)">\n(.*)\n</subagent_result>', part, re.DOTALL)
                if m:
                    agent_id, result_text = m.group(1), m.group(2)
                    self._all_results[agent_id] = result_text
                    self._output_buffer.append(f"mock driver: received result from {agent_id}\n")
            if self._wave_no < len(self._wave_counts):
                self._wave_no += 1
                self._output_buffer.append(
                    f"mock driver: wave {self._wave_no - 1} complete, launching next wave\n"
                )
                self._state = _MockState.SPAWN
            else:
                self._state = _MockState.FINISH
            return

        if self._active:
            self._output_buffer.append("mock driver: busy, ignoring concurrent user message\n")
            self._state = _MockState.BUSY_REPLY
            return

        # New request
        self._turn_no += 1
        self._active = True
        self._all_results = {}
        self._output_buffer = []
        self._user_text, self._wave_counts = self._parse_request(text)
        self._wave_no = 1
        self._state = _MockState.SPAWN

    def add_tool_results(self, results: list[ToolResult]):
        for result in results:
            self._output_buffer.append(f"mock driver: {result.tool_use_id} -> {result.output}\n")
        self._state = _MockState.ENTER_COLLECTION

    def generate(self, on_text_delta) -> TurnOutcome:
        if self._state == _MockState.BUSY_REPLY:
            # Busy text stays in buffer — will be included in final output.
            # Return empty so DriverRuntime resumes collection.
            self._state = _MockState.ENTER_COLLECTION
            return TurnOutcome(final_text="", tool_calls=[])

        if self._state == _MockState.SPAWN:
            agent_ids = self._build_wave_agent_ids()
            self._current_wave_ids = agent_ids
            count = self._wave_counts[self._wave_no - 1]
            header = (
                f"mock driver: spawning wave {self._wave_no}/{len(self._wave_counts)} "
                f"with {count} mock subagents for request: {self._user_text}\n"
            )
            on_text_delta(header)
            # Also buffer header so it appears in final output even when
            # on_text_delta is suppressed (wave 2+ runs with suppress_stream)
            self._output_buffer.append(header)
            calls = self._build_spawn_tool_calls(agent_ids)
            self._state = _MockState.TOOLS_SENT
            return TurnOutcome(final_text=header, tool_calls=calls)

        if self._state == _MockState.ENTER_COLLECTION:
            self._state = _MockState.IDLE
            return TurnOutcome(final_text="", tool_calls=[])

        if self._state == _MockState.FINISH:
            aggregation = self._build_aggregation()
            buffered = "".join(self._output_buffer)
            self._output_buffer = []
            self._active = False
            self._wave_no = 0
            self._wave_counts = []
            full_text = buffered + "\n" + aggregation
            return TurnOutcome(final_text=full_text, tool_calls=[])

        return TurnOutcome(final_text="", tool_calls=[])
