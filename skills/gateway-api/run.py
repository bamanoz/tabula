#!/usr/bin/env python3
"""OpenAI-compatible HTTP API gateway for Tabula."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import queue
import re
import sys
import threading
import time
from http.server import HTTPServer, BaseHTTPRequestHandler
from uuid import uuid4

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_HOME = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
AUTH_TOKEN = os.environ.get("TABULA_API_AUTH", "")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"
VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
PROVIDER = os.environ.get("TABULA_PROVIDER", "anthropic")

# Provider alias resolution (same as boot.py)
PROVIDER_ALIASES = {
    "anthropic": "anthropic",
    "claude": "anthropic",
    "openai": "openai",
    "gpt": "openai",
    "openclaw": "openai",
}
ACTIVE_PROVIDER = PROVIDER_ALIASES.get(PROVIDER, PROVIDER)


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[gateway-api] {msg}\n")
        sys.stderr.flush()


class SessionState:
    """Tracks a kernel session with its connection, receiver thread, and driver."""

    def __init__(self, session_id: str):
        self.session_id = session_id
        self.conn = KernelConnection(TABULA_URL)
        self.driver_pid: int | None = None
        self.events: queue.Queue[tuple[str, str]] = queue.Queue()
        self.alive = True
        self._receiver_thread: threading.Thread | None = None

    def connect(self, driver_cmd: str):
        """Connect to kernel, join session, spawn driver."""
        self.conn.send({
            "type": "connect",
            "name": f"api-{self.session_id}",
            "sends": ["message", "cancel", "tool_use"],
            "receives": ["stream_start", "stream_delta", "stream_end", "done", "error", "tool_result", "member_joined"],
        })
        self.conn.recv()
        self.conn.send({"type": "join", "session": self.session_id})
        self.conn.recv()

        # Spawn driver
        spawn_cmd = f"{driver_cmd} --session {self.session_id}"
        self.conn.send({
            "type": "tool_use",
            "id": "spawn-driver",
            "name": "SPAWN",
            "input": {"command": spawn_cmd},
        })
        deadline = time.time() + 15
        while time.time() < deadline:
            msg = self.conn.recv(timeout=15)
            if msg is None:
                raise RuntimeError("lost connection while spawning driver")
            if msg.get("type") == "tool_result" and msg.get("id") == "spawn-driver":
                output = msg.get("output", "")
                m = re.match(r"PID (\d+)", output)
                if m:
                    self.driver_pid = int(m.group(1))
                    break
                raise RuntimeError(f"driver spawn failed: {output}")

        # Wait for driver to join the session
        deadline = time.time() + 10
        while time.time() < deadline:
            msg = self.conn.recv(timeout=10)
            if msg is None:
                raise RuntimeError("lost connection waiting for driver")
            if msg.get("type") == "member_joined":
                break

        # Start receiver thread
        self._receiver_thread = threading.Thread(target=self._receiver, daemon=True)
        self._receiver_thread.start()

    def _receiver(self):
        while self.alive:
            msg = self.conn.recv()
            if msg is None:
                self.events.put(("disconnect", ""))
                return
            msg_type = msg.get("type")
            if msg_type in ("stream_start", "stream_delta", "stream_end", "done", "error"):
                payload = msg.get("text", "")
                self.events.put((msg_type, payload))

    def close(self):
        self.alive = False
        if self.driver_pid is not None:
            try:
                self.conn.send({
                    "type": "tool_use",
                    "id": "kill-driver",
                    "name": "KILL",
                    "input": {"pid": self.driver_pid},
                })
            except Exception:
                pass
        self.conn.close()


class GatewayAPI:
    """Manages sessions and provides the HTTP handler."""

    def __init__(self):
        self.sessions: dict[str, SessionState] = {}
        self._lock = threading.Lock()
        self.driver_cmd = f"{VENV_PYTHON} skills/driver-{ACTIVE_PROVIDER}/run.py"

    def get_or_create_session(self, session_id: str) -> SessionState:
        with self._lock:
            if session_id in self.sessions:
                return self.sessions[session_id]
            state = SessionState(session_id)
            state.connect(self.driver_cmd)
            self.sessions[session_id] = state
            log(f"created session {session_id}")
            return state

    def resolve_session_id(self, request_body: dict, headers: dict) -> str:
        """Determine session ID from request."""
        # Explicit header takes priority
        sid = headers.get("x-session-id", "")
        if sid:
            return sid
        # Derive from user field (deterministic)
        user = request_body.get("user", "")
        if user:
            h = hashlib.sha256(user.encode()).hexdigest()[:8]
            return f"sess-{h}"
        # New session
        return f"sess-{uuid4().hex[:8]}"


def make_handler(gateway: GatewayAPI):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            if VERBOSE:
                sys.stderr.write(f"[gateway-api] {format % args}\n")

        def _check_auth(self) -> bool:
            if not AUTH_TOKEN:
                return True
            auth = self.headers.get("Authorization", "")
            if auth == f"Bearer {AUTH_TOKEN}":
                return True
            self.send_error(401, "Unauthorized")
            return False

        def _read_body(self) -> dict | None:
            length = int(self.headers.get("Content-Length", 0))
            if length == 0:
                self.send_error(400, "Empty body")
                return None
            try:
                return json.loads(self.rfile.read(length))
            except (json.JSONDecodeError, ValueError):
                self.send_error(400, "Invalid JSON")
                return None

        def do_POST(self):
            if self.path == "/v1/chat/completions":
                self._handle_chat_completions()
            elif self.path == "/v1/responses":
                self._handle_responses()
            else:
                self.send_error(404, "Not found")

        def _handle_chat_completions(self):
            if not self._check_auth():
                return

            body = self._read_body()
            if body is None:
                return

            messages = body.get("messages", [])
            if not messages:
                self.send_error(400, "No messages")
                return

            # Extract last user message
            user_text = ""
            for msg in reversed(messages):
                if msg.get("role") == "user":
                    user_text = msg.get("content", "")
                    break
            if not user_text:
                self.send_error(400, "No user message")
                return

            stream = body.get("stream", False)
            headers_dict = {k.lower(): v for k, v in self.headers.items()}
            session_id = gateway.resolve_session_id(body, headers_dict)

            try:
                session = gateway.get_or_create_session(session_id)
            except RuntimeError as e:
                self.send_error(503, str(e))
                return

            self._drain_events(session)
            session.conn.send({"type": "message", "text": user_text})
            completion_id = f"chatcmpl-{uuid4().hex[:12]}"

            if stream:
                self._handle_stream(session, completion_id)
            else:
                self._handle_sync(session, completion_id)

        def _handle_responses(self):
            if not self._check_auth():
                return

            body = self._read_body()
            if body is None:
                return

            # Extract user text from input (string or array of items)
            inp = body.get("input", "")
            user_text = ""
            if isinstance(inp, str):
                user_text = inp
            elif isinstance(inp, list):
                for item in reversed(inp):
                    if isinstance(item, dict) and item.get("role") == "user":
                        content = item.get("content", "")
                        if isinstance(content, str):
                            user_text = content
                        elif isinstance(content, list):
                            for part in content:
                                if isinstance(part, dict) and part.get("type") in ("input_text", "text"):
                                    user_text = part.get("text", "")
                                    break
                        break
            if not user_text:
                self._send_json_error(400, "invalid_request_error", "Missing user message in `input`.")
                return

            stream = body.get("stream", False)
            headers_dict = {k.lower(): v for k, v in self.headers.items()}
            session_id = gateway.resolve_session_id(body, headers_dict)

            try:
                session = gateway.get_or_create_session(session_id)
            except RuntimeError as e:
                self._send_json_error(503, "api_error", str(e))
                return

            self._drain_events(session)
            session.conn.send({"type": "message", "text": user_text})

            resp_id = f"resp_{uuid4().hex[:12]}"
            msg_id = f"msg_{uuid4().hex[:12]}"

            if stream:
                self._handle_responses_stream(session, resp_id, msg_id)
            else:
                self._handle_responses_sync(session, resp_id, msg_id)

        def _send_json_error(self, code: int, error_type: str, message: str):
            body = json.dumps({"error": {"type": error_type, "message": message}}).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def _drain_events(self, session: SessionState):
            while True:
                try:
                    session.events.get_nowait()
                except queue.Empty:
                    break

        def _handle_stream(self, session: SessionState, completion_id: str):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.end_headers()

            # Initial role chunk
            chunk = {
                "id": completion_id,
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": "tabula",
                "choices": [{"delta": {"role": "assistant"}, "index": 0, "finish_reason": None}],
            }
            self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
            self.wfile.flush()

            while True:
                try:
                    kind, payload = session.events.get(timeout=300)
                except queue.Empty:
                    break

                if kind == "stream_delta":
                    chunk = {
                        "id": completion_id,
                        "object": "chat.completion.chunk",
                        "created": int(time.time()),
                        "model": "tabula",
                        "choices": [{"delta": {"content": payload}, "index": 0, "finish_reason": None}],
                    }
                    self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
                    self.wfile.flush()
                elif kind == "done":
                    # Final chunk with finish_reason
                    chunk = {
                        "id": completion_id,
                        "object": "chat.completion.chunk",
                        "created": int(time.time()),
                        "model": "tabula",
                        "choices": [{"delta": {}, "index": 0, "finish_reason": "stop"}],
                    }
                    self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                    break
                elif kind == "error":
                    chunk = {
                        "id": completion_id,
                        "object": "chat.completion.chunk",
                        "created": int(time.time()),
                        "model": "tabula",
                        "choices": [{"delta": {"content": f"\n[error: {payload}]"}, "index": 0, "finish_reason": "stop"}],
                    }
                    self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                    break
                elif kind == "disconnect":
                    break

        def _handle_sync(self, session: SessionState, completion_id: str):
            text_parts = []
            while True:
                try:
                    kind, payload = session.events.get(timeout=300)
                except queue.Empty:
                    break

                if kind == "stream_delta":
                    text_parts.append(payload)
                elif kind == "done":
                    break
                elif kind == "error":
                    text_parts.append(f"\n[error: {payload}]")
                    break
                elif kind == "disconnect":
                    break

            response = {
                "id": completion_id,
                "object": "chat.completion",
                "created": int(time.time()),
                "model": "tabula",
                "choices": [{
                    "message": {"role": "assistant", "content": "".join(text_parts)},
                    "finish_reason": "stop",
                    "index": 0,
                }],
                "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
            }
            body = json.dumps(response).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        # ── Responses API handlers ─────────────────────────────────

        def _sse_event(self, event_type: str, data: dict):
            self.wfile.write(f"event: {event_type}\ndata: {json.dumps(data)}\n\n".encode())
            self.wfile.flush()

        def _make_response_obj(self, resp_id: str, status: str, output: list, error: dict | None = None) -> dict:
            obj = {
                "id": resp_id,
                "object": "response",
                "created_at": int(time.time()),
                "status": status,
                "model": "tabula",
                "output": output,
                "usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
            }
            if error:
                obj["error"] = error
            return obj

        def _make_message_item(self, msg_id: str, status: str, text: str = "") -> dict:
            return {
                "type": "message",
                "id": msg_id,
                "role": "assistant",
                "content": [{"type": "output_text", "text": text}],
                "status": status,
            }

        def _handle_responses_stream(self, session: SessionState, resp_id: str, msg_id: str):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.end_headers()

            empty_item = self._make_message_item(msg_id, "in_progress")
            resp_obj = self._make_response_obj(resp_id, "in_progress", [empty_item])

            self._sse_event("response.created", {"type": "response.created", "response": resp_obj})
            self._sse_event("response.in_progress", {"type": "response.in_progress", "response": resp_obj})
            self._sse_event("response.output_item.added", {
                "type": "response.output_item.added", "output_index": 0, "item": empty_item,
            })
            self._sse_event("response.content_part.added", {
                "type": "response.content_part.added",
                "item_id": msg_id, "output_index": 0, "content_index": 0,
                "part": {"type": "output_text", "text": ""},
            })

            full_text = ""
            while True:
                try:
                    kind, payload = session.events.get(timeout=300)
                except queue.Empty:
                    break

                if kind == "stream_delta":
                    full_text += payload
                    self._sse_event("response.output_text.delta", {
                        "type": "response.output_text.delta",
                        "item_id": msg_id, "output_index": 0, "content_index": 0,
                        "delta": payload,
                    })
                elif kind == "done":
                    self._sse_event("response.output_text.done", {
                        "type": "response.output_text.done",
                        "item_id": msg_id, "output_index": 0, "content_index": 0,
                        "text": full_text,
                    })
                    done_part = {"type": "output_text", "text": full_text}
                    self._sse_event("response.content_part.done", {
                        "type": "response.content_part.done",
                        "item_id": msg_id, "output_index": 0, "content_index": 0,
                        "part": done_part,
                    })
                    done_item = self._make_message_item(msg_id, "completed", full_text)
                    self._sse_event("response.output_item.done", {
                        "type": "response.output_item.done", "output_index": 0, "item": done_item,
                    })
                    done_resp = self._make_response_obj(resp_id, "completed", [done_item])
                    self._sse_event("response.completed", {"type": "response.completed", "response": done_resp})
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                    break
                elif kind == "error":
                    err = {"code": "api_error", "message": payload}
                    fail_resp = self._make_response_obj(resp_id, "failed", [], err)
                    self._sse_event("response.failed", {"type": "response.failed", "response": fail_resp})
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                    break
                elif kind == "disconnect":
                    break

        def _handle_responses_sync(self, session: SessionState, resp_id: str, msg_id: str):
            text_parts = []
            while True:
                try:
                    kind, payload = session.events.get(timeout=300)
                except queue.Empty:
                    break

                if kind == "stream_delta":
                    text_parts.append(payload)
                elif kind == "done":
                    break
                elif kind == "error":
                    err = {"code": "api_error", "message": payload}
                    resp = self._make_response_obj(resp_id, "failed", [], err)
                    body = json.dumps(resp).encode()
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
                elif kind == "disconnect":
                    break

            full_text = "".join(text_parts)
            item = self._make_message_item(msg_id, "completed", full_text)
            resp = self._make_response_obj(resp_id, "completed", [item])
            body = json.dumps(resp).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    return Handler


def main():
    parser = argparse.ArgumentParser(description="Tabula OpenAI-compatible API gateway")
    parser.add_argument("--port", type=int, default=8090, help="HTTP port to listen on")
    args = parser.parse_args()

    gateway = GatewayAPI()
    server = HTTPServer(("0.0.0.0", args.port), make_handler(gateway))
    server.daemon_threads = True
    log(f"listening on http://0.0.0.0:{args.port}")
    print(f"gateway-api listening on http://0.0.0.0:{args.port}", file=sys.stderr)

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        for state in gateway.sessions.values():
            state.close()
        server.server_close()


if __name__ == "__main__":
    main()
