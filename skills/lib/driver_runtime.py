#!/usr/bin/env python3
"""Shared runtime for main LLM drivers."""

from __future__ import annotations

import json
import os
import re
import time
from dataclasses import dataclass

from .kernel_client import KernelConnection
from .providers import ProviderSession, ToolCall, ToolResult


MAX_WAIT_SEC = 300


@dataclass
class DriverConfig:
    name: str
    url: str
    session: str = "main"


def extract_spawn_id(command: str) -> str | None:
    match = re.search(r"--id(?:\s+|=)['\"]?([A-Za-z0-9_.-]+)['\"]?(?:\s|$)", command)
    return match.group(1) if match else None


class DriverRuntime:
    def __init__(self, config: DriverConfig, provider_factory, logger):
        self.config = config
        self.provider_factory = provider_factory
        self.log = logger
        self.conn = KernelConnection(config.url)
        self.provider: ProviderSession | None = None
        self.aborted = False

        self._expected_tool_ids: list[str] = []
        self._tool_results_buf: list[ToolResult] = []

        self._pending_ids: set[str] = set()
        self._collected: list[dict] = []
        self._collect_start = 0.0
        self._spawn_ids_this_turn: set[str] = set()
        self._known_subagent_ids: set[str] = set()
        self._early_results: list[dict] = []
        self._needs_turn = False

        self._tool_to_spawn_id: dict[str, str] = {}
        self._pid_to_agent_id: dict[int, str] = {}

        # History persistence
        self._history_file = None
        tabula_home = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
        history_dir = os.path.join(tabula_home, "sessions", config.session)
        try:
            os.makedirs(history_dir, exist_ok=True)
            self._history_file = open(os.path.join(history_dir, "history.jsonl"), "a")
        except OSError as e:
            logger(f"cannot open history file: {e}")

    @property
    def collecting(self) -> bool:
        return bool(self._pending_ids)

    def _write_history(self, record: dict):
        if not self._history_file:
            return
        record["ts"] = time.time()
        try:
            self._history_file.write(json.dumps(record, ensure_ascii=False) + "\n")
            self._history_file.flush()
        except OSError:
            pass

    def abort(self):
        self.aborted = True
        if self.provider:
            self.provider.abort()

    def connect(self):
        self.conn.send(
            {
                "type": "connect",
                "name": self.config.name,
                "sends": ["stream_start", "stream_delta", "stream_end", "tool_use", "done"],
                "receives": ["message", "tool_result", "init", "error"],
            }
        )
        self.conn.recv()
        self.conn.send({"type": "join", "session": self.config.session})
        self.conn.recv()

    def process_turn(self, suppress_stream: bool = False):
        if not self.provider:
            return

        self.aborted = False
        stream_started = False

        def on_text_delta(text: str):
            nonlocal stream_started
            if suppress_stream:
                return
            if not stream_started:
                self.conn.send({"type": "stream_start"})
                stream_started = True
            self.conn.send({"type": "stream_delta", "text": text})

        try:
            outcome = self.provider.generate(on_text_delta)
        except Exception as exc:
            self.log(f"API error: {exc}")
            if self.aborted:
                self.provider.record_aborted_turn()
                return
            if self.collecting:
                self._collect_start = time.time()
                return
            self.conn.send({"type": "stream_start"})
            self.conn.send({"type": "stream_delta", "text": f"<error>{exc}</error>"})
            self.conn.send({"type": "stream_end"})
            self.conn.send({"type": "done"})
            return
        finally:
            if stream_started:
                self.conn.send({"type": "stream_end"})

        if self.aborted:
            self.provider.record_aborted_turn()
            return

        # Record assistant output to history
        if outcome.final_text.strip():
            self._write_history({"role": "assistant", "text": outcome.final_text})
        for tool in outcome.tool_calls:
            self._write_history({"role": "assistant", "tool_use": {"id": tool.id, "name": tool.name, "input": tool.input}})

        spawn_ids = set()
        for tool in outcome.tool_calls:
            if tool.name == "SPAWN":
                spawn_id = extract_spawn_id(tool.input.get("command", ""))
                if spawn_id:
                    spawn_ids.add(spawn_id)
                    self._tool_to_spawn_id[tool.id] = spawn_id

        if spawn_ids:
            self._spawn_ids_this_turn.update(spawn_ids)
            self._known_subagent_ids.update(spawn_ids)
            self.log(f"detected SPAWN ids: {sorted(spawn_ids)}")

        if outcome.tool_calls:
            self._expected_tool_ids = [tool.id for tool in outcome.tool_calls]
            self._tool_results_buf = []
            for tool in outcome.tool_calls:
                self.conn.send(
                    {
                        "type": "tool_use",
                        "id": tool.id,
                        "name": tool.name,
                        "input": tool.input,
                    }
                )
            return

        if self._spawn_ids_this_turn:
            self._pending_ids = set(self._spawn_ids_this_turn)
            self._spawn_ids_this_turn.clear()
            self._collected = list(self._early_results)
            self._early_results = []
            self._collect_start = time.time()
            self.log(f"collection mode: waiting for {sorted(self._pending_ids)}")
            return

        if self.collecting:
            self._collect_start = time.time()
            return

        if suppress_stream and outcome.final_text.strip():
            self.conn.send({"type": "stream_start"})
            self.conn.send({"type": "stream_delta", "text": outcome.final_text})
            self.conn.send({"type": "stream_end"})

        self.conn.send({"type": "done"})

    def _flush_collected(self):
        if not self.provider:
            return
        self._flush_collected_internal(timeout=False)

    def _flush_collected_internal(self, *, timeout: bool):
        if not self.provider:
            return

        parts = []
        collected_ids = set()
        for result in self._collected:
            parts.append(f"<subagent_result id=\"{result['id']}\">\n{result['text']}\n</subagent_result>")
            collected_ids.add(result["id"])

        remaining = self._pending_ids - collected_ids
        if timeout and remaining:
            for agent_id in sorted(remaining):
                parts.append(f"<subagent_result id=\"{agent_id}\">\n<error>timed out</error>\n</subagent_result>")
            collected_ids.update(remaining)
            remaining = set()
        elif remaining:
            parts.append(f"<subagent_pending ids=\"{', '.join(sorted(remaining))}\" />")

        self._collected = []
        self._pending_ids = remaining
        self._known_subagent_ids -= collected_ids
        if remaining:
            self._collect_start = time.time()

        if not parts:
            return

        self.provider.add_user_text("\n\n---\n\n".join(parts))
        self._needs_turn = True

    def handle_init(self, msg: dict):
        prompt = msg.get("prompt", "")
        # Inject session identity so LLM uses correct --parent-session
        prompt += f"\n\nYour session name is `{self.config.session}`."
        self.provider = self.provider_factory(prompt, msg.get("tools", []))
        self._restore_history()
        self.log("provider initialized")

    def _restore_history(self):
        """Load history.jsonl and replay into provider for session resume."""
        if not self.provider or not self._history_file:
            return
        history_path = self._history_file.name
        if not os.path.isfile(history_path):
            return
        entries = []
        try:
            with open(history_path) as f:
                for line in f:
                    line = line.strip()
                    if line:
                        entries.append(json.loads(line))
        except (OSError, json.JSONDecodeError) as e:
            self.log(f"failed to read history: {e}")
            return
        if entries:
            self.provider.restore_history(entries)
            self.log(f"restored {len(entries)} history entries")

    def handle_message(self, msg: dict):
        if not self.provider:
            return

        text = msg.get("text", "")
        msg_id = msg.get("id", "")
        from_session = msg.get("from_session", "")

        if msg_id and msg_id in self._known_subagent_ids:
            self._record_subagent_result(msg_id, text)
            return

        if from_session:
            text = f"<cross_session from=\"{from_session}\">\n{text}\n</cross_session>"

        self._write_history({"role": "user", "text": text})
        self.provider.add_user_text(text)
        self.process_turn()

    def _record_subagent_result(self, agent_id: str, text: str):
        target = self._collected if self.collecting else self._early_results
        if any(result["id"] == agent_id for result in target):
            return
        target.append({"id": agent_id, "text": text})

    def handle_tool_result(self, msg: dict):
        if not self.provider:
            return

        tool_id = msg.get("id", "")
        output = msg.get("output", "")

        self._write_history({"role": "tool", "tool_use_id": tool_id, "output": output})

        pid_match = re.match(r"PID (\d+)", output)
        if pid_match and tool_id in self._tool_to_spawn_id:
            self._pid_to_agent_id[int(pid_match.group(1))] = self._tool_to_spawn_id[tool_id]
        elif not pid_match and tool_id in self._tool_to_spawn_id:
            # SPAWN failed — record as failed subagent result so collection doesn't hang
            agent_id = self._tool_to_spawn_id[tool_id]
            self._record_subagent_result(agent_id, f"<error>spawn failed via {tool_id}: {output}</error>")

        self._tool_results_buf.append(ToolResult(tool_use_id=tool_id, output=output))
        received_ids = {result.tool_use_id for result in self._tool_results_buf}
        if not all(tool_id in received_ids for tool_id in self._expected_tool_ids):
            return

        self.provider.add_tool_results(self._tool_results_buf)
        self._expected_tool_ids = []
        self._tool_results_buf = []
        self.process_turn(suppress_stream=True)

    def handle_error(self, msg: dict):
        if not self.provider:
            return

        text = msg.get("text", "unknown error")
        match = re.search(r"process (\d+) crashed", text)
        if match:
            pid = int(match.group(1))
            agent_id = self._pid_to_agent_id.get(pid)
            if agent_id and agent_id in self._known_subagent_ids:
                self._record_subagent_result(agent_id, f"<error>crashed: {text}</error>")
                return
        if self.collecting:
            return

        self.provider.add_user_text(f"<system_error>{text}</system_error>")
        self.process_turn()

    def run(self):
        while True:
            if self._needs_turn:
                self._needs_turn = False
                self.process_turn(suppress_stream=True)
                continue

            if self.collecting:
                elapsed = time.time() - self._collect_start
                if elapsed >= MAX_WAIT_SEC:
                    self._flush_collected_internal(timeout=True)
                    continue
                all_in = self._collected and not (self._pending_ids - {result["id"] for result in self._collected})
                if all_in:
                    self._flush_collected()
                    continue
                timeout = min(MAX_WAIT_SEC - elapsed, 5.0)
            else:
                timeout = None

            try:
                msg = self.conn.recv(timeout=timeout)
            except TimeoutError:
                continue

            if msg is None:
                break

            msg_type = msg.get("type")
            if msg_type == "init":
                self.handle_init(msg)
            elif msg_type == "message":
                self.handle_message(msg)
            elif msg_type == "tool_result":
                self.handle_tool_result(msg)
            elif msg_type == "error":
                self.handle_error(msg)
