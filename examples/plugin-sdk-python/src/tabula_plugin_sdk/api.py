"""Tiny runtime API for reference Tabula plugins.

This intentionally implements the Phase 3 minimum surface: register, tool_call,
event/event_reply, send, log, update_tools, and graceful shutdown handling.
"""

from __future__ import annotations

import json
import signal
import sys
from collections.abc import Callable
from typing import Any

from . import protocol

ToolHandler = Callable[[dict[str, Any], dict[str, Any]], Any]
EventHandler = Callable[[dict[str, Any], dict[str, Any]], dict[str, Any] | None]
ShutdownHandler = Callable[[], None]


class PluginAPI:
    def __init__(self, register_request: dict[str, Any]):
        self.register_request = register_request
        self.plugin_id = str(register_request.get("plugin_id", ""))
        self.config = dict(register_request.get("config") or {})
        self.protocol_version = int(register_request.get("protocol_version") or protocol.PLUGIN_PROTOCOL_VERSION)
        self._tools: list[dict[str, Any]] = []
        self._subscriptions: list[dict[str, Any]] = []
        self._tool_handlers: dict[str, ToolHandler] = {}
        self._event_handlers: dict[str, EventHandler] = {}
        self._shutdown_handlers: list[ShutdownHandler] = []
        self._shutdown = False

    def tool(self, name: str, description: str = "", schema: dict[str, Any] | None = None, deadline_ms: int | None = None):
        """Decorator/register helper for a plugin tool."""

        def decorator(fn: ToolHandler) -> ToolHandler:
            spec: dict[str, Any] = {"name": name}
            if description:
                spec["description"] = description
            if schema is not None:
                spec["schema"] = schema
            if deadline_ms is not None:
                spec["deadline_ms"] = deadline_ms
            self._tools.append(spec)
            self._tool_handlers[name] = fn
            return fn

        return decorator

    def on(self, event: str, priority: int = 0, timeout_ms: int | None = None):
        """Decorator/register helper for a hook event subscription."""

        def decorator(fn: EventHandler) -> EventHandler:
            spec: dict[str, Any] = {"event": event, "priority": priority}
            if timeout_ms is not None:
                spec["timeout_ms"] = timeout_ms
            self._subscriptions.append(spec)
            self._event_handlers[event] = fn
            return fn

        return decorator

    def on_shutdown(self, fn: ShutdownHandler) -> ShutdownHandler:
        """Register a cleanup callback run when the kernel asks us to stop."""

        self._shutdown_handlers.append(fn)
        return fn

    def register(self) -> None:
        protocol.write_message(
            protocol.METHOD_REGISTER,
            {
                "protocol_version": self.protocol_version,
                "plugin_id": self.plugin_id,
                "tools": self._tools,
                "subscriptions": self._subscriptions,
            },
        )

    def update_tools(self, tools: list[dict[str, Any]], removed: list[str] | None = None) -> None:
        self._tools = list(tools)
        protocol.write_message(protocol.METHOD_UPDATE_TOOLS, {"tools": self._tools, "removed": removed or []})

    def log(self, msg: str, level: str = "info", **fields: Any) -> None:
        params: dict[str, Any] = {"level": level, "msg": msg}
        if fields:
            params["fields"] = fields
        protocol.write_message(protocol.METHOD_LOG, params)

    def send(self, type: str, payload: Any = None, session: str = "") -> None:
        params: dict[str, Any] = {"channel": "bus", "type": type}
        if payload is not None:
            params["payload"] = payload
        if session:
            params["session"] = session
        protocol.write_message(protocol.METHOD_SEND, params)

    def serve(self) -> None:
        self.register()
        try:
            while not self._shutdown:
                msg = protocol.read_message()
                if msg is None:
                    break
                method = msg.get("method")
                params = dict(msg.get("params") or {})
                if method == protocol.METHOD_TOOL_CALL:
                    self._handle_tool_call(params)
                elif method == protocol.METHOD_EVENT:
                    self._handle_event(params)
                elif method == protocol.METHOD_SHUTDOWN:
                    self._shutdown = True
                elif method == protocol.METHOD_REGISTER_REQUEST:
                    # Already consumed by run(); ignore duplicate preambles.
                    continue
                else:
                    self.log(f"unknown method {method}", level="warn")
        finally:
            self._run_shutdown_handlers()

    def _run_shutdown_handlers(self) -> None:
        for handler in reversed(self._shutdown_handlers):
            try:
                handler()
            except Exception as exc:  # pragma: no cover - best-effort cleanup path
                self.log(f"shutdown handler failed: {exc}", level="warn")

    def _handle_tool_call(self, params: dict[str, Any]) -> None:
        call_id = str(params.get("callId") or "")
        name = str(params.get("name") or "")
        try:
            handler = self._tool_handlers[name]
            args = params.get("args")
            if not isinstance(args, dict):
                args = {}
            result = handler(args, params)
            protocol.write_message(protocol.METHOD_TOOL_RESULT, {"callId": call_id, "result": result})
        except Exception as exc:  # pragma: no cover - exercised by integration if handler fails
            protocol.write_message(protocol.METHOD_TOOL_RESULT, {"callId": call_id, "error": str(exc)})

    def _handle_event(self, params: dict[str, Any]) -> None:
        event = str(params.get("event") or "")
        call_id = str(params.get("callId") or "")
        handler = self._event_handlers.get(event)
        reply = {"action": protocol.ACTION_OK}
        if handler is not None:
            data = params.get("data")
            if isinstance(data, str):
                data = json.loads(data)
            if not isinstance(data, dict):
                data = {}
            reply = handler(data, params) or reply
        if call_id:
            protocol.write_message(protocol.METHOD_EVENT_REPLY, {"callId": call_id, **reply})


def run(configure: Callable[[PluginAPI], None]) -> None:
    """Run a plugin using a configure(api) callback."""

    def _sigterm(_signum: int, _frame: Any) -> None:
        sys.exit(0)

    signal.signal(signal.SIGTERM, _sigterm)
    first = protocol.read_message()
    if first is None or first.get("method") != protocol.METHOD_REGISTER_REQUEST:
        raise RuntimeError("expected register_request as first plugin message")
    api = PluginAPI(dict(first.get("params") or {}))
    configure(api)
    api.serve()
