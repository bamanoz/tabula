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
MAX_WAIT_SEC = 300   # max wait for subagent results (5 min)


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
        self._pid_to_agent_id: dict[int, str] = {}    # PID → subagent ID mapping
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

    def call_api_streaming(self, suppress_stream=False):
        """Call Claude streaming API. Streams text deltas to kernel,
        accumulates tool_use blocks. Returns assistant content blocks.
        If suppress_stream=True, text is accumulated but not sent to kernel."""

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
                        if not suppress_stream:
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
            # Don't send done yet — caller may enter collection mode
            pass
        else:
            # Track which tool_results we need before next API call
            self._expected_tool_ids = [t["id"] for t in tool_uses]
            self._tool_results_buf = []

        return content_blocks, tool_uses

    def process_turn(self, suppress_stream=False):
        """Run one LLM turn: call API, stream response, handle tool loop.
        If suppress_stream=True, text is not streamed to gateway (used for
        tool-continuation turns and collection-flush turns). The final text-only
        response is retroactively streamed before sending done."""
        self.aborted = False

        try:
            content_blocks, tool_uses = self.call_api_streaming(suppress_stream=suppress_stream)
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
            # Don't send done — we're still working (waiting for subagents)
        elif not tool_uses:
            # LLM responded with text and no tools — turn is done
            if self.collecting:
                log(f"LLM finished but {len(self._pending_ids)} subagents still pending — giving up collection")
                self._pending_ids.clear()
                self._collected = []
            # If streaming was suppressed, retroactively stream the final text
            if suppress_stream:
                final_text = "\n".join(
                    b["text"] for b in content_blocks if b.get("type") == "text" and b.get("text")
                )
                if final_text.strip():
                    self.conn.send({"type": "stream_start"})
                    self.conn.send({"type": "stream_delta", "text": final_text})
                    self.conn.send({"type": "stream_end"})
            self.conn.send({"type": "done"})

        # If there were tool uses, wait for tool_results
        # (kernel will send them, and our main loop dispatches handle_tool_result)

    def _extract_spawn_ids(self, tool_uses: list[dict]) -> set[str]:
        """Extract subagent --id values from SPAWN tool_use commands."""
        ids = set()
        for tool in tool_uses:
            if tool.get("name") != "SPAWN":
                continue
            cmd = tool.get("input", {}).get("command", "")
            # Match --id VALUE or --id=VALUE, with optional quotes
            m = re.search(r"--id[\s=]+['\"]?(\S+?)['\"]?(?:\s|$)", cmd)
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
        log(f"message: id='{msg_id}' collecting={self.collecting} pending={self._pending_ids} text={text[:100]}")

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

        # Track PID → agent_id mapping from SPAWN results ("PID 12345")
        import re
        pid_match = re.match(r"PID (\d+)", output)
        if pid_match:
            pid = int(pid_match.group(1))
            # Find the SPAWN tool_use that produced this to get the agent --id
            for t in (self.messages[-1].get("content", []) if self.messages else []):
                if isinstance(t, dict) and t.get("id") == tool_id and t.get("name") == "SPAWN":
                    cmd = t.get("input", {}).get("command", "")
                    id_match = re.search(r"--id\s+(\S+)", cmd)
                    if id_match:
                        agent_id = id_match.group(1).strip("'\"")
                        self._pid_to_agent_id[pid] = agent_id
                        log(f"mapped PID {pid} → agent {agent_id}")
                    break

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
        # Suppress streaming for tool-continuation turns to avoid repeating reports
        self.messages.append({"role": "user", "content": self._tool_results_buf})
        self._expected_tool_ids = []
        self._tool_results_buf = []
        self.process_turn(suppress_stream=True)

    def handle_error(self, msg: dict):
        text = msg.get("text", "unknown error")
        log(f"error: {text}")

        # If a subagent crashed during collection, mark it as done with error
        if self.collecting:
            import re
            m = re.search(r"process (\d+) crashed", text)
            if m:
                pid = int(m.group(1))
                agent_id = self._pid_to_agent_id.get(pid)
                if agent_id and agent_id in self._pending_ids:
                    self._collected.append({"id": agent_id, "text": f"[subagent crashed: {text}]"})
                    log(f"subagent {agent_id} (PID {pid}) crashed, marked as collected")
                    return
            # Unknown crash during collection — don't break the loop
            return

        self.messages.append({"role": "user", "content": f"[system error: {text}]"})
        self.process_turn()

    def run(self):
        while True:
            # Process pending LLM turn from flush
            if self._needs_turn:
                self._needs_turn = False
                # Suppress streaming for collection-flush turns — if the LLM spawns more
                # agents, we don't want to stream intermediate reports. The final text-only
                # response will be retroactively streamed before done.
                self.process_turn(suppress_stream=True)
                continue

            # In collection mode: wait for ALL results or max timeout
            if self.collecting:
                elapsed = time.time() - self._collect_start

                # Max wait exceeded — flush what we have
                if elapsed >= MAX_WAIT_SEC:
                    log(f"max wait {MAX_WAIT_SEC}s reached, flushing")
                    self._flush_collected()
                    continue

                # All results in — flush immediately
                all_in = self._collected and not (self._pending_ids - {r["id"] for r in self._collected})
                if all_in:
                    log("all expected results collected, flushing")
                    self._flush_collected()
                    continue

                # Poll with timeout
                timeout = min(MAX_WAIT_SEC - elapsed, 5.0)
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
                self.handle_message(msg)
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
