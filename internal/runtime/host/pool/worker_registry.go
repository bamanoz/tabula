package pool

import (
	"sync"

	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type workerRegistry struct {
	mu      sync.Mutex
	entries map[key]*entry
}

type key struct {
	kernelID string
	tenantID string
	targetID string
}

type entry struct {
	mu     sync.Mutex
	worker policy.Worker
}

type evictedEntry struct {
	target wire.Target
	entry  *entry
}

func newWorkerRegistry() *workerRegistry {
	return &workerRegistry{entries: map[key]*entry{}}
}

func (r *workerRegistry) entryFor(k key) *entry {
	if r == nil {
		return &entry{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.entries[k]; existing != nil {
		return existing
	}
	e := &entry{}
	r.entries[k] = e
	return e
}

func (r *workerRegistry) evict(target *wire.Target, tenants ...string) []evictedEntry {
	if r == nil {
		return nil
	}
	requestedTenants := tenantSet(tenants)
	r.mu.Lock()
	defer r.mu.Unlock()
	evicted := make([]evictedEntry, 0)
	for k, e := range r.entries {
		if target != nil && (target.Kind != wire.TargetKindPlugin || target.ID != k.targetID) {
			continue
		}
		if len(requestedTenants) > 0 && !requestedTenants[k.tenantID] {
			continue
		}
		delete(r.entries, k)
		evicted = append(evicted, evictedEntry{target: wire.Target{Kind: wire.TargetKindPlugin, ID: k.targetID}, entry: e})
	}
	return evicted
}

func (r *workerRegistry) entriesSnapshot() []*entry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]*entry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}
	return entries
}

func entryHasWorker(entry *entry, worker policy.Worker) bool {
	if entry == nil {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.worker == worker
}

func clearEntryWorker(entry *entry, worker policy.Worker) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.worker == worker {
		entry.worker = nil
	}
}
