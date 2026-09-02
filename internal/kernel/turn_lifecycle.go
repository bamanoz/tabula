package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bamanoz/tabula/internal/agent"
)

func (h *Hub) prepareInputContent(content json.RawMessage, key agent.SessionKey, inputID string) (json.RawMessage, string, error) {
	sourceDigest, err := agent.DigestJSON(content)
	if err != nil {
		return nil, "", fmt.Errorf("%w: invalid input content: %v", agent.ErrInvalidArgument, err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(content, &original); err != nil || original == nil {
		return nil, "", fmt.Errorf("%w: input content must be an object", agent.ErrInvalidArgument)
	}
	payload := cloneRawMap(original)
	payload["input_id"] = mustRawJSON(inputID)
	payload["session"] = mustRawJSON(key.SessionID)
	payload["tenant_id"] = mustRawJSON(key.TenantID)
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	rewritten, ok := h.dispatchHook("before_message", rawPayload, key.TenantID, key.SessionID)
	if !ok {
		return nil, "", fmt.Errorf("%w: before_message hook blocked input", agent.ErrPermissionDenied)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &result); err != nil || result == nil {
		return nil, "", fmt.Errorf("%w: before_message hook returned invalid payload", agent.ErrInvalidArgument)
	}
	text, exists := result["text"]
	if !exists {
		return append(json.RawMessage(nil), content...), sourceDigest, nil
	}
	var value string
	if err := json.Unmarshal(text, &value); err != nil {
		return nil, "", fmt.Errorf("%w: before_message text must be a string", agent.ErrInvalidArgument)
	}
	original["text"] = text
	rewrittenContent, err := json.Marshal(original)
	if err != nil {
		return nil, "", err
	}
	return rewrittenContent, sourceDigest, nil
}

func summarizeTurnToolCatalog(raw json.RawMessage) (toolCount, schemaCount int, digest string) {
	var tools []struct {
		Name   string `json:"name"`
		Schema any    `json:"schema"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		sum := sha256.Sum256(raw)
		return 0, 0, hex.EncodeToString(sum[:])
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, tool := range tools {
		if tool.Schema != nil {
			schemaCount++
		}
	}
	canonical, _ := json.Marshal(tools)
	sum := sha256.Sum256(canonical)
	return len(tools), schemaCount, hex.EncodeToString(sum[:])
}

func (h *Hub) prepareTurnContext(assignment agent.Assignment) (json.RawMessage, error) {
	promptContext := ""
	if session, ok := h.sessions.Get(assignment.Key.SessionID, assignment.Key.TenantID); ok {
		promptContext = session.GetInitContext()
	}
	tools := h.initToolsJSON(assignment.Key.TenantID)
	promptContext, tools = h.policy.BeforePromptBuild(
		assignment.Key.SessionID,
		assignment.Key.TenantID,
		"driver:"+assignment.Fence.DriverInstanceID,
		promptContext,
		tools,
		h.initMetaJSON(assignment.Key.TenantID),
	)
	toolCount, schemaCount, catalogDigest := summarizeTurnToolCatalog(tools)
	h.Logger.Debug("turn tool catalog prepared",
		"tenant_id", assignment.Key.TenantID,
		"session_id", assignment.Key.SessionID,
		"turn_id", assignment.TurnID,
		"attempt_id", assignment.AttemptID,
		"tool_count", toolCount,
		"schema_count", schemaCount,
		"catalog_digest", catalogDigest,
	)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(assignment.Input, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("%w: assigned input must be an object", agent.ErrCorruptState)
	}
	payload = cloneRawMap(payload)
	correlation := map[string]any{
		turnIDMetaKey:           assignment.TurnID,
		attemptIDMetaKey:        assignment.AttemptID,
		driverInstanceMetaKey:   assignment.Fence.DriverInstanceID,
		leaseIDMetaKey:          assignment.Fence.LeaseID,
		driverGenerationMetaKey: assignment.Fence.Generation,
		turnCorrelationMetaKey:  assignment.TurnCorrelationID,
	}
	for key, value := range correlation {
		payload[key] = mustRawJSON(value)
	}
	payload["session"] = mustRawJSON(assignment.Key.SessionID)
	payload["tenant_id"] = mustRawJSON(assignment.Key.TenantID)
	payload["meta"] = mustRawJSON(correlation)
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	rewritten, ok := h.dispatchHook("before_turn", rawPayload, assignment.Key.TenantID, assignment.Key.SessionID)
	if !ok {
		return nil, fmt.Errorf("%w: before_turn hook blocked assignment", agent.ErrPermissionDenied)
	}
	var result struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(rewritten, &result); err != nil {
		return nil, fmt.Errorf("%w: before_turn hook returned invalid payload", agent.ErrInvalidArgument)
	}
	return json.Marshal(struct {
		PromptContext string          `json:"prompt_context,omitempty"`
		Tools         json.RawMessage `json:"tools"`
		TurnContext   string          `json:"turn_context,omitempty"`
	}{
		PromptContext: promptContext,
		Tools:         tools,
		TurnContext:   result.Context,
	})
}

func (h *Hub) dispatchAfterTurn(record agent.Record, turnID, reason string) {
	turn, ok := record.State.Turns[turnID]
	if !ok {
		return
	}
	payload := map[string]any{
		"tenant_id":           record.Key.TenantID,
		"session":             record.Key.SessionID,
		"turn_id":             turn.ID,
		"turn_correlation_id": turn.ID,
		"status":              turn.Status,
	}
	if reason == "" {
		reason = turn.RecoveryReason
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if len(turn.Attempts) != 0 {
		attempt := turn.Attempts[len(turn.Attempts)-1]
		payload["attempt_id"] = attempt.ID
		payload["driver_instance_id"] = attempt.DriverInstanceID
		payload["lease_id"] = attempt.LeaseID
		payload["driver_generation"] = attempt.DriverGeneration
		payload["meta"] = map[string]any{
			turnIDMetaKey:           turn.ID,
			attemptIDMetaKey:        attempt.ID,
			driverInstanceMetaKey:   attempt.DriverInstanceID,
			leaseIDMetaKey:          attempt.LeaseID,
			driverGenerationMetaKey: attempt.DriverGeneration,
			turnCorrelationMetaKey:  turn.ID,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.dispatchHook("after_turn", raw, record.Key.TenantID, record.Key.SessionID)
}

func cloneRawMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	clone := make(map[string]json.RawMessage, len(source)+4)
	for key, value := range source {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func mustRawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
