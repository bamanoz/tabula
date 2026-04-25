#!/usr/bin/env python3
"""Protocol constants shared across all Tabula skills.

This is the single source of truth for message types, hook actions,
and tool names on the Python side. All skills should import from here
instead of hardcoding string literals.
"""

from __future__ import annotations

# Protocol version — must match Go kernel's ProtocolVersion.
PROTOCOL_VERSION = 1

# -- Message types (client -> kernel) ------------------------------------------
MSG_CONNECT = "connect"
MSG_JOIN = "join"
MSG_MESSAGE = "message"
MSG_TOOL_USE = "tool_use"
MSG_HOOK_RESULT = "hook_result"
MSG_STREAM_START = "stream_start"
MSG_STREAM_DELTA = "stream_delta"
MSG_STREAM_END = "stream_end"
MSG_DONE = "done"
MSG_STATUS = "status"
MSG_CANCEL = "cancel"

# -- Message types (kernel -> client) ------------------------------------------
MSG_CONNECTED = "connected"
MSG_JOINED = "joined"
MSG_MEMBER_JOINED = "member_joined"
MSG_INIT = "init"
MSG_TOOL_RESULT = "tool_result"
MSG_HOOK = "hook"
MSG_ERROR = "error"

# -- All message types (for capability declarations) ---------------------------
ALL_MESSAGE_TYPES = {
    MSG_CONNECT, MSG_JOIN, MSG_MESSAGE, MSG_TOOL_USE, MSG_HOOK_RESULT,
    MSG_STREAM_START, MSG_STREAM_DELTA, MSG_STREAM_END, MSG_DONE,
    MSG_STATUS, MSG_CANCEL,
    MSG_CONNECTED, MSG_JOINED, MSG_MEMBER_JOINED, MSG_INIT,
    MSG_TOOL_RESULT, MSG_HOOK, MSG_ERROR,
}

# -- Hook actions ---------------------------------------------------------------
HOOK_PASS = "pass"
HOOK_MODIFY = "modify"
HOOK_BLOCK = "block"
HOOK_CLAIM = "claim"

# -- Kernel tools ---------------------------------------------------------------
TOOL_SHELL_EXEC = "shell_exec"
TOOL_PROCESS_SPAWN = "process_spawn"
TOOL_PROCESS_KILL = "process_kill"
TOOL_PROCESS_LIST = "process_list"
DEFAULT_KERNEL_TOOLS = [
    TOOL_SHELL_EXEC,
    TOOL_PROCESS_SPAWN,
    TOOL_PROCESS_KILL,
    TOOL_PROCESS_LIST,
]

# -- Hook events ----------------------------------------------------------------
HOOK_BEFORE_MESSAGE = "before_message"
HOOK_AFTER_MESSAGE = "after_message"
HOOK_BEFORE_TOOL_CALL = "before_tool_call"
HOOK_AFTER_TOOL_CALL = "after_tool_call"
HOOK_SESSION_START = "session_start"
HOOK_SESSION_END = "session_end"
HOOK_CANCEL = "cancel"
HOOK_BEFORE_SPAWN = "before_spawn"
HOOK_AFTER_SPAWN = "after_spawn"

# -- Message envelope fields ---------------------------------------------------
# Optional opaque JSON object carried alongside messages. The kernel does not
# interpret its contents; it is forwarded as-is. Conventional uses:
#   - on `init`:        {"project_root": "/abs/path", ...}
#   - on `tool_result`: {"diff": "...", "files": [...], "summary": "..."}
FIELD_META = "meta"
