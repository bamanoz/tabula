"""Protocol constants and NDJSON helpers for Tabula plugins."""

from __future__ import annotations

import json
import sys
from typing import Any, TextIO

PLUGIN_PROTOCOL_VERSION = 1

METHOD_REGISTER_REQUEST = "register_request"
METHOD_REGISTER = "register"
METHOD_TOOL_CALL = "tool_call"
METHOD_TOOL_RESULT = "tool_result"
METHOD_EVENT = "event"
METHOD_EVENT_REPLY = "event_reply"
METHOD_SEND = "send"
METHOD_LOG = "log"
METHOD_SHUTDOWN = "shutdown"
METHOD_UPDATE_TOOLS = "update_tools"

ACTION_OK = "ok"
ACTION_REWRITE = "rewrite"
ACTION_DENY = "deny"
ACTION_CLAIM = "claim"


def read_message(stream: TextIO | None = None) -> dict[str, Any] | None:
    """Read one NDJSON protocol message, returning None at EOF."""

    stream = stream or sys.stdin
    for line in stream:
        if not line.strip():
            continue
        message = json.loads(line)
        if not isinstance(message, dict):
            raise ValueError("protocol message must be a JSON object")
        return message
    return None


def write_message(method: str, params: dict[str, Any] | None = None, stream: TextIO | None = None) -> None:
    """Write one NDJSON protocol message and flush immediately."""

    stream = stream or sys.stdout
    stream.write(json.dumps({"method": method, "params": params or {}}, separators=(",", ":")))
    stream.write("\n")
    stream.flush()
