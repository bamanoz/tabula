package kernel

// BusMessageType identifies internal extension-routing message shapes. These
// values are carried inside protocol v4 extension.send/extension.event data;
// they are not a standalone wire protocol.
type BusMessageType string

const (
	MsgJoin      BusMessageType = "join"
	MsgJoined    BusMessageType = "joined"
	MsgEvent     BusMessageType = "event"
	MsgRequest   BusMessageType = "request"
	MsgReply     BusMessageType = "reply"
	MsgHook      BusMessageType = "hook"
	MsgHookReply BusMessageType = "hook_reply"
	MsgError     BusMessageType = "error"
)

const (
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
