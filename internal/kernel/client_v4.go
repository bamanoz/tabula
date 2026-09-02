package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/sessionrecord"
)

const ClientProtocolVersion = 4

const defaultClientEventLimit = 256

var errClientAuthentication = errors.New("client authentication failed")

// ClientEnvelope is the protocol v4 envelope used by authenticated clients.
type ClientEnvelope struct {
	V         int             `json:"v"`
	Kind      string          `json:"kind"`
	Op        string          `json:"op"`
	ID        string          `json:"id,omitempty"`
	TenantID  string          `json:"tenant_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Data      json.RawMessage `json:"data"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

type clientErrorData struct {
	Code                string  `json:"code"`
	Message             string  `json:"message"`
	Retryable           bool    `json:"retryable"`
	SnapshotRequired    bool    `json:"snapshot_required,omitempty"`
	RetainedAfterCursor string  `json:"retained_after_cursor,omitempty"`
	SessionVersion      *uint64 `json:"session_version,omitempty"`
}

type connectionOpenData struct {
	Name          string                `json:"name"`
	AuthToken     string                `json:"auth_token"`
	SendTopics    []string              `json:"send_topics,omitempty"`
	ReceiveTopics []string              `json:"receive_topics,omitempty"`
	ReceiveGlobal []string              `json:"receive_global_topics,omitempty"`
	Hooks         []khooks.Subscription `json:"hooks,omitempty"`
	Meta          json.RawMessage       `json:"meta,omitempty"`
}

type extensionSendData struct {
	Type      string          `json:"type"`
	Topic     string          `json:"topic"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Text      string          `json:"text,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	State     string          `json:"state,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	Artifact  json.RawMessage `json:"artifact,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	Context   string          `json:"context,omitempty"`
	Tools     json.RawMessage `json:"tools,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Action    string          `json:"action,omitempty"`
	Reason    string          `json:"reason,omitempty"`
}

type sessionCreateData struct {
	DriverComponentID string `json:"driver_component_id"`
	AgentSpecRevision string `json:"agent_spec_revision"`
}

type sessionMutationData struct {
	ExpectedSessionVersion *uint64 `json:"expected_session_version"`
}

type sessionListData struct {
	Statuses        []agent.SessionStatus `json:"statuses,omitempty"`
	IncludeArchived bool                  `json:"include_archived,omitempty"`
	AfterSessionID  string                `json:"after_session_id,omitempty"`
	Limit           int                   `json:"limit,omitempty"`
}

type inputSubmitData struct {
	InputID                string          `json:"input_id"`
	Content                json.RawMessage `json:"content"`
	ExpectedSessionVersion *uint64         `json:"expected_session_version,omitempty"`
}

type turnData struct {
	TurnID string `json:"turn_id"`
}

type recoveryData struct {
	TurnID                 string      `json:"turn_id"`
	Fence                  agent.Fence `json:"fence,omitempty"`
	ReconciliationEvidence string      `json:"reconciliation_evidence,omitempty"`
}

type subscribeData struct {
	AfterCursor string `json:"after_cursor"`
	Limit       *int   `json:"limit,omitempty"`
}

type sessionRecordAppendData struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type sessionRecordListData struct {
	Kind     string `json:"kind,omitempty"`
	AfterID  int64  `json:"after_id,omitempty"`
	BeforeID int64  `json:"before_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type clientSessionRecord struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Producer  string          `json:"producer"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

type clientInputAcceptance struct {
	InputID        string `json:"input_id"`
	TurnID         string `json:"turn_id"`
	Position       int    `json:"position"`
	SessionVersion uint64 `json:"session_version"`
	Cursor         string `json:"cursor"`
}

type clientStoredEvent struct {
	EventID        string          `json:"event_id"`
	Cursor         string          `json:"cursor"`
	SessionVersion uint64          `json:"session_version"`
	Type           string          `json:"type"`
	OccurredAt     string          `json:"occurred_at"`
	Data           json.RawMessage `json:"data"`
}

// DecodeClientEnvelope strictly decodes one protocol v4 client frame.
func DecodeClientEnvelope(data []byte) (*ClientEnvelope, error) {
	var envelope ClientEnvelope
	if err := decodeStrict(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.V != ClientProtocolVersion {
		return nil, fmt.Errorf("v must be %d", ClientProtocolVersion)
	}
	if envelope.Kind == "" || envelope.Op == "" || envelope.ID == "" || len(envelope.Data) == 0 {
		return nil, errors.New("kind, op, id, and data are required")
	}
	if envelope.Kind != "command" && envelope.Kind != "query" {
		return nil, errors.New("kind must be command or query")
	}
	if envelope.Op != "connection.open" && envelope.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if envelope.Op != "connection.open" && envelope.Op != "session.list" && envelope.Op != "extension.send" && envelope.SessionID == "" {
		return nil, errors.New("session_id is required")
	}
	return &envelope, nil
}

// HandleClientEnvelope routes one validated protocol v4 client request.
func (h *Hub) HandleClientEnvelope(sender *Client, envelope *ClientEnvelope) {
	if sender == nil || envelope == nil {
		return
	}
	if err := validateClientEnvelopeRoute(sender, envelope); err != nil {
		h.sendClientError(sender, envelope, err)
		return
	}
	ctx := context.Background()
	if envelope.Op == "connection.open" {
		response, err := h.handleClientConnectionOpen(sender, envelope)
		if err != nil {
			h.sendClientError(sender, envelope, err)
			return
		}
		sender.SendEnvelope(response)
		return
	}
	if envelope.Op == "extension.send" {
		response, err := h.handleClientExtensionSend(sender, envelope)
		if err != nil {
			h.sendClientError(sender, envelope, err)
			return
		}
		sender.SendEnvelope(response)
		return
	}
	if envelope.Op == TopicToolCall {
		if err := h.HandleClientToolCall(sender, envelope); err != nil {
			h.sendClientError(sender, envelope, err)
		}
		return
	}
	if envelope.Op != "session.record.append" && envelope.Op != "session.record.list" && (h.agentSessions == nil || h.inputProcessor == nil || h.recovery == nil) {
		h.sendClientError(sender, envelope, errors.New("session repository unavailable"))
		return
	}

	key := agent.SessionKey{TenantID: envelope.TenantID, SessionID: envelope.SessionID}
	var response *ClientEnvelope
	var err error
	switch envelope.Op {
	case "session.create":
		response, err = h.handleClientSessionCreate(ctx, envelope, key)
	case "session.archive":
		response, err = h.handleClientSessionMutation(ctx, envelope, key, agent.ArchiveSession{Meta: agent.CommandMeta{ID: envelope.ID, Actor: agent.ActorClient}})
	case "session.unarchive":
		response, err = h.handleClientSessionMutation(ctx, envelope, key, agent.UnarchiveSession{Meta: agent.CommandMeta{ID: envelope.ID, Actor: agent.ActorClient}})
	case "session.delete":
		response, err = h.handleClientSessionMutation(ctx, envelope, key, agent.DeleteSession{Meta: agent.CommandMeta{ID: envelope.ID, Actor: agent.ActorClient}})
	case "input.submit":
		response, err = h.handleClientInputSubmit(ctx, sender, envelope, key)
	case "turn.cancel":
		response, err = h.handleClientTurnCancel(ctx, envelope, key)
	case "turn.resume":
		response, err = h.handleClientRecovery(ctx, sender, envelope, key, agent.RecoveryResume)
	case "turn.retry":
		response, err = h.handleClientRecovery(ctx, sender, envelope, key, agent.RecoveryRetry)
	case "turn.discard":
		response, err = h.handleClientRecovery(ctx, sender, envelope, key, agent.RecoveryDiscard)
	case "session.get":
		response, err = h.handleClientSessionGet(ctx, envelope, key)
	case "session.list":
		response, err = h.handleClientSessionList(ctx, envelope)
	case "session.subscribe":
		response, err = h.handleClientSessionSubscribe(ctx, sender, envelope, key)
	case "session.record.append":
		response, err = h.handleClientSessionRecordAppend(ctx, sender, envelope, key)
	case "session.record.list":
		response, err = h.handleClientSessionRecordList(ctx, envelope, key)
	default:
		err = fmt.Errorf("%w: unsupported operation %q", agent.ErrInvalidArgument, envelope.Op)
	}
	if err != nil {
		h.sendClientError(sender, envelope, err)
		return
	}
	sender.SendEnvelope(response)
}

func validateClientEnvelopeRoute(sender *Client, envelope *ClientEnvelope) error {
	if envelope.V != ClientProtocolVersion {
		return fmt.Errorf("%w: v must be %d", agent.ErrInvalidArgument, ClientProtocolVersion)
	}
	if envelope.ID == "" || len(envelope.Data) == 0 {
		return fmt.Errorf("%w: id and data are required", agent.ErrInvalidArgument)
	}
	if envelope.Kind != "command" && envelope.Kind != "query" {
		return fmt.Errorf("%w: kind must be command or query", agent.ErrInvalidArgument)
	}
	if envelope.Op == "connection.open" {
		if envelope.Kind != "command" || envelope.TenantID != "" || envelope.SessionID != "" || sender.name != "" {
			return fmt.Errorf("%w: connection.open must be the first unscoped command", agent.ErrInvalidArgument)
		}
		return nil
	}
	if sender.name == "" {
		return errClientAuthentication
	}
	if envelope.TenantID == "" {
		return fmt.Errorf("%w: tenant_id is required", agent.ErrInvalidArgument)
	}
	if envelope.Op != "session.list" && envelope.Op != "extension.send" && envelope.SessionID == "" {
		return fmt.Errorf("%w: session_id is required", agent.ErrInvalidArgument)
	}
	if sender.tenantID != "" && sender.tenantID != envelope.TenantID {
		return agent.ErrPermissionDenied
	}
	if envelope.Op != "session.list" && envelope.Op != "extension.send" && sender.session != "" && sender.session != envelope.SessionID {
		return agent.ErrPermissionDenied
	}
	commands := map[string]bool{"session.create": true, "session.archive": true, "session.unarchive": true, "session.delete": true, "session.record.append": true, "input.submit": true, "turn.cancel": true, "turn.resume": true, "turn.retry": true, "turn.discard": true, "extension.send": true, TopicToolCall: true}
	queries := map[string]bool{"session.get": true, "session.list": true, "session.subscribe": true, "session.record.list": true}
	if envelope.Kind == "command" && !commands[envelope.Op] || envelope.Kind == "query" && !queries[envelope.Op] {
		return fmt.Errorf("%w: operation %q is not valid for kind %q", agent.ErrInvalidArgument, envelope.Op, envelope.Kind)
	}
	return nil
}

func (h *Hub) handleClientConnectionOpen(sender *Client, envelope *ClientEnvelope) (*ClientEnvelope, error) {
	var data connectionOpenData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, err
	}
	data.Name = strings.TrimSpace(data.Name)
	if data.Name == "" {
		return nil, fmt.Errorf("%w: client name is required", agent.ErrInvalidArgument)
	}
	if len(data.Meta) != 0 {
		var meta map[string]any
		if err := decodeStrict(data.Meta, &meta); err != nil {
			return nil, fmt.Errorf("%w: meta must be an object", agent.ErrInvalidArgument)
		}
	}
	depth, err := h.policy.CanConnect("", data.AuthToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errClientAuthentication, err)
	}
	clientID := h.configureClient(sender, data.Name, data.SendTopics, data.ReceiveTopics, data.ReceiveGlobal, data.Hooks, data.Meta, depth)
	if !sender.MarkProtocolReady() {
		return nil, fmt.Errorf("%w: connection is already open", agent.ErrInvalidTransition)
	}
	if len(data.Hooks) != 0 {
		h.rebuildHookIndex()
	}
	h.Logger.Info("client connected", "name", sender.name, "id", clientID)
	return clientResponse(envelope, "result", "connection.open", map[string]any{
		"client_id":       fmt.Sprintf("c%d", clientID),
		"server_protocol": ClientProtocolVersion,
	})
}

func (h *Hub) handleClientExtensionSend(sender *Client, envelope *ClientEnvelope) (*ClientEnvelope, error) {
	var data extensionSendData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, err
	}
	if data.Type == "" {
		data.Type = string(MsgEvent)
	}
	if data.Type == string(MsgError) {
		return nil, fmt.Errorf("%w: extension message type %q is reserved", agent.ErrInvalidArgument, data.Type)
	}
	if data.Type != string(MsgJoin) && data.Type != string(MsgHookReply) && data.Topic == "" {
		return nil, fmt.Errorf("%w: extension topic is required", agent.ErrInvalidArgument)
	}
	messageID := data.ID
	if messageID == "" {
		messageID = envelope.ID
	}
	h.HandleBusMessage(sender, &BusMessage{
		Type: data.Type, Topic: data.Topic,
		TenantID: envelope.TenantID, Session: envelope.SessionID,
		ID: messageID, Name: data.Name, Text: data.Text, Data: data.Data, Meta: data.Meta,
		State: data.State, Input: data.Input, Output: data.Output, Artifact: data.Artifact,
		Truncated: data.Truncated, Context: data.Context, Tools: data.Tools, Payload: data.Payload,
		Action: data.Action, Reason: data.Reason,
	})
	return clientResponse(envelope, "result", envelope.Op, map[string]any{"accepted": true})
}

func (h *Hub) handleClientSessionCreate(ctx context.Context, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	var data sessionCreateData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, err
	}
	command := agent.CreateSession{
		Meta:              agent.CommandMeta{ID: envelope.ID, Actor: agent.ActorClient},
		TenantID:          key.TenantID,
		SessionID:         key.SessionID,
		DriverComponentID: data.DriverComponentID,
		AgentSpecRevision: data.AgentSpecRevision,
	}
	events, err := agent.Decide(agent.NewState(), command)
	if err != nil {
		return nil, err
	}
	projection, err := agent.ApplyAll(agent.NewState(), events)
	if err != nil {
		return nil, fmt.Errorf("apply session creation: %w", err)
	}
	digest, err := agent.DigestCommand(command)
	if err != nil {
		return nil, fmt.Errorf("digest session creation: %w", err)
	}
	result, err := h.agentSessions.Commit(ctx, agent.Commit{
		Key:             key,
		CommandID:       envelope.ID,
		CommandDigest:   digest,
		ExpectedVersion: 0,
		Events:          events,
		Projection:      projection,
	})
	if err != nil {
		return nil, err
	}
	// Session creation is durable before runtime work begins. The lifecycle
	// reconciler ensures absent drivers without delaying the client response.
	return clientResponse(envelope, "result", "session.create", map[string]any{
		"session_version": result.CommandVersion,
		"cursor":          fmt.Sprintf("cur_%d", result.CommandCursor),
		"duplicate":       result.Duplicate,
	})
}

func (h *Hub) handleClientSessionMutation(ctx context.Context, envelope *ClientEnvelope, key agent.SessionKey, command agent.Command) (*ClientEnvelope, error) {
	var data sessionMutationData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, err
	}
	if data.ExpectedSessionVersion == nil {
		return nil, fmt.Errorf("%w: expected_session_version is required", agent.ErrInvalidArgument)
	}
	record, err := h.agentSessions.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	if *data.ExpectedSessionVersion != record.Version {
		return nil, fmt.Errorf("%w: expected %d, current %d", agent.ErrVersionConflict, *data.ExpectedSessionVersion, record.Version)
	}
	events, err := agent.Decide(record.State, command)
	if err != nil {
		return nil, err
	}
	projection, err := agent.ApplyAll(record.State, events)
	if err != nil {
		return nil, fmt.Errorf("apply %s: %w", envelope.Op, err)
	}
	digest, err := agent.DigestCommand(command)
	if err != nil {
		return nil, fmt.Errorf("digest %s: %w", envelope.Op, err)
	}
	result, err := h.agentSessions.Commit(ctx, agent.Commit{
		Key:             key,
		CommandID:       envelope.ID,
		CommandDigest:   digest,
		ExpectedVersion: record.Version,
		Events:          events,
		Projection:      projection,
	})
	if err != nil {
		return nil, err
	}
	if envelope.Op == "session.delete" && h.driverSupervisor != nil {
		if stopErr := h.driverSupervisor.Stop(ctx, key); stopErr != nil {
			h.Logger.Debug("driver stop deferred after session deletion", "tenant_id", key.TenantID, "session_id", key.SessionID, "err", stopErr)
		}
	}
	return clientResponse(envelope, "result", envelope.Op, map[string]any{
		"session_version": result.CommandVersion,
		"cursor":          fmt.Sprintf("cur_%d", result.CommandCursor),
		"duplicate":       result.Duplicate,
	})
}

func (h *Hub) handleClientInputSubmit(ctx context.Context, sender *Client, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	var data inputSubmitData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: input.submit data: %v", agent.ErrInvalidArgument, err)
	}
	if data.InputID == "" || len(data.Content) == 0 {
		return nil, fmt.Errorf("%w: input_id and content are required", agent.ErrInvalidArgument)
	}
	content, sourceDigest, err := h.prepareInputContent(data.Content, key, data.InputID)
	if err != nil {
		return nil, err
	}
	acceptance, err := h.inputProcessor.Submit(ctx, agent.InputSubmitRequest{
		Key: key, CommandID: envelope.ID, InputID: data.InputID, Content: content, SourceDigest: sourceDigest,
		ActorID: sender.name, ExpectedVersion: data.ExpectedSessionVersion,
	})
	if err != nil {
		return nil, err
	}
	sender.subscribeAgentSession(key)
	// Delivery follows durable acceptance. Any runtime failure leaves acceptance
	// authoritative and is repaired by normal runtime reconciliation.
	if h.execution != nil {
		_ = h.execution.DeliverCurrent(ctx, key)
	}
	return clientResponse(envelope, "result", "input.accepted", clientInputAcceptance{
		InputID: acceptance.InputID, TurnID: acceptance.TurnID, Position: int(acceptance.Position),
		SessionVersion: acceptance.SessionVersion, Cursor: fmt.Sprintf("cur_%d", acceptance.Cursor),
	})
}

func (h *Hub) handleClientTurnCancel(ctx context.Context, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	var data turnData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: turn.cancel data: %v", agent.ErrInvalidArgument, err)
	}
	if data.TurnID == "" {
		return nil, fmt.Errorf("%w: turn_id is required", agent.ErrInvalidArgument)
	}
	request := agent.CancelRequest{Key: key, CommandID: envelope.ID, TurnID: data.TurnID, Actor: agent.ActorClient}
	result, err := h.recovery.CancelResult(ctx, request)
	if err != nil {
		return nil, err
	}
	record := result.Record
	turn := record.State.Turns[data.TurnID]
	if h.tools != nil && (turn.Status == agent.TurnCancelling || turn.Status == agent.TurnCancelled) {
		h.tools.CancelTurn(key.TenantID, key.SessionID, data.TurnID)
	}
	if !result.Duplicate && turn.Status == agent.TurnCancelled {
		h.dispatchAfterTurn(record, data.TurnID, "")
	}
	if h.execution != nil {
		// Coordinator sees duplicate durable command and only performs delivery.
		// Runtime unavailability cannot roll back or hide committed cancellation.
		_, _ = h.execution.Cancel(ctx, request)
	}
	return clientResponse(envelope, "result", envelope.Op, record)
}

func (h *Hub) handleClientRecovery(ctx context.Context, sender *Client, envelope *ClientEnvelope, key agent.SessionKey, action agent.RecoveryAction) (*ClientEnvelope, error) {
	if !sender.hasRecoveryAuthority() {
		return nil, agent.ErrPermissionDenied
	}
	var data recoveryData
	if action == agent.RecoveryResume {
		if err := decodeStrict(envelope.Data, &data); err != nil {
			return nil, fmt.Errorf("%w: %s data: %v", agent.ErrInvalidArgument, envelope.Op, err)
		}
	} else {
		var turn turnData
		if err := decodeStrict(envelope.Data, &turn); err != nil {
			return nil, fmt.Errorf("%w: %s data: %v", agent.ErrInvalidArgument, envelope.Op, err)
		}
		data.TurnID = turn.TurnID
	}
	if data.TurnID == "" {
		return nil, fmt.Errorf("%w: turn_id is required", agent.ErrInvalidArgument)
	}
	result, err := h.recovery.RecoverResult(ctx, agent.RecoveryRequest{
		Key: key, CommandID: envelope.ID, TurnID: data.TurnID, Actor: agent.ActorHuman,
		Action: action, Fence: data.Fence, ReconciliationEvidence: data.ReconciliationEvidence,
	})
	if err != nil {
		return nil, err
	}
	record := result.Record
	if !result.Duplicate && action == agent.RecoveryDiscard {
		h.dispatchAfterTurn(record, data.TurnID, "")
	}
	if h.execution != nil {
		_ = h.execution.DeliverCurrent(ctx, key)
	}
	return clientResponse(envelope, "result", envelope.Op, record)
}

func (h *Hub) handleClientSessionGet(ctx context.Context, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	var data struct{}
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: session.get data: %v", agent.ErrInvalidArgument, err)
	}
	record, err := h.agentSessions.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	return clientResponse(envelope, "reply", envelope.Op, struct {
		Projection map[string]any `json:"projection"`
		Cursor     string         `json:"cursor"`
	}{Projection: clientProjection(record.State, record.Cursor), Cursor: fmt.Sprintf("cur_%d", record.Cursor)})
}

func (h *Hub) handleClientSessionList(ctx context.Context, envelope *ClientEnvelope) (*ClientEnvelope, error) {
	var data sessionListData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: session.list data: %v", agent.ErrInvalidArgument, err)
	}
	limit := data.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 256 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 256", agent.ErrInvalidArgument)
	}
	page, err := h.agentSessions.List(ctx, agent.SessionQuery{
		TenantID:        envelope.TenantID,
		Statuses:        data.Statuses,
		IncludeArchived: data.IncludeArchived,
		AfterSessionID:  data.AfterSessionID,
		Limit:           limit,
	})
	if err != nil {
		return nil, err
	}
	sessions := make([]map[string]any, 0, len(page.Records))
	for _, record := range page.Records {
		sessions = append(sessions, clientProjection(record.State, record.Cursor))
	}
	return clientResponse(envelope, "reply", envelope.Op, map[string]any{
		"sessions":              sessions,
		"next_after_session_id": page.NextAfterSessionID,
	})
}

func (h *Hub) handleClientSessionSubscribe(ctx context.Context, sender *Client, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	var data subscribeData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: session.subscribe data: %v", agent.ErrInvalidArgument, err)
	}
	if data.AfterCursor == "" {
		return nil, fmt.Errorf("%w: after_cursor is required", agent.ErrInvalidArgument)
	}
	after, err := parseClientCursor(data.AfterCursor)
	if err != nil {
		return nil, fmt.Errorf("%w: after_cursor: %v", agent.ErrInvalidArgument, err)
	}
	limit := defaultClientEventLimit
	if data.Limit != nil {
		limit = *data.Limit
	}
	if limit < 1 || limit > 256 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 256", agent.ErrInvalidArgument)
	}
	events, err := h.agentSessions.ReadEvents(ctx, key, after, limit)
	if err != nil {
		return nil, err
	}
	encoded := make([]clientStoredEvent, 0, len(events))
	for _, event := range events {
		stored, err := encodeClientStoredEvent(event)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, stored)
	}
	sender.subscribeAgentSession(key)
	return clientResponse(envelope, "reply", envelope.Op, struct {
		Events []clientStoredEvent `json:"events"`
	}{Events: encoded})
}

func (h *Hub) handleClientSessionRecordAppend(ctx context.Context, sender *Client, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	if h.sessionRecords == nil {
		return nil, errors.New("session record storage unavailable")
	}
	var data sessionRecordAppendData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: session.record.append data: %v", agent.ErrInvalidArgument, err)
	}
	stored, err := h.sessionRecords.Append(ctx, sessionrecord.Record{
		TenantID: key.TenantID, SessionID: key.SessionID, Kind: strings.TrimSpace(data.Kind),
		Producer: "client:" + sender.name, Payload: data.Payload,
	})
	if err != nil {
		return nil, err
	}
	return clientResponse(envelope, "result", envelope.Op, clientSessionRecordValue(stored))
}

func (h *Hub) handleClientSessionRecordList(ctx context.Context, envelope *ClientEnvelope, key agent.SessionKey) (*ClientEnvelope, error) {
	if h.sessionRecords == nil {
		return nil, errors.New("session record storage unavailable")
	}
	var data sessionRecordListData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: session.record.list data: %v", agent.ErrInvalidArgument, err)
	}
	limit := data.Limit
	if limit == 0 {
		limit = 100
	}
	records, err := h.sessionRecords.Read(ctx, sessionrecord.Query{
		TenantID: key.TenantID, SessionID: key.SessionID, Kind: strings.TrimSpace(data.Kind),
		AfterID: data.AfterID, BeforeID: data.BeforeID, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	encoded := make([]clientSessionRecord, 0, len(records))
	for _, record := range records {
		encoded = append(encoded, clientSessionRecordValue(record))
	}
	return clientResponse(envelope, "reply", envelope.Op, struct {
		Records []clientSessionRecord `json:"records"`
	}{Records: encoded})
}

func clientSessionRecordValue(record sessionrecord.StoredRecord) clientSessionRecord {
	return clientSessionRecord{
		ID: record.ID, Kind: record.Kind, Producer: record.Producer,
		Payload: append(json.RawMessage(nil), record.Payload...), CreatedAt: record.CreatedAt.Format(time.RFC3339Nano),
	}
}

func parseClientCursor(value string) (agent.Cursor, error) {
	if !strings.HasPrefix(value, "cur_") {
		return 0, errors.New("must have cur_ prefix")
	}
	var cursor uint64
	if _, err := fmt.Sscanf(value, "cur_%d", &cursor); err != nil {
		return 0, errors.New("must contain a non-negative integer")
	}
	if value != fmt.Sprintf("cur_%d", cursor) {
		return 0, errors.New("must be canonical")
	}
	return agent.Cursor(cursor), nil
}

func encodeClientStoredEvent(event agent.StoredEvent) (clientStoredEvent, error) {
	typ, payload, err := encodeClientEvent(event.Event.Body)
	if err != nil {
		return clientStoredEvent{}, err
	}
	return clientStoredEvent{
		EventID:        event.ID,
		Cursor:         fmt.Sprintf("cur_%d", event.Cursor),
		SessionVersion: event.Version,
		Type:           typ,
		OccurredAt:     fmt.Sprintf("ver_%d", event.Version),
		Data:           payload,
	}, nil
}

func (h *Hub) publishClientStoredEvent(key agent.SessionKey, event agent.StoredEvent) {
	stored, err := encodeClientStoredEvent(event)
	if err != nil {
		h.Logger.Error("encode committed client event", "tenant_id", key.TenantID, "session_id", key.SessionID, "event_id", event.ID, "err", err)
		return
	}
	data, err := json.Marshal(stored)
	if err != nil {
		h.Logger.Error("marshal committed client event", "tenant_id", key.TenantID, "session_id", key.SessionID, "event_id", event.ID, "err", err)
		return
	}
	delivered := 0
	for _, client := range h.allClients() {
		if !client.IsConnected() || !client.subscribesAgentSession(key) {
			continue
		}
		if client.SendEnvelope(&ClientEnvelope{
			V: ClientProtocolVersion, Kind: "event", Op: stored.Type, ID: stored.EventID,
			TenantID: key.TenantID, SessionID: key.SessionID, Data: data,
		}) {
			delivered++
		}
	}
	h.Logger.Debug("committed client event published", "tenant_id", key.TenantID, "session_id", key.SessionID, "event_id", event.ID, "type", stored.Type, "delivered", delivered)
}

func clientProjection(state agent.State, cursor agent.Cursor) map[string]any {
	turns := make([]map[string]any, 0, len(state.Turns))
	for _, turnID := range state.Queue {
		turn, ok := state.Turns[turnID]
		if !ok {
			continue
		}
		turns = append(turns, clientTurn(turn))
	}
	if state.ActiveTurnID != "" {
		if turn, ok := state.Turns[state.ActiveTurnID]; ok && !turnInList(turns, turn.ID) {
			turns = append(turns, clientTurn(turn))
		}
	}
	for turnID, turn := range state.Turns {
		if turnInList(turns, turnID) {
			continue
		}
		turns = append(turns, clientTurn(turn))
	}
	return map[string]any{
		"tenant_id":       state.TenantID,
		"session_id":      state.SessionID,
		"session_version": state.Version,
		"cursor":          fmt.Sprintf("cur_%d", cursor),
		"state":           clientSessionState(state),
		"execution_state": clientExecutionState(state),
		"turns":           turns,
	}
}

func clientTurn(turn agent.Turn) map[string]any {
	value := map[string]any{
		"turn_id":  turn.ID,
		"position": turn.Position,
		"input_id": turn.InputID,
		"state":    string(turn.Status),
	}
	if turn.ActiveAttemptID != "" {
		value["attempt_id"] = turn.ActiveAttemptID
	}
	if turn.CancellationRequested {
		value["cancellation_requested"] = true
	}
	if turn.RecoveryReason != "" {
		value["recovery_reason"] = turn.RecoveryReason
	}
	return value
}

func turnInList(turns []map[string]any, turnID string) bool {
	for _, turn := range turns {
		if turn["turn_id"] == turnID {
			return true
		}
	}
	return false
}

func clientSessionState(state agent.State) string {
	switch state.Status {
	case agent.SessionOpen:
		if state.Archived {
			return "suspended"
		}
		return "open"
	case agent.SessionSuspended:
		return "suspended"
	case agent.SessionClosed:
		return "closed"
	default:
		return "open"
	}
}

func clientExecutionState(state agent.State) string {
	if state.ActiveTurnID != "" {
		if turn, ok := state.Turns[state.ActiveTurnID]; ok {
			switch turn.Status {
			case agent.TurnPreparing:
				return "preparing"
			case agent.TurnExecuting:
				return "executing"
			case agent.TurnCancelling:
				return "cancelling"
			case agent.TurnRecoveryRequired:
				return "recovery_required"
			}
		}
	}
	if len(state.Queue) > 0 {
		if state.Driver.Status == agent.DriverSuspect {
			return "degraded"
		}
		return "queued"
	}
	if state.Driver.Status == agent.DriverSuspect {
		return "degraded"
	}
	return "idle"
}

func clientResponse(request *ClientEnvelope, kind, op string, data any) (*ClientEnvelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode %s response: %w", request.Op, err)
	}
	return &ClientEnvelope{V: ClientProtocolVersion, Kind: kind, Op: op, ID: request.ID, TenantID: request.TenantID, SessionID: request.SessionID, Data: raw}, nil
}

func (h *Hub) sendClientError(sender *Client, request *ClientEnvelope, err error) {
	if sender == nil {
		return
	}
	data := clientError(err)
	if request != nil && request.TenantID != "" && request.SessionID != "" && data.SessionVersion == nil && h.agentSessions != nil && !errors.Is(err, agent.ErrPermissionDenied) {
		if record, loadErr := h.agentSessions.Load(context.Background(), agent.SessionKey{TenantID: request.TenantID, SessionID: request.SessionID}); loadErr == nil {
			data.SessionVersion = &record.Version
		}
	}
	raw, _ := json.Marshal(data)
	response := &ClientEnvelope{V: ClientProtocolVersion, Kind: "error", Data: raw}
	if request != nil {
		response.Op, response.ID, response.TenantID, response.SessionID = request.Op, request.ID, request.TenantID, request.SessionID
	}
	sender.SendEnvelope(response)
}

func clientError(err error) clientErrorData {
	data := clientErrorData{Code: "internal", Message: err.Error()}
	switch {
	case errors.Is(err, errClientAuthentication):
		data.Code = "authentication_failed"
	case errors.Is(err, agent.ErrInvalidArgument), errors.Is(err, sessionrecord.ErrInvalidArgument):
		data.Code = "invalid_argument"
	case errors.Is(err, agent.ErrPermissionDenied):
		data.Code = "permission_denied"
	case errors.Is(err, agent.ErrCursorExpired):
		data.Code, data.SnapshotRequired = "cursor_expired", true
		var expired *agent.CursorExpiredError
		if errors.As(err, &expired) {
			data.RetainedAfterCursor = fmt.Sprintf("cur_%d", expired.RetainedAfter)
		}
	case errors.Is(err, agent.ErrNotFound), errors.Is(err, sessionrecord.ErrNotFound):
		data.Code = "not_found"
	case errors.Is(err, agent.ErrInputConflict), errors.Is(err, agent.ErrCommandConflict), errors.Is(err, agent.ErrOutputConflict):
		data.Code = "conflict"
	case errors.Is(err, agent.ErrVersionConflict):
		data.Code, data.Retryable = "version_conflict", true
	case errors.Is(err, agent.ErrInvalidTransition):
		data.Code = "invalid_transition"
	case errors.Is(err, agent.ErrStaleDriver):
		data.Code = "stale_driver"
	case errors.Is(err, agent.ErrSessionClosed):
		data.Code = "session_closed"
	case errors.Is(err, ErrExecutionRuntimeUnavailable), errors.Is(err, ErrExecutionDeliveryRejected), errors.Is(err, agent.ErrNoAssignableTurn):
		data.Code, data.Retryable = "runtime_unavailable", true
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		data.Code, data.Retryable = "storage_unavailable", true
	case strings.Contains(err.Error(), "repository unavailable"), strings.Contains(err.Error(), "storage"):
		data.Code, data.Retryable = "storage_unavailable", true
	}
	return data
}

func decodeStrict(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return errors.New("JSON object is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func encodeClientEvent(event agent.Event) (string, json.RawMessage, error) {
	payload, err := encodeClientEventData(event)
	if err != nil {
		return "", nil, err
	}
	switch typed := event.(type) {
	case agent.SessionCreated:
		return "session.state_changed", payload, nil
	case agent.SessionArchived, agent.SessionUnarchived, agent.SessionSuspendedEvent, agent.SessionResumedEvent, agent.SessionDeletedEvent:
		return "session.state_changed", payload, nil
	case agent.InputSubmitted:
		return "input.accepted", payload, nil
	case agent.DriverLeaseGranted, agent.DriverBecameReady, agent.DriverLeaseRenewed, agent.DriverBecameSuspect, agent.DriverLeaseReleased, agent.DriverLeaseExpired:
		return "session.state_changed", payload, nil
	case agent.AttemptAssignedEvent, agent.AttemptPreparedContextSet, agent.AttemptPreparedEvent, agent.AttemptPermittedEvent, agent.AttemptCompletedEvent, agent.AttemptFailedEvent, agent.AttemptAbandonedEvent, agent.AttemptUncertainEvent, agent.AttemptCancelledEvent, agent.AttemptResumedEvent, agent.AttemptSupersededEvent, agent.TurnCancellationRequested, agent.TurnDiscardedEvent:
		_ = typed
		return "turn.state_changed", payload, nil
	case agent.AttemptOutputAppended:
		switch typed.Output.Type {
		case "stream.delta", "reasoning", "usage", "provider.retry", "provider.error", "compaction", "tool.result":
			return typed.Output.Type, payload, nil
		default:
			return "stream.delta", payload, nil
		}
	default:
		return "", nil, fmt.Errorf("%w: unsupported event %T", agent.ErrCorruptState, event)
	}
}

func encodeClientEventData(event agent.Event) (json.RawMessage, error) {
	var payload any
	switch typed := event.(type) {
	case agent.SessionCreated:
		payload = map[string]any{"state": "open"}
	case agent.SessionArchived:
		payload = map[string]any{"state": "suspended", "archived": true}
	case agent.SessionUnarchived:
		payload = map[string]any{"state": "open", "archived": false}
	case agent.SessionSuspendedEvent:
		payload = map[string]any{"state": "suspended"}
	case agent.SessionResumedEvent:
		payload = map[string]any{"state": "open"}
	case agent.SessionDeletedEvent:
		payload = map[string]any{"state": "closed"}
	case agent.InputSubmitted:
		payload = map[string]any{
			"turn_id":  typed.Turn.ID,
			"input_id": typed.Input.ID,
			"position": typed.Turn.Position,
			"state":    string(typed.Turn.Status),
			"content":  json.RawMessage(typed.Input.Content),
		}
	case agent.DriverLeaseGranted:
		payload = map[string]any{"execution_state": "queued", "driver_status": string(typed.Driver.Status)}
	case agent.DriverBecameReady:
		payload = map[string]any{"execution_state": "queued", "driver_status": string(agent.DriverReady)}
	case agent.DriverLeaseRenewed:
		payload = map[string]any{"execution_state": "queued", "driver_status": string(agent.DriverReady)}
	case agent.DriverBecameSuspect:
		payload = map[string]any{"execution_state": "degraded", "driver_status": string(agent.DriverSuspect)}
	case agent.DriverLeaseReleased, agent.DriverLeaseExpired:
		payload = map[string]any{"execution_state": "queued", "driver_status": string(agent.DriverAbsent)}
	case agent.AttemptAssignedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.Attempt.ID, "state": "preparing"}
	case agent.AttemptPreparedContextSet:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "preparing"}
	case agent.AttemptPreparedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "preparing"}
	case agent.AttemptPermittedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "executing"}
	case agent.AttemptOutputAppended:
		payload = map[string]any{
			"turn_id":     typed.TurnID,
			"attempt_id":  typed.AttemptID,
			"output_type": typed.Output.Type,
			"sequence":    typed.Output.Sequence,
			"payload":     json.RawMessage(typed.Output.Payload),
		}
	case agent.AttemptCompletedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "completed"}
	case agent.AttemptFailedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "failed", "reason": typed.Reason, "retryable": typed.Retryable}
	case agent.AttemptAbandonedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "failed", "reason": typed.Reason}
	case agent.AttemptUncertainEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "recovery_required", "reason": typed.Reason, "reconciliation_evidence": typed.ReconciliationEvidence}
	case agent.TurnCancellationRequested:
		payload = map[string]any{"turn_id": typed.TurnID, "state": "cancelling"}
	case agent.AttemptCancelledEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "cancelled"}
	case agent.AttemptResumedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "executing", "reconciliation_evidence": typed.Evidence}
	case agent.AttemptSupersededEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "attempt_id": typed.AttemptID, "state": "queued"}
	case agent.TurnDiscardedEvent:
		payload = map[string]any{"turn_id": typed.TurnID, "state": "discarded"}
	default:
		return nil, fmt.Errorf("%w: unsupported event %T", agent.ErrCorruptState, event)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
