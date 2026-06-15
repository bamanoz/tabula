package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/toolresult"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const maxExecOutput = 16 * 1024 // 16KB
const toolResultInlineLimitBytes = 12000
const toolResultSpoolPreviewBytes = 4096
const toolResultSpoolIdleTimeout = 30 * time.Second
const toolResultSpoolStaleAge = 24 * time.Hour

type invokeResultSpoolState string

const (
	invokeResultSpoolOpen          invokeResultSpoolState = "open"
	invokeResultSpoolCompleted     invokeResultSpoolState = "completed"
	invokeResultSpoolFailed        invokeResultSpoolState = "failed"
	invokeResultSpoolCancelled     invokeResultSpoolState = "cancelled"
	invokeResultSpoolTimedOut      invokeResultSpoolState = "timed_out"
	invokeResultSpoolProtocolError invokeResultSpoolState = "protocol_error"
)

type ToolService struct {
	hub *Hub
}

func NewToolService(hub *Hub) *ToolService {
	return &ToolService{hub: hub}
}

// HandleToolUse routes a tool.call request through the before_tool_call hook
// and dispatches the result to a runtime-hosted tool.
//
// The kernel does not host builtin LLM tools. Runtime tools are dispatched
// through the unified tool registry populated by attached runtime targets.
func (s *ToolService) HandleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session
	tenantID := sender.tenantID

	s.hub.Logger.Debug("tool.call", "tool", toolName, "tenant_id", tenantID, "session", session, "id", toolID)

	effectiveInput, blocked := s.hub.policy.CanUseTool(sender, toolName, toolID, msg.Input, msg.Meta, session)
	if blocked != nil {
		s.hub.Logger.Warn("tool call blocked by policy", "tool", toolName, "tool_call_id", toolID, "tenant_id", tenantID, "session", session, "input_bytes", len(msg.Input))
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, buildNotInvokedToolResult(blocked), nil, false)
		return
	}
	msg.Input = effectiveInput
	s.hub.broadcastToSessionFrom(tenantID, session, TopicToolCall, &Message{
		Type:  string(MsgRequest),
		Topic: TopicToolCall,
		ID:    toolID,
		Name:  toolName,
		Input: msg.Input,
		Meta:  msg.Meta,
	}, sender, sender)

	s.handleDynamicTool(tenantID, session, toolID, toolName, msg.Input)
}

func (s *ToolService) handleDynamicTool(tenantID, session, toolID, toolName string, input json.RawMessage) {
	s.hub.toolExecMu.RLock()
	entry, ok := s.hub.toolExec[toolExecKey(tenantID, toolName)]
	if !ok {
		entry, ok = s.hub.toolExec[toolName]
	}
	s.hub.toolExecMu.RUnlock()
	if !ok {
		count, visibleElsewhere := s.hub.toolDispatchDiagnostics(tenantID, toolName)
		s.hub.Logger.Warn(
			"unknown tool dispatch",
			"tool", toolName,
			"tool_call_id", toolID,
			"tenant_id", tenantID,
			"session", session,
			"tool_registry_entries", count,
			"tool_visible_in_other_tenant", visibleElsewhere,
		)
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: unknown tool %s", toolName), nil, false)
		return
	}
	switch entry.Source {
	case toolSourceRuntime:
		s.handleRuntimeTool(tenantID, session, toolID, toolName, entry, input)
	default:
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: tool %s has unknown dispatch source", toolName), nil, false)
	}
}

func buildNotInvokedToolResult(blocked *HookDispatchDecision) string {
	result := map[string]any{
		"ok":    false,
		"error": "not_invoked",
		"hook": map[string]any{
			"event":        blocked.Event,
			"target":       blocked.Target,
			"hook_id":      blocked.HookID,
			"reply_action": blocked.ReplyAction,
			"reason":       blocked.Reason,
			"status":       blocked.Status,
		},
	}
	if len(blocked.Payload) > 0 {
		var details any
		if json.Unmarshal(blocked.Payload, &details) == nil {
			result["hook"].(map[string]any)["details"] = details
		}
	}
	return string(mustMarshalRaw(result))
}
func (s *ToolService) handleRuntimeTool(tenantID, session, toolID, toolName string, entry toolDispatch, input json.RawMessage) {
	go func() {
		var conn runtimeapi.RuntimeConn
		pickedRuntimeID := entry.RuntimeID
		var code wire.ErrorCode
		var pickErr error
		if pickedRuntimeID == "" {
			conn, pickedRuntimeID, code, pickErr = s.hub.pickRuntime(tenantID)
		} else {
			conn, code, pickErr = s.hub.runtimeForTenant(tenantID, pickedRuntimeID)
		}
		if pickErr != nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: "+pickErr.Error(), nil, false)
			if code != "" {
				s.hub.Logger.Warn("runtime pick failed", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "code", code, "err", pickErr)
			}
			return
		}
		if conn == nil {
			s.hub.Logger.Warn("runtime tool unavailable", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID)
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s is unavailable", toolName), nil, false)
			return
		}
		releaseRuntimeTarget := s.hub.markRuntimeTargetBusy(pickedRuntimeID, entry.Target)
		defer releaseRuntimeTarget()
		deadline := resolveToolDeadline(entry.DeadlineMs)
		ctx, cancel := context.WithTimeout(context.Background(), runtimeInvokeDeadline(deadline))
		defer cancel()
		spool, err := newInvokeResultSpool(s.hub, tenantID, session, toolID)
		if err != nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err), nil, false)
			return
		}
		spool.toolName = toolName
		defer spool.Close()
		stopWatch := spool.watchIdle(ctx, cancel, toolResultSpoolIdleTimeout)
		defer stopWatch()

		resp, err := conn.InvokeStream(ctx, runtimeapi.InvokeReq{
			CallID:    toolID,
			TenantID:  tenantID,
			SessionID: session,
			Target:    entry.Target,
			Tool:      toolName,
			Args:      input,
			TimeoutMS: int64(deadline / time.Millisecond),
		}, spool)
		if err != nil {
			if ctx.Err() == context.Canceled && spool.TerminalState() == invokeResultSpoolTimedOut {
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: streamed tool result timed out", nil, false)
				return
			}
			spool.MarkTerminal(invokeResultSpoolFailed)
			s.hub.Logger.Warn("runtime tool invoke failed", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID, "err", err)
			if ctx.Err() != nil {
				spool.MarkTerminal(invokeResultSpoolTimedOut)
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: invoke timed out", nil, false)
				return
			}
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err), nil, false)
			return
		}
		if resp.OK {
			spool.MarkTerminal(invokeResultSpoolCompleted)
		} else if resp.Error != nil && resp.Error.Code == wire.ErrorCancelled {
			spool.MarkTerminal(invokeResultSpoolCancelled)
		} else if resp.Error != nil && resp.Error.Code == wire.ErrorTimeout {
			spool.MarkTerminal(invokeResultSpoolTimedOut)
		} else {
			spool.MarkTerminal(invokeResultSpoolFailed)
		}
		result, resultErr := s.finalizeRuntimeToolResult(tenantID, session, toolID, toolName, resp, spool)
		if resultErr != nil {
			result = runtimeToolResult{Output: "ERROR: " + resultErr.Error()}
		}
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, result.Output, result.Artifact, result.Truncated)
		s.hub.emitAfterToolCall(tenantID, session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": result.Output,
		})
	}()
}

type runtimeToolResult struct {
	Output    string
	Artifact  json.RawMessage
	Truncated bool
}

type toolResultSource struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes,omitempty"`
	Preview string `json:"preview,omitempty"`
}

type beforeToolResultPayload struct {
	Tool      string            `json:"tool"`
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	Session   string            `json:"session"`
	OK        bool              `json:"ok"`
	Output    string            `json:"output,omitempty"`
	Artifact  json.RawMessage   `json:"artifact,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
	Source    *toolResultSource `json:"source,omitempty"`
}

type invokeResultSpool struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	bytes    int64
	preview  []byte
	started  bool
	state    invokeResultSpoolState
	activity chan struct{}
	hub      *Hub
	tenantID string
	session  string
	toolID   string
	toolName string
}

func newInvokeResultSpool(h *Hub, tenantID, session, toolID string) (*invokeResultSpool, error) {
	root := os.TempDir()
	if h != nil {
		if store, ok := h.sessionStore.(*DiskSessionStore); ok && strings.TrimSpace(store.home) != "" {
			root = filepath.Join(store.home, "run", "tool-results")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	cleanupStaleInvokeResultSpools(root, toolResultSpoolStaleAge)
	stem := strings.TrimSpace(session)
	if stem == "" {
		stem = strings.TrimSpace(toolID)
	}
	if stem == "" {
		stem = "tool-result"
	}
	file, err := os.CreateTemp(root, filepath.Base(stem)+"-*.json")
	if err != nil {
		return nil, err
	}
	return &invokeResultSpool{file: file, path: file.Name(), state: invokeResultSpoolOpen, activity: make(chan struct{}, 1), hub: h, tenantID: tenantID, session: session, toolID: toolID}, nil
}

func (s *invokeResultSpool) Start(string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	s.touch()
	s.sendEvent(TopicToolResultStart, "", string(invokeResultSpoolOpen))
	return nil
}

func (s *invokeResultSpool) Delta(_ string, chunk []byte) error {
	if s == nil || len(chunk) == 0 {
		return nil
	}
	s.mu.Lock()
	if s.file == nil {
		s.mu.Unlock()
		return nil
	}
	n, err := s.file.Write(chunk)
	s.bytes += int64(n)
	var previewText string
	if len(s.preview) < toolResultSpoolPreviewBytes && n > 0 {
		remaining := toolResultSpoolPreviewBytes - len(s.preview)
		if n < remaining {
			remaining = n
		}
		s.preview = append(s.preview, chunk[:remaining]...)
		previewText = utf8Preview(chunk[:remaining])
	}
	state := string(s.state)
	s.mu.Unlock()
	if previewText != "" {
		s.sendEvent(TopicToolResultDelta, previewText, state)
	}
	s.touch()
	return err
}

func (s *invokeResultSpool) End(_ string, totalBytes int64) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.file == nil {
		s.mu.Unlock()
		return nil
	}
	if totalBytes > 0 && s.bytes != totalBytes {
		wrote := s.bytes
		if s.state == invokeResultSpoolOpen {
			s.state = invokeResultSpoolProtocolError
		}
		s.mu.Unlock()
		s.touch()
		s.sendEvent(TopicToolResultEnd, "", string(invokeResultSpoolProtocolError))
		return fmt.Errorf("streamed tool result byte mismatch: wrote %d bytes, expected %d", wrote, totalBytes)
	}
	err := s.file.Sync()
	if err != nil && s.state == invokeResultSpoolOpen {
		s.state = invokeResultSpoolProtocolError
	}
	s.mu.Unlock()
	s.touch()
	if err != nil {
		s.sendEvent(TopicToolResultEnd, "", string(invokeResultSpoolProtocolError))
		return err
	}
	s.sendEvent(TopicToolResultEnd, "", string(invokeResultSpoolCompleted))
	return nil
}

func (s *invokeResultSpool) Raw() (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.file = nil
	}
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func (s *invokeResultSpool) Source() *toolResultSource {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	path := s.path
	bytes := s.bytes
	preview := append([]byte(nil), s.preview...)
	s.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return &toolResultSource{Kind: "spool_file", Path: path, Bytes: bytes, Preview: utf8Preview(preview)}
}

func (s *invokeResultSpool) MarkTerminal(state invokeResultSpoolState) bool {
	if s == nil || state == invokeResultSpoolOpen {
		return false
	}
	s.mu.Lock()
	if s.state != invokeResultSpoolOpen {
		s.mu.Unlock()
		return false
	}
	s.state = state
	started := s.started
	s.mu.Unlock()
	s.touch()
	if started && state != invokeResultSpoolCompleted {
		s.sendEvent(TopicToolResultEnd, "", string(state))
	}
	return true
}

func (s *invokeResultSpool) TerminalState() invokeResultSpoolState {
	if s == nil {
		return invokeResultSpoolOpen
	}
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	return state
}

func (s *invokeResultSpool) Started() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	return started
}

func (s *invokeResultSpool) touch() {
	if s == nil || s.activity == nil {
		return
	}
	select {
	case s.activity <- struct{}{}:
	default:
	}
}

func (s *invokeResultSpool) watchIdle(ctx context.Context, cancel context.CancelFunc, idle time.Duration) func() {
	if s == nil || idle <= 0 || cancel == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
			case <-timer.C:
				if s.Started() && s.MarkTerminal(invokeResultSpoolTimedOut) {
					cancel()
				}
				return
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *invokeResultSpool) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	s.mu.Lock()
	if s.state == invokeResultSpoolOpen {
		s.state = invokeResultSpoolCancelled
	}
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			firstErr = err
		}
		s.file = nil
	}
	path := s.path
	s.mu.Unlock()
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *invokeResultSpool) sendEvent(topic, text, state string) {
	if s == nil || s.hub == nil || s.session == "" || s.toolID == "" {
		return
	}
	s.hub.broadcastToSession(s.tenantID, s.session, topic, &Message{
		Type:  string(MsgEvent),
		Topic: topic,
		ID:    s.toolID,
		Name:  s.toolName,
		Text:  text,
		State: state,
	}, nil)
}

func cleanupStaleInvokeResultSpools(root string, maxAge time.Duration) {
	if root == "" || maxAge <= 0 {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(root, entry.Name()))
	}
}

func utf8Preview(data []byte) string {
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}

func (s *ToolService) finalizeRuntimeToolResult(tenantID, session, toolID, toolName string, resp runtimeapi.InvokeResp, spool *invokeResultSpool) (runtimeToolResult, error) {
	payload, large, err := buildBeforeToolResultPayload(tenantID, session, toolID, toolName, resp, spool)
	if err != nil {
		return runtimeToolResult{}, fmt.Errorf("runtime tool %s failed: %w", toolName, err)
	}
	finalPayload, err := s.rewriteBeforeToolResult(tenantID, session, payload, large)
	if err != nil {
		return runtimeToolResult{}, err
	}
	return finalPayload.result(), nil
}

func buildBeforeToolResultPayload(tenantID, session, toolID, toolName string, resp runtimeapi.InvokeResp, spool *invokeResultSpool) (beforeToolResultPayload, bool, error) {
	payload := beforeToolResultPayload{Tool: toolName, ID: toolID, TenantID: tenantID, Session: session, OK: resp.OK}
	if !resp.OK {
		payload.Output = runtimeToolErrorOutput(resp)
		return payload, false, nil
	}
	if resp.Bytes > toolResultInlineLimitBytes {
		payload.Source = spool.Source()
		return payload, true, nil
	}
	raw, err := spool.Raw()
	if err != nil {
		return beforeToolResultPayload{}, false, err
	}
	output, err := toolresult.Render(raw)
	if err != nil {
		return beforeToolResultPayload{}, false, err
	}
	if len(output) > toolResultInlineLimitBytes {
		payload.Source = spool.Source()
		return payload, true, nil
	}
	payload.Output = output
	return payload, false, nil
}

func (s *ToolService) rewriteBeforeToolResult(tenantID, session string, payload beforeToolResultPayload, large bool) (beforeToolResultPayload, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return beforeToolResultPayload{}, err
	}
	rewritten, ok := s.hub.dispatchHook("before_tool_result", raw, tenantID, session)
	if !ok {
		if large {
			return beforeToolResultPayload{}, fmt.Errorf("tool result exceeded inline delivery limit and before_tool_result blocked delivery")
		}
		return payload, nil
	}
	var final beforeToolResultPayload
	if err := json.Unmarshal(rewritten, &final); err != nil {
		if large {
			return beforeToolResultPayload{}, fmt.Errorf("tool result exceeded inline delivery limit and before_tool_result returned invalid payload: %w", err)
		}
		return payload, nil
	}
	if len(final.Output) > toolResultInlineLimitBytes {
		return beforeToolResultPayload{}, fmt.Errorf("tool result exceeded inline delivery limit and before_tool_result did not provide bounded output")
	}
	if final.Output == "" {
		if large || final.Source != nil {
			return beforeToolResultPayload{}, fmt.Errorf("tool result exceeded inline delivery limit and before_tool_result did not rewrite it")
		}
		if payload.Output != "" {
			return payload, nil
		}
	}
	return final, nil
}

func (p beforeToolResultPayload) result() runtimeToolResult {
	return runtimeToolResult{Output: p.Output, Artifact: p.Artifact, Truncated: p.Truncated}
}

func runtimeToolErrorOutput(resp runtimeapi.InvokeResp) string {
	if resp.Error == nil {
		return "ERROR: runtime returned empty error"
	}
	if resp.Error.Message != "" {
		return "ERROR: " + resp.Error.Message
	}
	return "ERROR: " + string(resp.Error.Code)
}

func runtimeInvokeDeadline(toolDeadline time.Duration) time.Duration {
	if toolDeadline <= 0 {
		toolDeadline = 30 * time.Second
	}
	grace := 5 * time.Second
	if toolDeadline/10 > grace {
		grace = toolDeadline / 10
	}
	return toolDeadline + grace
}

func resolveToolDeadline(deadlineMs int) time.Duration {
	if deadlineMs <= 0 {
		deadlineMs = 30000
	}
	maxDeadline := int((15 * time.Minute) / time.Millisecond)
	if deadlineMs > maxDeadline {
		deadlineMs = maxDeadline
	}
	return time.Duration(deadlineMs) * time.Millisecond
}

func formatCommandResult(out []byte, err error) string {
	if len(out) > maxExecOutput {
		out = append(out[:maxExecOutput], []byte("\n[truncated]")...)
	}

	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result = fmt.Sprintf("ERROR: exit code %d", exitErr.ExitCode())
		} else {
			result = fmt.Sprintf("ERROR: %v", err)
		}
	}
	if result == "" {
		return "OK"
	}
	return result
}
