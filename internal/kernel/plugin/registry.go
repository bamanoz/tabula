package plugin

import "sync"

// Registry is the kernel-side store of currently registered plugins.
// Intentionally separate from kernel.ClientRegistry: plugins are
// kernel-level singletons (no maxClients cap, no numeric id allocation)
// per creative `creative-plugin-runtime.md` §11.
//
// Phase 2 D2.1 ships the in-memory store; persistence (snapshot) is added
// alongside Hub.SnapshotPlugins() in a later pass.
type Registry struct {
	mu      sync.RWMutex
	byID    map[string]*Handle
	ordered []*Handle // preserves registration order for All()
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Handle)}
}

// Add inserts a Handle. If a Handle with the same id already exists it
// is replaced and the prior Handle is returned so the caller can shut
// it down (idempotent re-register per creative §3 RegisterPlugin
// godoc). Returns nil for the prior Handle on first insert.
func (r *Registry) Add(h *Handle) *Handle {
	if h == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	prior := r.byID[h.id]
	r.byID[h.id] = h
	if prior != nil {
		// replace in ordered slice
		for i, existing := range r.ordered {
			if existing == prior {
				r.ordered[i] = h
				return prior
			}
		}
	}
	r.ordered = append(r.ordered, h)
	return prior
}

// Remove deletes the Handle for id. Returns the removed Handle, or nil
// if no such id was registered. Caller is responsible for Close().
func (r *Registry) Remove(id string) *Handle {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.byID[id]
	if !ok {
		return nil
	}
	delete(r.byID, id)
	for i, existing := range r.ordered {
		if existing == h {
			r.ordered = append(r.ordered[:i], r.ordered[i+1:]...)
			break
		}
	}
	return h
}

// Get returns the Handle for id, or nil if absent.
func (r *Registry) Get(id string) *Handle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// All returns a snapshot copy of all currently registered Handles in
// registration order. Safe to mutate the returned slice.
func (r *Registry) All() []*Handle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Handle, len(r.ordered))
	copy(out, r.ordered)
	return out
}

// Len returns the number of registered Handles.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}
