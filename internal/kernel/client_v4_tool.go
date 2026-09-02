package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bamanoz/tabula/internal/agent"
	"github.com/bamanoz/tabula/internal/kernel/clientmeta"
)

type clientToolCallData struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	Meta  json.RawMessage `json:"meta,omitempty"`
}

type clientToolResultData struct {
	Name      string          `json:"name"`
	Output    string          `json:"output"`
	Artifact  json.RawMessage `json:"artifact,omitempty"`
	Truncated bool            `json:"truncated"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

type clientToolResultEventData struct {
	Name  string          `json:"name"`
	Text  string          `json:"text,omitempty"`
	State string          `json:"state"`
	Meta  json.RawMessage `json:"meta,omitempty"`
}

type v4ToolCallKey struct {
	tenantID string
	session  string
	callID   string
}

type pendingV4ToolCall struct {
	client      *Client
	name        string
	correlation toolAttemptContext
}

// HandleClientToolCall starts one protocol-v4 tool.call command. Completion is
// asynchronous: streamed frames are sent as event envelopes and the terminal
// tool.result is sent as the command's result envelope.
func (h *Hub) HandleClientToolCall(sender *Client, envelope *ClientEnvelope) error {
	if h == nil || h.tools == nil || sender == nil || envelope == nil {
		return fmt.Errorf("%w: tool call bridge is unavailable", agent.ErrInvalidArgument)
	}
	if envelope.V != ClientProtocolVersion || envelope.Kind != "command" || envelope.Op != TopicToolCall {
		return fmt.Errorf("%w: expected protocol-v4 tool.call command", agent.ErrInvalidArgument)
	}
	if strings.TrimSpace(envelope.ID) == "" || strings.TrimSpace(envelope.TenantID) == "" || strings.TrimSpace(envelope.SessionID) == "" {
		return fmt.Errorf("%w: id, tenant_id, and session_id are required", agent.ErrInvalidArgument)
	}
	if sender.tenantID != "" && sender.tenantID != envelope.TenantID {
		return agent.ErrPermissionDenied
	}
	if sender.session != "" && sender.session != envelope.SessionID {
		return agent.ErrPermissionDenied
	}

	var data clientToolCallData
	if err := decodeStrict(envelope.Data, &data); err != nil {
		return fmt.Errorf("%w: invalid tool.call data: %v", agent.ErrInvalidArgument, err)
	}
	data.Name = strings.TrimSpace(data.Name)
	if data.Name == "" {
		return fmt.Errorf("%w: tool name is required", agent.ErrInvalidArgument)
	}
	if err := requireJSONObject("input", data.Input); err != nil {
		return err
	}
	if len(data.Meta) != 0 {
		if err := requireJSONObject("meta", data.Meta); err != nil {
			return err
		}
	}
	correlation, err := toolAttemptContextFromMeta(data.Meta)
	if err != nil {
		return err
	}
	if clientmeta.Decode(sender.meta).Role == "driver" && correlation.empty() {
		return fmt.Errorf("%w: driver tool calls require complete attempt fence metadata", agent.ErrInvalidArgument)
	}
	if err := h.validateToolAttempt(envelope.TenantID, envelope.SessionID, correlation); err != nil {
		return err
	}

	key := v4ToolCallKey{tenantID: envelope.TenantID, session: envelope.SessionID, callID: envelope.ID}
	if err := h.tools.registerV4ToolCall(key, sender, data.Name, correlation); err != nil {
		return err
	}
	msg := &BusMessage{ID: envelope.ID, Name: data.Name, Input: append(json.RawMessage(nil), data.Input...), Meta: append(json.RawMessage(nil), data.Meta...)}
	if err := h.tools.HandleScopedToolCall(sender, envelope.TenantID, envelope.SessionID, msg); err != nil {
		h.tools.removeV4ToolCall(key, sender)
		return err
	}
	return nil
}

func requireJSONObject(field string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return fmt.Errorf("%w: %s must be a JSON object", agent.ErrInvalidArgument, field)
	}
	var value map[string]any
	if err := decodeStrict(trimmed, &value); err != nil {
		return fmt.Errorf("%w: invalid %s: %v", agent.ErrInvalidArgument, field, err)
	}
	return nil
}

func (s *ToolService) registerV4ToolCall(key v4ToolCallKey, client *Client, name string, correlation toolAttemptContext) error {
	if s == nil || client == nil {
		return fmt.Errorf("%w: tool call bridge is unavailable", agent.ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.v4PendingCalls[key]; exists {
		return fmt.Errorf("%w: tool call id %q is already pending", agent.ErrCommandConflict, key.callID)
	}
	s.v4PendingCalls[key] = &pendingV4ToolCall{client: client, name: name, correlation: correlation}
	return nil
}

func (s *ToolService) removeV4ToolCall(key v4ToolCallKey, client *Client) {
	if s == nil {
		return
	}
	s.mu.Lock()
	pending := s.v4PendingCalls[key]
	if pending != nil && (client == nil || pending.client == client) {
		delete(s.v4PendingCalls, key)
	}
	s.mu.Unlock()
}

func (s *ToolService) cancelV4ToolCallsForClient(client *Client) {
	if s == nil || client == nil {
		return
	}
	s.mu.Lock()
	for key, pending := range s.v4PendingCalls {
		if pending.client == client {
			delete(s.v4PendingCalls, key)
		}
	}
	s.mu.Unlock()
}

func (s *ToolService) sendV4ToolResultEvent(tenantID, session, callID, name, topic, text, state string, correlation toolAttemptContext) bool {
	if s == nil {
		return false
	}
	key := v4ToolCallKey{tenantID: tenantID, session: session, callID: callID}
	s.mu.Lock()
	pending := s.v4PendingCalls[key]
	if pending == nil || pending.correlation != correlation {
		s.mu.Unlock()
		return false
	}
	client := pending.client
	if name == "" {
		name = pending.name
	}
	s.mu.Unlock()
	if client == nil || !client.IsConnected() {
		s.removeV4ToolCall(key, client)
		return false
	}
	raw, err := json.Marshal(clientToolResultEventData{Name: name, Text: text, State: state, Meta: correlation.meta()})
	if err != nil {
		return false
	}
	return client.SendEnvelope(&ClientEnvelope{V: ClientProtocolVersion, Kind: "event", Op: topic, ID: callID, TenantID: tenantID, SessionID: session, Data: raw})
}

func (s *ToolService) sendV4ToolResult(tenantID, session, callID, name, output string, artifact json.RawMessage, truncated bool, correlation toolAttemptContext) bool {
	if s == nil {
		return false
	}
	key := v4ToolCallKey{tenantID: tenantID, session: session, callID: callID}
	s.mu.Lock()
	pending := s.v4PendingCalls[key]
	if pending == nil || pending.correlation != correlation {
		s.mu.Unlock()
		return false
	}
	delete(s.v4PendingCalls, key)
	client := pending.client
	if name == "" {
		name = pending.name
	}
	s.mu.Unlock()
	if client == nil || !client.IsConnected() {
		return false
	}
	raw, err := json.Marshal(clientToolResultData{Name: name, Output: output, Artifact: artifact, Truncated: truncated, Meta: correlation.meta()})
	if err != nil {
		return false
	}
	return client.SendEnvelope(&ClientEnvelope{V: ClientProtocolVersion, Kind: "result", Op: TopicToolResult, ID: callID, TenantID: tenantID, SessionID: session, Data: raw})
}

func (s *ToolService) pendingV4ToolCallCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.v4PendingCalls)
}
