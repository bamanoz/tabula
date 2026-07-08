package kernel

import (
	"encoding/json"
	"strings"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
)

// toolSource discriminates the dispatch route for a registered LLM tool
// in Hub.toolExec.
type toolSource int

const (
	// toolSourceRuntime routes to a runtime-hosted target over Runtime API.
	toolSourceRuntime toolSource = iota
)

// toolDispatch is the value stored in Hub.toolExec for each registered
// tool name.
//
// Schema is the tool's JSON schema as advertised in tools.json or in the
// plugin register-reply; today it is informational only — the LLM
// receives the schema via a separate path (toolsJSON marshalled at
// boot). Reserved for future per-tool validation / debug snapshots.
type toolDispatch struct {
	Source     toolSource
	TenantID   string // "*" for globally visible legacy/runtime tools
	RuntimeID  string // owning runtime id
	Target     runtimeapi.Target
	Schema     json.RawMessage // optional JSON schema for the tool
	DeadlineMs int             // optional plugin tool deadline; defaulted by dispatcher
	Advertise  bool            // false keeps stale in-flight calls routable without adding the tool to new prompts
}

func runtimeDispatch(runtimeID, tenantID string, target runtimeapi.Target, schema json.RawMessage, deadlineMs int) toolDispatch {
	if tenantID == "" {
		tenantID = "*"
	}
	return toolDispatch{Source: toolSourceRuntime, TenantID: tenantID, RuntimeID: runtimeID, Target: target, Schema: schema, DeadlineMs: deadlineMs, Advertise: true}
}

func toolExecKey(tenantID, toolName string) string {
	if tenantID == "" || tenantID == "*" {
		return toolName
	}
	return tenantID + "\x00" + toolName
}

func toolExecVisible(entry toolDispatch, tenantID string) bool {
	return entry.TenantID == "" || entry.TenantID == "*" || entry.TenantID == tenantID
}

func (h *Hub) toolDispatchDiagnostics(tenantID, toolName string) (int, bool) {
	if h == nil {
		return 0, false
	}
	h.toolExecMu.RLock()
	defer h.toolExecMu.RUnlock()
	count := len(h.toolExec)
	for key, entry := range h.toolExec {
		_, name, tenantScoped := strings.Cut(key, "\x00")
		if !tenantScoped {
			name = key
		}
		if name == toolName && !toolExecVisible(entry, tenantID) {
			return count, true
		}
	}
	return count, false
}
