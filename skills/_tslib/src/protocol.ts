/**
 * Protocol constants shared across all Tabula skills (TypeScript port).
 *
 * Mirrors `skills/_pylib/protocol.py` and `internal/kernel/protocol.go`.
 * This is the single source of truth for message types, hook actions,
 * and tool names on the TypeScript side.
 */

// Protocol version — must match Go kernel's ProtocolVersion.
export const PROTOCOL_VERSION = 1;

// -- Message types (client -> kernel) ----------------------------------------
export const MSG_CONNECT = "connect";
export const MSG_JOIN = "join";
export const MSG_MESSAGE = "message";
export const MSG_TOOL_USE = "tool_use";
export const MSG_HOOK_RESULT = "hook_result";
export const MSG_STREAM_START = "stream_start";
export const MSG_STREAM_DELTA = "stream_delta";
export const MSG_STREAM_END = "stream_end";
export const MSG_DONE = "done";
export const MSG_STATUS = "status";
export const MSG_CANCEL = "cancel";

// -- Message types (kernel -> client) ----------------------------------------
export const MSG_CONNECTED = "connected";
export const MSG_JOINED = "joined";
export const MSG_MEMBER_JOINED = "member_joined";
export const MSG_INIT = "init";
export const MSG_TOOL_RESULT = "tool_result";
export const MSG_HOOK = "hook";
export const MSG_ERROR = "error";

export const ALL_MESSAGE_TYPES = new Set<string>([
	MSG_CONNECT, MSG_JOIN, MSG_MESSAGE, MSG_TOOL_USE, MSG_HOOK_RESULT,
	MSG_STREAM_START, MSG_STREAM_DELTA, MSG_STREAM_END, MSG_DONE,
	MSG_STATUS, MSG_CANCEL,
	MSG_CONNECTED, MSG_JOINED, MSG_MEMBER_JOINED, MSG_INIT,
	MSG_TOOL_RESULT, MSG_HOOK, MSG_ERROR,
]);

// -- Hook actions ------------------------------------------------------------
export const HOOK_PASS = "pass";
export const HOOK_MODIFY = "modify";
export const HOOK_BLOCK = "block";
export const HOOK_CLAIM = "claim";

// -- Kernel tools ------------------------------------------------------------
export const TOOL_SHELL_EXEC = "shell_exec";
export const TOOL_PROCESS_SPAWN = "process_spawn";
export const TOOL_PROCESS_KILL = "process_kill";
export const TOOL_PROCESS_LIST = "process_list";
export const DEFAULT_KERNEL_TOOLS = [
	TOOL_SHELL_EXEC,
	TOOL_PROCESS_SPAWN,
	TOOL_PROCESS_KILL,
	TOOL_PROCESS_LIST,
] as const;

// -- Hook events -------------------------------------------------------------
export const HOOK_BEFORE_MESSAGE = "before_message";
export const HOOK_AFTER_MESSAGE = "after_message";
export const HOOK_BEFORE_TOOL_CALL = "before_tool_call";
export const HOOK_AFTER_TOOL_CALL = "after_tool_call";
export const HOOK_SESSION_START = "session_start";
export const HOOK_SESSION_END = "session_end";
export const HOOK_CANCEL = "cancel";
export const HOOK_BEFORE_SPAWN = "before_spawn";
export const HOOK_AFTER_SPAWN = "after_spawn";

// -- Message envelope fields -------------------------------------------------
// Optional opaque JSON object carried alongside messages. The kernel does not
// interpret its contents; it is forwarded as-is. Conventional uses:
//   - on `init`:        {"project_root": "/abs/path", ...}
//   - on `tool_result`: {"diff": "...", "files": [...], "summary": "..."}
export const FIELD_META = "meta";

// -- Message envelope --------------------------------------------------------

/**
 * Generic Tabula protocol envelope. Fields mirror `internal/kernel/message.go`.
 * Only `version` and `type` are guaranteed; the rest depend on the message type.
 */
export interface Envelope {
	version?: number;
	type: string;
	name?: string;
	session?: string;
	id?: string;
	text?: string;
	input?: unknown;
	output?: unknown;
	context?: Record<string, unknown>;
	tools?: unknown;
	sends?: string[];
	receives?: string[];
	token?: string;
	hooks?: unknown;
	payload?: unknown;
	action?: string;
	reason?: string;
	meta?: Record<string, unknown>;
	[k: string]: unknown;
}
