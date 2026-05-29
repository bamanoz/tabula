package kernel

import (
	"encoding/json"
	"sync"

	"github.com/bamanoz/tabula/internal/tenant"
)

type ClientRegistry struct {
	mu           sync.RWMutex
	clients      map[*Client]bool
	nextClientID int
}

func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients:      make(map[*Client]bool),
		nextClientID: 1,
	}
}

func (r *ClientRegistry) Add(c *Client, maxClients int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if maxClients > 0 && len(r.clients) >= maxClients {
		return false
	}
	r.clients[c] = true
	return true
}

func (r *ClientRegistry) Remove(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
}

func (r *ClientRegistry) Configure(c *Client, name string, sends, receives, receivesGlobal []string, hooks []HookSubscription, meta json.RawMessage, depth int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	c.name = name
	c.meta = append(json.RawMessage(nil), meta...)
	c.sends = makeCapabilitySet(sends)
	c.receives = makeCapabilitySet(receives)
	c.receivesGlobal = makeCapabilitySet(receivesGlobal)
	c.id = r.nextClientID
	r.nextClientID++
	c.depth = depth
	c.hooks = hooks
	return c.id
}

func (r *ClientRegistry) AssignSession(c *Client, tenantID, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	c.tenantID = tenantID
	c.session = session
}

func (r *ClientRegistry) All() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clients := make([]*Client, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	return clients
}

func (r *ClientRegistry) InSession(tenantID, session string) []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	clients := make([]*Client, 0)
	for c := range r.clients {
		if !c.IsConnected() || c.session == "" {
			continue
		}
		if c.tenantID != tenantID {
			continue
		}
		if session != "" && c.session != session {
			continue
		}
		clients = append(clients, c)
	}
	return clients
}

func (r *ClientRegistry) DepthByName(name string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		if c.name == name {
			return c.depth, true
		}
	}
	return 0, false
}

// NextID returns the next client ID without incrementing the counter.
func (r *ClientRegistry) NextID() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nextClientID
}
