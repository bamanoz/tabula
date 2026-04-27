package kernel

import "fmt"

// ProtocolVersion is the current wire protocol version.
// Incremented when breaking changes are made to the message format.
const ProtocolVersion = 1

// MsgType constants for the wire protocol.
// These are the string values used in JSON over WebSocket.
type MsgType string

const (
	MsgConnect      MsgType = "connect"
	MsgConnected    MsgType = "connected"
	MsgJoin         MsgType = "join"
	MsgJoined       MsgType = "joined"
	MsgMemberJoined MsgType = "member_joined"
	MsgInit         MsgType = "init"
	MsgMessage      MsgType = "message"
	MsgStreamStart  MsgType = "stream_start"
	MsgStreamDelta  MsgType = "stream_delta"
	MsgStreamEnd    MsgType = "stream_end"
	MsgDone         MsgType = "done"
	MsgToolUse      MsgType = "tool_use"
	MsgToolResult   MsgType = "tool_result"
	MsgHook         MsgType = "hook"
	MsgHookResult   MsgType = "hook_result"
	MsgCancel       MsgType = "cancel"
	MsgError        MsgType = "error"
	MsgStatus       MsgType = "status"
)

// KernelTool is retained as a type for tool name constants. The four legacy
// kernel-builtin tools (shell_exec, process_spawn, process_kill, process_list)
// are no longer dispatched from kernel as of Phase 1 D1.1 of the skill/plugin
// architecture migration (see docs/plans/SKILL_PLUGIN_ARCHITECTURE.md §4.1).
//
// The constants below are kept as deprecated identifiers so that:
//   1. Existing tests can reference them while being marked t.Skip per
//      creative §7 dead-code-keep policy (D1.11 option b).
//   2. The subagent plugin (in tabula-bundles) can re-use the same string
//      values for its `process_spawn` semantics when it lands.
//
// TODO(skill-plugin-arch): remove these constants once subagent plugin GA in
// tabula-bundles restores the spawn invariant via plugin-side dispatch.
type KernelTool string

const (
	// Deprecated: kernel no longer dispatches shell_exec. Use a skill or
	// plugin tool instead. See docs/plans/SKILL_PLUGIN_ARCHITECTURE.md §4.
	ToolShellExec KernelTool = "shell_exec"
	// Deprecated: kernel no longer dispatches process_spawn. Migrating to
	// the subagent plugin in tabula-bundles. See D1.11(b).
	ToolProcessSpawn KernelTool = "process_spawn"
	// Deprecated: kernel no longer dispatches process_kill.
	ToolProcessKill KernelTool = "process_kill"
	// Deprecated: kernel no longer dispatches process_list.
	ToolProcessList KernelTool = "process_list"
)

// DefaultKernelTools is now empty: kernel ships with zero builtin LLM-tools.
// Kept as an exported symbol for downstream callers that enumerate kernel
// tools at boot time.
var DefaultKernelTools = []KernelTool{}

// DefaultKernelToolNames returns an empty slice; see DefaultKernelTools.
func DefaultKernelToolNames() []string {
	return []string{}
}

// PluginProtocolVersion is the current stdio JSON-RPC protocol version used
// for kernel ↔ plugin communication (distinct from ProtocolVersion which
// covers WebSocket kernel ↔ client). Bumped independently per
// memory-bank/creative/creative-plugin-protocol.md §2.2.
const PluginProtocolVersion = 1

// HookAction constants for hook responses.
type HookAction string

const (
	ActionPass   HookAction = "pass"
	ActionModify HookAction = "modify"
	ActionBlock  HookAction = "block"
	ActionClaim  HookAction = "claim"
)

// validateMessage checks that a client message has the required fields for its type.
// Returns nil if valid, or an error describing what's missing.
func validateMessage(msg *Message) error {
	if msg.Type == "" {
		return fmt.Errorf("missing message type")
	}

	switch MsgType(msg.Type) {
	case MsgConnect:
		if msg.Name == "" {
			return fmt.Errorf("connect: missing name")
		}
	case MsgJoin:
		if msg.Session == "" {
			return fmt.Errorf("join: missing session")
		}
	case MsgToolUse:
		if msg.ID == "" {
			return fmt.Errorf("tool_use: missing id")
		}
		if msg.Name == "" {
			return fmt.Errorf("tool_use: missing name")
		}
	case MsgHookResult:
		if msg.ID == "" {
			return fmt.Errorf("hook_result: missing id")
		}
		if msg.Action == "" {
			return fmt.Errorf("hook_result: missing action")
		}
	}
	return nil
}
