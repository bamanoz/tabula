package kernel

import (
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
)

// ProtocolVersion is the current wire protocol version.
// Incremented when breaking changes are made to the message format.
const ProtocolVersion = 3

// MsgType constants for the wire protocol.
// These are the string values used in JSON over WebSocket.
type MsgType string

const (
	MsgHello     MsgType = "hello"
	MsgHelloAck  MsgType = "hello_ack"
	MsgJoin      MsgType = "join"
	MsgJoined    MsgType = "joined"
	MsgEvent     MsgType = "event"
	MsgRequest   MsgType = "request"
	MsgReply     MsgType = "reply"
	MsgHook      MsgType = "hook"
	MsgHookReply MsgType = "hook_reply"
	MsgError     MsgType = "error"
	MsgStatus    MsgType = "status"
)

const (
	TopicMessageUser         = "message.user"
	TopicKernelSessions      = "kernel.sessions.snapshot"
	TopicStreamStart         = "stream.start"
	TopicStreamDelta         = "stream.delta"
	TopicStreamEnd           = "stream.end"
	TopicReasonStart         = "reasoning.start"
	TopicReasonDelta         = "reasoning.delta"
	TopicReasonEnd           = "reasoning.end"
	TopicToolCall            = "tool.call"
	TopicToolResult          = "tool.result"
	TopicToolResultStart     = "tool.result.start"
	TopicToolResultDelta     = "tool.result.delta"
	TopicToolResultEnd       = "tool.result.end"
	TopicExchangeChoose      = "exchange.choose"
	TopicExchangeApprove     = "exchange.approve"
	TopicSessionInit         = "session.init"
	TopicSessionMemberJoined = "session.member_joined"
	TopicSessionStatus       = "session.status"
	TopicSessionArchive      = "session.archive"
	TopicSessionDelete       = "session.delete"
	TopicSessionArchived     = "session.archived"
	TopicSessionDeleted      = "session.deleted"
	TopicTurnDone            = "turn.done"
	TopicTurnCancel          = "turn.cancel"
	TopicTurnSteer           = "turn.steer"
	TopicCompactionStart     = "compaction.start"
	TopicCompactionEnd       = "compaction.end"
	TopicCompactionError     = "compaction.error"
)

func isExchangeTopic(topic string) bool {
	return topic == TopicExchangeChoose || topic == TopicExchangeApprove
}

// MinPluginProtocolVersion and MaxPluginProtocolVersion describe the worker
// protocol generation accepted through the runtime-owned plugin path.
const (
	MinPluginProtocolVersion = 1
	MaxPluginProtocolVersion = 1
)

// validateMessage checks that a client message has the required fields for its type.
// Returns nil if valid, or an error describing what's missing.
func validateMessage(msg *Message) error {
	if msg.Type == "" {
		return fmt.Errorf("missing message type")
	}
	if msg.V != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d (kernel expects %d)", msg.V, ProtocolVersion)
	}

	switch MsgType(msg.Type) {
	case MsgHello:
		data, err := decodeHelloData(msg)
		if err != nil {
			return err
		}
		if data.Name == "" {
			return fmt.Errorf("hello: missing name")
		}
	case MsgJoin:
		if msg.Session == "" {
			return fmt.Errorf("join: missing session")
		}
	case MsgEvent:
		if msg.Topic == "" {
			return fmt.Errorf("event: missing topic")
		}
		if msg.Topic == TopicMessageUser && messageText(msg) == "" {
			return fmt.Errorf("event %s: missing data.text", TopicMessageUser)
		}
		if msg.Topic == TopicStreamDelta && eventText(msg) == "" {
			return fmt.Errorf("event %s: missing data.text", TopicStreamDelta)
		}
		if msg.Topic == TopicReasonDelta && eventText(msg) == "" {
			return fmt.Errorf("event %s: missing data.text", TopicReasonDelta)
		}
	case MsgRequest:
		if msg.Topic == "" {
			return fmt.Errorf("request: missing topic")
		}
		if isExchangeTopic(msg.Topic) && msg.ID == "" {
			return fmt.Errorf("request %s: missing id", msg.Topic)
		}
		if msg.Topic != TopicToolCall {
			break
		}
		if msg.ID == "" {
			return fmt.Errorf("request %s: missing id", TopicToolCall)
		}
		if msg.Name == "" {
			return fmt.Errorf("request %s: missing name", TopicToolCall)
		}
	case MsgReply:
		if msg.Topic == "" {
			return fmt.Errorf("reply: missing topic")
		}
		if (msg.Topic == TopicToolResult || isExchangeTopic(msg.Topic)) && msg.ID == "" {
			return fmt.Errorf("reply %s: missing id", msg.Topic)
		}
	case MsgHookReply:
		if msg.ID == "" {
			return fmt.Errorf("hook_reply: missing id")
		}
		if msg.Action == "" {
			return fmt.Errorf("hook_reply: missing action")
		}
	}
	return nil
}

type helloData struct {
	Name          string                `json:"name"`
	AuthToken     string                `json:"auth_token"`
	Roles         []string              `json:"roles,omitempty"`
	SendTopics    []string              `json:"send_topics,omitempty"`
	ReceiveTopics []string              `json:"receive_topics,omitempty"`
	GlobalTopics  []string              `json:"global_topics,omitempty"`
	Hooks         []khooks.Subscription `json:"hooks,omitempty"`
	Meta          json.RawMessage       `json:"meta,omitempty"`
}

func decodeHelloData(msg *Message) (helloData, error) {
	if msg == nil || len(msg.Data) == 0 {
		return helloData{}, fmt.Errorf("hello: missing data")
	}
	var data helloData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return helloData{}, fmt.Errorf("hello: invalid data: %w", err)
	}
	return data, nil
}
