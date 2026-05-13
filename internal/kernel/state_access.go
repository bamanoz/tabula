package kernel

import "os/exec"

func (h *Hub) addClient(c *Client) bool {
	return h.clients.Add(c, h.MaxClients)
}

func (h *Hub) removeClient(c *Client) {
	h.clients.Remove(c)
}

func (h *Hub) assignClientSession(c *Client, session string) {
	h.clients.AssignSession(c, session)
}

func (h *Hub) configureClient(c *Client, name string, sends, receives, receivesGlobal []string, hooks []HookSubscription, depth int) int {
	return h.clients.Configure(c, name, sends, receives, receivesGlobal, hooks, depth)
}

func (h *Hub) forEachProcess(fn func(pid int, proc *SpawnedProcess)) {
	h.processes.ForEach(fn)
}

func (h *Hub) registerSpawnedCommand(cmd *exec.Cmd, command, session string) *SpawnedProcess {
	return h.processes.Register(cmd, command, session)
}
