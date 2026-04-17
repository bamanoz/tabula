package kernel

import "encoding/json"

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
