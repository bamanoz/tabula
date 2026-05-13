package kernel

import (
	"encoding/json"
	"sort"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type snapshotProcessInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Alive   bool   `json:"alive"`
}

type snapshotSessionInfo struct {
	TenantID        string                `json:"tenant_id"`
	State           SessionState          `json:"state"`
	CreatedAt       string                `json:"created_at"`
	LastActiveAt    string                `json:"last_active_at"`
	Busy            bool                  `json:"busy"`
	CancelRequested bool                  `json:"cancel_requested"`
	PendingInputs   int                   `json:"pending_inputs"`
	Clients         []string              `json:"clients"`
	Processes       []snapshotProcessInfo `json:"processes"`
}

type snapshotRuntimeInfo struct {
	ID                   string               `json:"id"`
	Attached             bool                 `json:"attached"`
	PID                  int                  `json:"pid"`
	Capabilities         []string             `json:"capabilities"`
	TenantsServed        []string             `json:"tenants_served"`
	CapabilitiesByTenant map[string][]string  `json:"capabilities_by_tenant"`
	WorkerCount          int                  `json:"worker_count,omitempty"`
	LastError            *string              `json:"last_error,omitempty"`
	ConnectedAt          string               `json:"connected_at,omitempty"`
	Targets              []snapshotTargetInfo `json:"targets,omitempty"`
}

type snapshotTargetInfo struct {
	Kind           string   `json:"kind"`
	ID             string   `json:"id"`
	Tenants        []string `json:"tenants,omitempty"`
	Tools          []string `json:"tools,omitempty"`
	Hooks          []string `json:"hooks,omitempty"`
	State          string   `json:"state,omitempty"`
	Source         string   `json:"source,omitempty"`
	Revision       int64    `json:"revision,omitempty"`
	PID            int      `json:"pid,omitempty"`
	LifecycleState string   `json:"lifecycle_state,omitempty"`
	Diagnostic     string   `json:"diagnostic,omitempty"`
}

type snapshotRuntimesInfo struct {
	Runtimes []snapshotRuntimeInfo `json:"runtimes"`
}

// SnapshotSessions returns a JSON snapshot of all sessions with state and metadata.
func (h *Hub) SnapshotSessions() []byte {
	sessions := make(map[string]*snapshotSessionInfo)

	for _, sess := range h.sessions.All() {
		sess.mu.RLock()
		info := &snapshotSessionInfo{
			TenantID:        sess.TenantID,
			State:           sess.State,
			CreatedAt:       sess.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			LastActiveAt:    sess.LastActiveAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Busy:            sess.inflightTurn,
			CancelRequested: sess.cancelRequested,
			PendingInputs:   len(sess.pendingInputs),
			Clients:         []string{},
			Processes:       []snapshotProcessInfo{},
		}
		sess.mu.RUnlock()
		for _, c := range h.sessionClients(sess.ID) {
			info.Clients = append(info.Clients, c.name)
		}
		for _, proc := range h.sessionProcesses(sess.ID) {
			info.Processes = append(info.Processes, snapshotProcessInfo{
				PID:     proc.PID,
				Command: proc.Command,
				Alive:   proc.Alive,
			})
		}
		sessions[sess.ID] = info
	}

	data, _ := json.Marshal(sessions)
	return data
}

func snapshotRuntimes(h *Hub) []byte {
	out := snapshotRuntimesInfo{Runtimes: []snapshotRuntimeInfo{}}
	if h == nil || h.runtimes == nil {
		data, _ := json.Marshal(out)
		return data
	}
	for _, attachment := range h.runtimes.Snapshot() {
		capabilities := runtimeCapabilityNames(attachment.Capabilities)
		tenantsServed := normalizedTenantsServed(attachment.TenantsServed)
		info := snapshotRuntimeInfo{
			ID:                   attachment.ID,
			Attached:             attachment.Attached,
			PID:                  attachment.PID,
			Capabilities:         capabilities,
			TenantsServed:        tenantsServed,
			CapabilitiesByTenant: capabilitiesByTenant(tenantsServed, capabilities),
			WorkerCount:          attachment.Health.WorkerCount,
			Targets:              runtimeTargets(attachment),
		}
		if attachment.LastError != "" {
			err := attachment.LastError
			info.LastError = &err
		}
		if !attachment.ConnectedAt.IsZero() {
			info.ConnectedAt = formatSnapshotTime(attachment.ConnectedAt)
		}
		out.Runtimes = append(out.Runtimes, info)
	}
	data, _ := json.Marshal(out)
	return data
}

func normalizedTenantsServed(tenants []string) []string {
	if len(tenants) == 0 {
		return []string{"*"}
	}
	out := append([]string(nil), tenants...)
	sort.Strings(out)
	return out
}

func capabilitiesByTenant(tenants, capabilities []string) map[string][]string {
	out := make(map[string][]string, len(tenants))
	for _, tenantID := range tenants {
		out[tenantID] = append([]string(nil), capabilities...)
	}
	return out
}
func formatSnapshotTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z07:00")
}

func runtimeCapabilityNames(capabilities []runtimeapi.Capability) []string {
	set := make(map[string]struct{})
	for _, capability := range capabilities {
		if capability.State != wire.CapabilityStateReady {
			continue
		}
		for _, tool := range capability.Tools {
			if tool.Name != "" {
				set[tool.Name] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func runtimeTargets(attachment RuntimeAttachment) []snapshotTargetInfo {
	targets := make([]snapshotTargetInfo, 0, len(attachment.Capabilities))
	for _, capability := range attachment.Capabilities {
		tools := make([]string, 0, len(capability.Tools))
		for _, tool := range capability.Tools {
			if tool.Name != "" {
				tools = append(tools, tool.Name)
			}
		}
		sort.Strings(tools)
		hooks := make([]string, 0, len(capability.Hooks))
		for _, hook := range capability.Hooks {
			if hook.Event != "" {
				hooks = append(hooks, hook.Event)
			}
		}
		sort.Strings(hooks)
		status := attachment.targetStatus[runtimeTargetKey(capability.Target)]
		targets = append(targets, snapshotTargetInfo{
			Kind:           string(capability.Target.Kind),
			ID:             capability.Target.ID,
			Tenants:        append([]string(nil), capability.Tenants...),
			Tools:          tools,
			Hooks:          hooks,
			State:          string(capability.State),
			Source:         string(capability.Source),
			Revision:       capability.Revision,
			PID:            status.PID,
			LifecycleState: string(status.Lifecycle),
			Diagnostic:     status.Message,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Kind == targets[j].Kind {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].Kind < targets[j].Kind
	})
	return targets
}
