#!/usr/bin/env python3
"""
Tabula LLM Driver — Anthropic adapter.

Connects to kernel via Unix socket (TABULA_SOCKET env var).
Translates between Anthropic streaming API and kernel protocol.
"""

import json
import os
import re
import signal
import sys
import time
import urllib.request
import urllib.error

import websocket as ws_client

BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_URL = f"{BASE_URL}/v1/messages"
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
MODEL = os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-6")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")


VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"

# Subagent result batching
DEBOUNCE_SEC = 5     # wait this long after last result before sending batch
MAX_WAIT_SEC = 300   # never wait longer than this total (5 min)


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[driver] {msg}\n")
        sys.stderr.flush()


class KernelConnection:
    """WebSocket connection to kernel."""

    def __init__(self, url: str):
        self.ws = ws_client.create_connection(url)

    def send(self, msg: dict):
        self.ws.send(json.dumps(msg, ensure_ascii=False))

    def recv(self, timeout: float | None = None) -> dict | None:
        """Receive one message. Returns None on disconnect, raises TimeoutError on timeout."""
        if timeout is not None:
            self.ws.settimeout(timeout)
        else:
            self.ws.settimeout(None)
        try:
            data = self.ws.recv()
            if not data:
                return None
            return json.loads(data)
        except ws_client.WebSocketTimeoutException:
            raise TimeoutError()
        except (ws_client.WebSocketConnectionClosedException, ConnectionError):
            return None

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


def kernel_to_anthropic_tools(kernel_tools: list[dict]) -> list[dict]:
    """Convert kernel-neutral tool definitions to Anthropic API format."""
    result = []
    for tool in kernel_tools:
        result.append({
            "name": tool["name"],
            "description": tool["description"],
            "input_schema": {
                "type": "object",
                "properties": {
                    k: {"type": v["type"], "description": v["description"]}
                    for k, v in tool.get("params", {}).items()
                },
                "required": tool.get("required", []),
            },
        })
    return result


class Driver:
    def __init__(self):
        self.conn = KernelConnection(TABULA_URL)
        self.system_prompt = ""
        self.tools = []
        self.messages: list[dict] = []
        self.aborted = False
        self._current_resp = None  # active HTTP response, for cancel
        # Tool result batching: collect all results before next API call
        self._expected_tool_ids: list[str] = []   # tool_use IDs from last turn
        self._tool_results_buf: list[dict] = []   # buffered tool_result blocks
        # Subagent result collection mode
        self._pending_ids: set[str] = set()    # subagent IDs we're waiting for
        self._collected: list[dict] = []        # buffered results {id, text}
        self._collect_start: float = 0          # when collection started
        self._spawn_ids_this_convo: set[str] = set()  # accumulated across tool turns
        self._needs_turn: bool = False  # flag to trigger process_turn from run loop

    def connect(self):
        self.conn.send({
            "type": "connect",
            "name": "anthropic",
            "sends": ["stream_start", "stream_delta", "stream_end", "tool_use", "done"],
            "receives": ["message", "tool_result", "init", "error"],
        })
        resp = self.conn.recv()
        log(f"connected: {resp}")

        self.conn.send({"type": "join", "session": "main"})
        resp = self.conn.recv()
        log(f"joined: {resp}")

    def call_api_streaming(self):
        """Call Claude streaming API. Streams text deltas to kernel,
        accumulates tool_use blocks. Returns assistant content blocks."""

        body = json.dumps({
            "model": MODEL,
            "max_tokens": 4096,
            "system": self.system_prompt,
            "messages": self.messages,
            "tools": self.tools if self.tools else None,
            "stream": True,
        }).encode()

        req = urllib.request.Request(
            API_URL,
            data=body,
            headers={
                "Content-Type": "application/json",
                "x-api-key": API_KEY,
                "anthropic-version": "2023-06-01",
            },
        )

        resp = urllib.request.urlopen(req)
        self._current_resp = resp

        content_blocks = []  # for history
        tool_uses = []  # tool_use blocks to send to kernel
        current_text = ""
        current_tool = None
        stream_started = False

        try:
            for raw_line in resp:
                if self.aborted:
                    break

                line = raw_line.decode().strip()
                if not line.startswith("data: "):
                    continue

                data = json.loads(line[6:])
                event_type = data.get("type")

                if event_type == "content_block_start":
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
                        text = delta["text"]
                        current_text += text
                        if not stream_started:
                            self.conn.send({"type": "stream_start"})
                            stream_started = True
                        self.conn.send({"type": "stream_delta", "text": text})
                    elif delta["type"] == "input_json_delta" and current_tool:
                        current_tool["input_json"] += delta["partial_json"]

                elif event_type == "content_block_stop":
                    if current_tool:
                        try:
                            input_data = json.loads(current_tool["input_json"]) if current_tool["input_json"] else {}
                        except json.JSONDecodeError:
                            input_data = {}
                        tool_block = {
                            "type": "tool_use",
                            "id": current_tool["id"],
                            "name": current_tool["name"],
                            "input": input_data,
                        }
                        content_blocks.append(tool_block)
                        tool_uses.append(tool_block)
                        current_tool = None
                    elif current_text:
                        content_blocks.append({"type": "text", "text": current_text})
                        current_text = ""

                elif event_type == "message_delta":
                    pass  # stop_reason handled implicitly

        except Exception:
            # resp.close() from SIGINT handler can break the iterator — that's ok
            if not self.aborted:
                raise

        finally:
            self._current_resp = None
            try:
                resp.close()
            except Exception:
                pass

        if stream_started:
            self.conn.send({"type": "stream_end"})

        # Send tool_use messages to kernel
        for tool in tool_uses:
            self.conn.send({
                "type": "tool_use",
                "id": tool["id"],
                "name": tool["name"],
                "input": tool["input"],
            })

        # Only send done if no tool calls — otherwise the turn continues
        if not tool_uses:
            self.conn.send({"type": "done"})
        else:
            # Track which tool_results we need before next API call
            self._expected_tool_ids = [t["id"] for t in tool_uses]
            self._tool_results_buf = []

        return content_blocks, tool_uses

    def process_turn(self):
        """Run one LLM turn: call API, stream response, handle tool loop."""
        self.aborted = False

        try:
            content_blocks, tool_uses = self.call_api_streaming()
        except Exception as e:
            log(f"API error: {e}")
            self.conn.send({"type": "stream_start"})
            self.conn.send({"type": "stream_delta", "text": f"[API Error: {e}]"})
            self.conn.send({"type": "stream_end"})
            self.conn.send({"type": "done"})
            return

        # If aborted, add a minimal assistant message to keep history valid
        if self.aborted:
            if content_blocks:
                # Keep only complete text blocks
                valid = [b for b in content_blocks if b.get("type") == "text" and b.get("text")]
                if valid:
                    self.messages.append({"role": "assistant", "content": valid})
                else:
                    self.messages.append({"role": "assistant", "content": [{"type": "text", "text": "[cancelled]"}]})
            else:
                self.messages.append({"role": "assistant", "content": [{"type": "text", "text": "[cancelled]"}]})
            return

        # Add assistant message to history
        self.messages.append({"role": "assistant", "content": content_blocks})

        # Track SPAWN IDs across tool turns
        spawn_ids = self._extract_spawn_ids(tool_uses)
        if spawn_ids:
            self._spawn_ids_this_convo.update(spawn_ids)
            log(f"detected SPAWN ids: {spawn_ids}")

        # When LLM finishes (no more tool calls) and we had SPAWNs — enter collection mode
        if not tool_uses and self._spawn_ids_this_convo:
            self._pending_ids = self._spawn_ids_this_convo.copy()
            self._spawn_ids_this_convo.clear()
            self._collected = []
            self._collect_start = time.time()
            log(f"collection mode: waiting for {self._pending_ids}")

        # If there were tool uses, wait for tool_results
        # (kernel will send them, and our main loop dispatches handle_tool_result)

    def _extract_spawn_ids(self, tool_uses: list[dict]) -> set[str]:
        """Extract subagent --id values from SPAWN tool_use commands."""
        ids = set()
        for tool in tool_uses:
            if tool.get("name") != "SPAWN":
                continue
            cmd = tool.get("input", {}).get("command", "")
            m = re.search(r"--id\s+(\S+)", cmd)
            if m:
                ids.add(m.group(1))
        return ids

    def _flush_collected(self):
        """Send collected subagent results as a single user message and trigger LLM turn."""
        if not self._collected:
            # Nothing to flush — keep waiting or give up
            if self._pending_ids:
                log(f"flush called with 0 results, giving up on {self._pending_ids}")
            self._pending_ids.clear()
            return

        parts = []
        collected_ids = set()
        for r in self._collected:
            parts.append(f"[Result from subagent {r['id']}]:\n{r['text']}")
            collected_ids.add(r["id"])

        remaining = self._pending_ids - collected_ids
        if remaining:
            parts.append(f"[Still waiting for subagents: {', '.join(sorted(remaining))}]")

        batch_text = "\n\n---\n\n".join(parts)
        log(f"flushing {len(self._collected)} results, {len(remaining)} still pending")

        self._collected = []
        self._pending_ids = remaining
        if remaining:
            self._collect_start = time.time()

        self.messages.append({"role": "user", "content": batch_text})
        # Don't call process_turn here — let run() loop handle it
        self._needs_turn = True

    @property
    def collecting(self) -> bool:
        return bool(self._pending_ids)

    def handle_init(self, msg: dict):
        self.system_prompt = msg.get("prompt", "")
        kernel_tools = msg.get("tools", [])
        self.tools = kernel_to_anthropic_tools(kernel_tools)
        log(f"init: prompt={len(self.system_prompt)} chars, tools={len(self.tools)}")

    def handle_message(self, msg: dict):
        text = msg.get("text", "")
        msg_id = msg.get("id", "")
        log(f"message: id={msg_id} {text[:200]}")

        # If we're in collection mode and this is from a subagent we're waiting for
        if self.collecting and msg_id and msg_id in self._pending_ids:
            self._collected.append({"id": msg_id, "text": text})
            log(f"collected result from {msg_id} ({len(self._collected)}/{len(self._pending_ids)})")
            # Don't process now — the run loop handles debounce/flush
            return

        # Normal message (user input or unexpected subagent)
        self.messages.append({"role": "user", "content": text})
        self.process_turn()

    def handle_tool_result(self, msg: dict):
        tool_id = msg.get("id", "")
        output = msg.get("output", "")
        log(f"tool_result: id={tool_id}")

        self._tool_results_buf.append({
            "type": "tool_result",
            "tool_use_id": tool_id,
            "content": output,
        })

        # Only proceed when ALL expected tool_results have arrived
        received_ids = {r["tool_use_id"] for r in self._tool_results_buf}
        if not all(tid in received_ids for tid in self._expected_tool_ids):
            log(f"waiting for more tool_results ({len(self._tool_results_buf)}/{len(self._expected_tool_ids)})")
            return

        # All results in — send as single user message and continue
        self.messages.append({"role": "user", "content": self._tool_results_buf})
        self._expected_tool_ids = []
        self._tool_results_buf = []
        self.process_turn()

    def handle_error(self, msg: dict):
        text = msg.get("text", "unknown error")
        log(f"error: {text}")
        self.messages.append({"role": "user", "content": f"[system error: {text}]"})
        self.process_turn()

    def run(self):
        last_result_time = 0.0

        while True:
            # Process pending LLM turn from flush
            if self._needs_turn:
                self._needs_turn = False
                self.process_turn()
                continue

            # In collection mode: use short timeout for debounce
            if self.collecting:
                elapsed = time.time() - self._collect_start
                time_since_last = time.time() - last_result_time if last_result_time else elapsed

                # Max wait exceeded — flush what we have
                if elapsed >= MAX_WAIT_SEC:
                    log(f"max wait {MAX_WAIT_SEC}s reached, flushing")
                    self._flush_collected()
                    last_result_time = 0.0
                    continue

                # Debounce: all results in, or N seconds of silence
                all_in = self._collected and not (self._pending_ids - {r["id"] for r in self._collected})
                if all_in:
                    log("all expected results collected, flushing")
                    self._flush_collected()
                    last_result_time = 0.0
                    continue

                if self._collected and time_since_last >= DEBOUNCE_SEC:
                    log(f"debounce {DEBOUNCE_SEC}s reached, flushing")
                    self._flush_collected()
                    last_result_time = 0.0
                    continue

                # Poll with short timeout
                timeout = min(DEBOUNCE_SEC, MAX_WAIT_SEC - elapsed, 1.0)
            else:
                timeout = None  # block forever

            try:
                msg = self.conn.recv(timeout=timeout)
            except TimeoutError:
                continue  # loop back to check debounce/max_wait
            if msg is None:
                log("connection closed")
                break

            msg_type = msg.get("type")
            if msg_type == "init":
                self.handle_init(msg)
            elif msg_type == "message":
                was_collecting = self.collecting
                self.handle_message(msg)
                if was_collecting and self.collecting:
                    last_result_time = time.time()
            elif msg_type == "tool_result":
                self.handle_tool_result(msg)
            elif msg_type == "error":
                self.handle_error(msg)
            else:
                log(f"ignoring: {msg_type}")


def main():
    if not API_KEY:
        log("ERROR: ANTHROPIC_API_KEY not set")
        sys.exit(1)

    driver = Driver()

    def handle_sigint(sig, frame):
        log("SIGINT — aborting stream")
        driver.aborted = True
        # Close the HTTP response to unblock the read
        resp = driver._current_resp
        if resp:
            try:
                resp.close()
            except Exception:
                pass

    signal.signal(signal.SIGINT, handle_sigint)

    log(f"connecting to {TABULA_URL}")
    driver.connect()
    driver.run()


if __name__ == "__main__":
    main()
