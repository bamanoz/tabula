package tenant

import "sort"

// MemoryStore is an in-memory tenant store for tests and default single-tenant hubs.
type MemoryStore struct {
	items map[string]Tenant
}

// NewMemoryStore constructs a memory store preloaded with tenants.
func NewMemoryStore(items ...Tenant) *MemoryStore {
	store := &MemoryStore{items: map[string]Tenant{}}
	for _, item := range items {
		if item.ID != "" {
			store.items[item.ID] = item
		}
	}
	return store
}

func (s *MemoryStore) List() ([]Tenant, error) {
	out := make([]Tenant, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) Get(id string) (Tenant, bool, error) {
	item, ok := s.items[id]
	return item, ok, nil
}

func (s *MemoryStore) Create(item Tenant) error {
	s.items[item.ID] = item
	return nil
}

func (s *MemoryStore) Delete(id string) error {
	delete(s.items, id)
	return nil
}
