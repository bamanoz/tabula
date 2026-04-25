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

func (h *Hub) processByPID(pid int) (*SpawnedProcess, bool) {
	return h.processes.ByPID(pid)
}

func (h *Hub) updateProcess(pid int, fn func(*SpawnedProcess)) bool {
	return h.processes.Update(pid, fn)
}

func (h *Hub) forEachProcess(fn func(pid int, proc *SpawnedProcess)) {
	h.processes.ForEach(fn)
}

func (h *Hub) clientDepthByName(name string) (int, bool) {
	return h.clients.DepthByName(name)
}

func (h *Hub) seedSpawnToken(token string, entry spawnTokenEntry) {
	h.tokens.Seed(token, entry)
}

func (h *Hub) spawnTokenEntry(token string) (spawnTokenEntry, bool) {
	return h.tokens.Get(token)
}

func (h *Hub) registerSpawnedCommand(cmd *exec.Cmd, command, session string) *SpawnedProcess {
	return h.processes.Register(cmd, command, session)
}
