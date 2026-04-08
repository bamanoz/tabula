package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
)

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	mu           sync.Mutex
	clients      map[*Client]bool
	spawned      map[int]*SpawnedProcess
	spawnTokens  map[string]int // token → child depth (one-time use)
	nextClientID int
	systemPrompt string
	toolsJSON    json.RawMessage
	MaxSpawnDepth int
	MaxChildren   int
	Verbose      bool
}

// NewHub creates a new Hub.
func NewHub(systemPrompt string, toolsJSON json.RawMessage, maxSpawnDepth int, maxChildren int, verbose bool) *Hub {
	return &Hub{
		clients:      make(map[*Client]bool),
		spawned:      make(map[int]*SpawnedProcess),
		spawnTokens:  make(map[string]int),
		nextClientID: 1,
		systemPrompt: systemPrompt,
		toolsJSON:    toolsJSON,
		MaxSpawnDepth: maxSpawnDepth,
		MaxChildren:   maxChildren,
		Verbose:      verbose,
	}
}

func (h *Hub) generateSpawnToken(childDepth int) string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	h.spawnTokens[token] = childDepth
	return token
}

func (h *Hub) log(format string, args ...any) {
	if h.Verbose {
		log.Printf("[kernel] "+format, args...)
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	h.log("client disconnected: %s (id c%d)", c.name, c.id)
}

// HandleMessage processes an incoming message from a client.
// Called from the client's readPump goroutine.
func (h *Hub) HandleMessage(sender *Client, msg *Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch msg.Type {
	case "connect":
		h.handleConnect(sender, msg)
	case "join":
		h.handleJoin(sender, msg)
	default:
		if !sender.connected {
			return
		}
		if !sender.canSend(msg.Type) {
			h.log("client %s not allowed to send %s", sender.name, msg.Type)
			return
		}
		if sender.session == "" {
			h.log("client %s not in a session", sender.name)
			return
		}

		switch msg.Type {
		case "tool_use":
			h.handleToolUse(sender, msg)
		case "cancel":
			h.handleCancel()
		default:
			target := sender.session
			if msg.Session != "" {
				target = msg.Session
			}
			h.broadcastToSession(target, msg.Type, msg, sender)
		}
	}
}

func (h *Hub) handleConnect(c *Client, msg *Message) {
	c.name = msg.Name
	c.sends = make(map[string]bool)
	for _, s := range msg.Sends {
		c.sends[s] = true
	}
	c.receives = make(map[string]bool)
	for _, r := range msg.Receives {
		c.receives[r] = true
	}
	c.id = h.nextClientID
	// Resolve depth from spawn token (one-time use)
	if msg.Token != "" {
		if depth, ok := h.spawnTokens[msg.Token]; ok {
			c.depth = depth
			delete(h.spawnTokens, msg.Token)
		} else {
			h.log("invalid spawn token from %s", msg.Name)
		}
	}
	h.nextClientID++
	c.connected = true

	resp := &Message{
		Type: "connected",
		ID:   fmt.Sprintf("c%d", c.id),
	}
	c.SendMsg(resp)
	h.log("client connected: %s (id c%d)", c.name, c.id)
}

func (h *Hub) handleJoin(c *Client, msg *Message) {
	c.session = msg.Session
	resp := &Message{
		Type:    "joined",
		Session: c.session,
	}
	c.SendMsg(resp)
	h.log("client %s joined session %s", c.name, c.session)

	if c.canReceive("init") {
		h.sendInit(c)
	}
}

func (h *Hub) sendInit(c *Client) {
	resp := &Message{
		Type:   "init",
		Prompt: h.systemPrompt,
		Tools:  h.toolsJSON,
	}
	c.SendMsg(resp)
	h.log("sent init to client %s", c.name)
}

func (h *Hub) handleCancel() {
	for pid, proc := range h.spawned {
		if proc.Alive {
			h.log("sending SIGINT to PID %d", pid)
			proc.Signal()
		}
	}
}

// broadcastToSession sends a message to all clients in a session that can receive the given type.
// Caller must hold h.mu.
func (h *Hub) broadcastToSession(session, msgType string, msg *Message, exclude *Client) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	delivered := 0
	for c := range h.clients {
		if c == exclude {
			continue
		}
		if !c.connected || c.session == "" {
			continue
		}
		if c.session != session {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendRaw(data)
		delivered++
	}
	h.log("broadcast %s to session %s: delivered to %d clients", msgType, session, delivered)
}

// broadcastToSessionRaw sends raw JSON bytes to a session.
// Caller must hold h.mu.
func (h *Hub) broadcastToSessionRaw(session, msgType string, data []byte) {
	delivered := 0
	for c := range h.clients {
		if !c.connected || c.session == "" {
			continue
		}
		if c.session != session {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendRaw(data)
		delivered++
	}
	h.log("broadcast %s to session %s: delivered to %d clients", msgType, session, delivered)
}

// RegisterSpawn registers an externally started process in the spawned map.
func (h *Hub) RegisterSpawn(cmd *exec.Cmd, command, session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pid := cmd.Process.Pid
	h.spawned[pid] = &SpawnedProcess{
		Cmd:     cmd,
		Command: command,
		Alive:   true,
		Session: session,
	}
	h.log("registered spawned PID %d: %s", pid, command)
}

// Shutdown cleans up all spawned processes.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for pid, proc := range h.spawned {
		if proc.Alive {
			h.log("killing PID %d on shutdown", pid)
			proc.Kill()
		}
	}
}
