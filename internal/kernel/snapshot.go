package kernel

import "encoding/json"

type snapshotProcessInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Alive   bool   `json:"alive"`
}

type snapshotSessionInfo struct {
	Clients   []string              `json:"clients"`
	Processes []snapshotProcessInfo `json:"processes"`
}

// SnapshotSessions returns a JSON snapshot of all sessions with state and metadata.
func (h *Hub) SnapshotSessions() []byte {
	sessions := make(map[string]*snapshotSessionInfo)

	for _, sess := range h.sessions.All() {
		info := &snapshotSessionInfo{
			Clients:   []string{},
			Processes: []snapshotProcessInfo{},
		}
		for _, c := range h.sessionClients(sess.ID) {
			info.Clients = append(info.Clients, c.name)
		}
		for _, proc := range h.sessionProcesses(sess.ID) {
			info.Processes = append(info.Processes, snapshotProcessInfo{
				PID:     proc.Cmd.Process.Pid,
				Command: proc.Command,
				Alive:   proc.Alive,
			})
		}
		sessions[sess.ID] = info
	}

	data, _ := json.Marshal(sessions)
	return data
}
