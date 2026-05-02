package kernel

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
)

type snapshotProcessInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Alive   bool   `json:"alive"`
}

type snapshotSessionInfo struct {
	State           SessionState          `json:"state"`
	CreatedAt       string                `json:"created_at"`
	LastActiveAt    string                `json:"last_active_at"`
	Busy            bool                  `json:"busy"`
	CancelRequested bool                  `json:"cancel_requested"`
	Clients         []string              `json:"clients"`
	Processes       []snapshotProcessInfo `json:"processes"`
}

type snapshotPluginInfo struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	PID             int      `json:"pid"`
	RestartCount    int      `json:"restart_count"`
	LastError       *string  `json:"last_error"`
	RegisteredTools []string `json:"registered_tools"`
	Subscriptions   []string `json:"subscriptions"`
	RegisteredAt    string   `json:"registered_at,omitempty"`
}

type snapshotPluginsInfo struct {
	Plugins []snapshotPluginInfo `json:"plugins"`
}

type snapshotRuntimeInfo struct {
	ID           string               `json:"id"`
	Attached     bool                 `json:"attached"`
	PID          int                  `json:"pid"`
	Capabilities []string             `json:"capabilities"`
	WorkerCount  int                  `json:"worker_count,omitempty"`
	LastError    *string              `json:"last_error,omitempty"`
	ConnectedAt  string               `json:"connected_at,omitempty"`
	Targets      []snapshotTargetInfo `json:"targets,omitempty"`
}

type snapshotTargetInfo struct {
	Kind  string   `json:"kind"`
	ID    string   `json:"id"`
	Tools []string `json:"tools,omitempty"`
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
			State:           sess.State,
			CreatedAt:       sess.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			LastActiveAt:    sess.LastActiveAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Busy:            sess.inflightTurn,
			CancelRequested: sess.cancelRequested,
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

// SnapshotPlugins returns a JSON snapshot of kernel-level plugin singletons.
// It is intentionally separate from SnapshotSessions because plugins are not
// session-scoped. Failed terminal lifecycle states are retained even after the
// live handle is removed from the plugin registry so diagnostics can explain why
// a boot-configured plugin is absent.
func (h *Hub) SnapshotPlugins() []byte {
	if h == nil {
		data, _ := json.Marshal(snapshotPluginsInfo{Plugins: []snapshotPluginInfo{}})
		return data
	}
	byID := make(map[string]snapshotPluginInfo)
	order := make([]string, 0)

	for _, handle := range h.snapshotPluginHandles() {
		if handle == nil {
			continue
		}
		info := snapshotPluginInfo{
			ID:              handle.ID(),
			Status:          pluginHandleStatus(handle),
			PID:             handle.PID(),
			RegisteredTools: pluginToolNames(handle.Tools()),
			Subscriptions:   pluginSubscriptionEvents(handle.Subscriptions()),
		}
		if started := handle.StartedAt(); !started.IsZero() {
			info.RegisteredAt = formatSnapshotTime(started)
		}
		byID[handle.ID()] = info
		order = append(order, handle.ID())
	}

	stateIDs := make([]string, 0)
	for id, state := range h.snapshotPluginStates() {
		if state == nil {
			continue
		}
		info, ok := byID[id]
		if !ok {
			info = snapshotPluginInfo{ID: id, RegisteredTools: []string{}, Subscriptions: []string{}}
			byID[id] = info
			stateIDs = append(stateIDs, id)
		}
		if state.Status != "" {
			info.Status = state.Status
		}
		info.RestartCount = state.RestartCount
		if state.LastError != "" {
			err := state.LastError
			info.LastError = &err
		} else {
			info.LastError = nil
		}
		if info.Status == "" {
			info.Status = "failed"
		}
		byID[id] = info
	}
	sort.Strings(stateIDs)
	order = append(order, stateIDs...)

	out := snapshotPluginsInfo{Plugins: make([]snapshotPluginInfo, 0, len(byID))}
	seen := make(map[string]struct{}, len(byID))
	for _, id := range order {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out.Plugins = append(out.Plugins, byID[id])
	}
	data, _ := json.Marshal(out)
	return data
}

func snapshotRuntimes(h *Hub) []byte {
	out := snapshotRuntimesInfo{Runtimes: []snapshotRuntimeInfo{}}
	if h == nil || h.runtimes == nil {
		data, _ := json.Marshal(out)
		return data
	}
	for _, attachment := range h.runtimes.Snapshot() {
		info := snapshotRuntimeInfo{
			ID:           attachment.ID,
			Attached:     attachment.Attached,
			PID:          attachment.PID,
			Capabilities: runtimeCapabilityNames(attachment.Capabilities),
			WorkerCount:  attachment.Health.WorkerCount,
			Targets:      runtimeTargets(attachment.Capabilities),
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

func (h *Hub) snapshotPluginHandles() []*plugin.Handle {
	if h == nil || h.plugins == nil {
		return nil
	}
	return h.plugins.All()
}

func (h *Hub) snapshotPluginStates() map[string]*pluginLifecycleState {
	if h == nil {
		return nil
	}
	h.pluginStatesMu.RLock()
	defer h.pluginStatesMu.RUnlock()
	out := make(map[string]*pluginLifecycleState, len(h.pluginStates))
	for id, state := range h.pluginStates {
		if state == nil {
			continue
		}
		copyState := *state
		out[id] = &copyState
	}
	return out
}

func pluginHandleStatus(handle *plugin.Handle) string {
	if handle == nil {
		return "failed"
	}
	if handle.IsAlive() && handle.IsRegistered() {
		return "running"
	}
	return "failed"
}

func pluginToolNames(tools []plugin.ToolSpec) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	sort.Strings(names)
	return names
}

func pluginSubscriptionEvents(subs []plugin.SubscriptionSpec) []string {
	events := make([]string, 0, len(subs))
	for _, sub := range subs {
		if sub.Event != "" {
			events = append(events, sub.Event)
		}
	}
	sort.Strings(events)
	return events
}

func formatSnapshotTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z07:00")
}

func runtimeCapabilityNames(capabilities []runtimeapi.Capability) []string {
	set := make(map[string]struct{})
	for _, capability := range capabilities {
		for _, tool := range capability.Tools {
			if tool != "" {
				set[tool] = struct{}{}
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

func runtimeTargets(capabilities []runtimeapi.Capability) []snapshotTargetInfo {
	targets := make([]snapshotTargetInfo, 0, len(capabilities))
	for _, capability := range capabilities {
		tools := append([]string(nil), capability.Tools...)
		sort.Strings(tools)
		targets = append(targets, snapshotTargetInfo{Kind: string(capability.Target.Kind), ID: capability.Target.ID, Tools: tools})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Kind == targets[j].Kind {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].Kind < targets[j].Kind
	})
	return targets
}
