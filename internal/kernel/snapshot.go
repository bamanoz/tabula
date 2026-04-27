package kernel

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
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
