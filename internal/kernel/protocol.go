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
