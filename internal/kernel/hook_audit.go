package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

const hookDispatchAuditKind = "hook.dispatch.audit"

func (h *Hub) recordHookDispatchAudit(a HookDispatchAudit) {
	if h == nil || a.Event == "" || a.Session == "" {
		return
	}
	store, ok := h.sessionStore.(*DiskSessionStore)
	if !ok || strings.TrimSpace(store.home) == "" {
		return
	}
	tenantID := a.TenantID
	if tenantID == "" {
		tenantID = h.sessionTenantID("", a.Session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	payload := map[string]any{
		"hook":            a.Event,
		"target":          a.Target,
		"hook_id":         a.HookID,
		"reply_action":    a.ReplyAction,
		"dispatch_effect": a.DispatchEffect,
		"status":          a.Status,
		"duration_ms":     a.DurationMs,
		"timeout_ms":      a.TimeoutMs,
		"input_summary":   summarizeHookPayload(a.Payload),
	}
	if a.Reason != "" {
		payload["reason"] = a.Reason
	}
	appendSessionLedgerEvent(store.home, a.Session, tenantID, hookDispatchAuditKind, "kernel:hook_dispatch", payload)
}

func appendSessionLedgerEvent(home, session, tenantID, kind, producer string, payload map[string]any) {
	event := map[string]any{
		"type":      "ledger.event",
		"kind":      kind,
		"session":   session,
		"tenant_id": tenantID,
		"producer":  producer,
		"payload":   payload,
		"ts":        float64(time.Now().UnixNano()) / 1e9,
	}
	path := filepath.Join(home, "data", "sessions", session, "ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(data, '\n'))
}

func summarizeHookPayload(raw json.RawMessage) map[string]any {
	summary := map[string]any{"payload_sha256": sha256Hex(raw)}
	var body struct {
		Tool  string         `json:"tool"`
		ID    string         `json:"id"`
		Meta  map[string]any `json:"meta"`
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return summary
	}
	if body.Tool != "" {
		summary["tool"] = body.Tool
	}
	if body.ID != "" {
		summary["tool_call_id"] = body.ID
	}
	if turnCorrelationID, _ := body.Meta[turnCorrelationMetaKey].(string); turnCorrelationID != "" {
		summary["turn_correlation_id"] = turnCorrelationID
	}
	if body.Input == nil {
		return summary
	}
	keys := make([]string, 0, len(body.Input))
	for key := range body.Input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summary["input_keys"] = keys
	inputBytes, _ := json.Marshal(body.Input)
	summary["input_sha256"] = sha256Hex(inputBytes)
	addKnownInputSummary(summary, body.Tool, body.Input)
	return summary
}

func addKnownInputSummary(summary map[string]any, tool string, input map[string]any) {
	switch {
	case tool == "exec_run" || tool == "exec_run_background":
		if cwd, ok := stringField(input, "cwd"); ok {
			summary["cwd"] = cwd
		}
		if timeout, ok := numberField(input, "timeout_seconds"); ok {
			summary["timeout_seconds"] = timeout
		}
		if command, ok := stringField(input, "command"); ok {
			summary["command_sha256"] = sha256Hex([]byte(command))
		}
	case strings.HasPrefix(tool, "fs_"):
		for _, key := range []string{"path", "glob", "pattern"} {
			if value, ok := stringField(input, key); ok {
				summary[key] = value
			}
		}
	case tool == "artifact_read":
		for _, key := range []string{"ref", "session"} {
			if value, ok := stringField(input, key); ok {
				summary[key] = value
			}
		}
		if offset, ok := numberField(input, "offset"); ok {
			summary["offset"] = offset
		}
		if limit, ok := numberField(input, "limit_chars"); ok {
			summary["limit_chars"] = limit
		}
	case strings.HasPrefix(tool, "subagent_"):
		for _, key := range []string{"id", "type", "mode"} {
			if value, ok := stringField(input, key); ok {
				summary[key] = value
			}
		}
		if timeout, ok := numberField(input, "timeout"); ok {
			summary["timeout"] = timeout
		}
		if task, ok := stringField(input, "task"); ok {
			summary["task_sha256"] = sha256Hex([]byte(task))
			summary["task_preview"] = previewString(task, 80)
		}
	}
}

func stringField(input map[string]any, key string) (string, bool) {
	value, ok := input[key].(string)
	return value, ok && value != ""
}

func numberField(input map[string]any, key string) (any, bool) {
	value, ok := input[key]
	if !ok {
		return nil, false
	}
	switch value.(type) {
	case float64, int, int64, json.Number:
		return value, true
	default:
		return nil, false
	}
}

func previewString(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
