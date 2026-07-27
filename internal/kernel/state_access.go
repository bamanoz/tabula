package kernel

import (
	"encoding/json"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"os/exec"

	"github.com/bamanoz/tabula/internal/kernel/process"
)

func (h *Hub) addClient(c *Client) bool {
	return h.clients.Add(c, h.MaxClients)
}

func (h *Hub) removeClient(c *Client) {
	h.clients.Remove(c)
}

func (h *Hub) assignClientSession(c *Client, tenantID, session string) {
	h.clients.AssignSession(c, tenantID, session)
}

func (h *Hub) configureClient(c *Client, name string, sends, receives, receivesGlobal []string, hooks []khooks.Subscription, meta json.RawMessage, depth int) int {
	return h.clients.Configure(c, name, sends, receives, receivesGlobal, hooks, meta, depth)
}

func (h *Hub) registerSpawnedCommand(cmd *exec.Cmd, command, session string) *process.Spawned {
	return h.processes.Register(cmd, command, session)
}
