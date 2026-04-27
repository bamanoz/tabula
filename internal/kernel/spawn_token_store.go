package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// spawnTokenEntry tracks a one-time spawn token with its creation time.
type spawnTokenEntry struct {
	depth     int
	createdAt time.Time
}

const spawnTokenTTL = 60 * time.Second

// SpawnTokenStore issues and consumes one-time tokens used to authenticate
// spawned child processes when they reconnect to the kernel.
//
// TODO(skill-plugin-arch): unused after kernel cleanup (Phase 1 D1.2 removed
// the kernel-side spawn flow). Kept as dead code per creative §7 (D1.11
// option b) until subagent plugin GA in tabula-bundles re-implements
// parent-token semantics in plugin-land.
type SpawnTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]spawnTokenEntry
}

func NewSpawnTokenStore() *SpawnTokenStore {
	return &SpawnTokenStore{
		tokens: make(map[string]spawnTokenEntry),
	}
}

func (s *SpawnTokenStore) Generate(childDepth int, now time.Time) (string, error) {
	s.PruneExpired(now)

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate spawn token: %w", err)
	}
	token := hex.EncodeToString(b)
	s.Seed(token, spawnTokenEntry{depth: childDepth, createdAt: now})
	return token, nil
}

func (s *SpawnTokenStore) Seed(token string, entry spawnTokenEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = entry
}

func (s *SpawnTokenStore) Consume(token string) (spawnTokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return spawnTokenEntry{}, false
	}
	delete(s.tokens, token)
	if time.Since(entry.createdAt) > spawnTokenTTL {
		return spawnTokenEntry{}, false
	}
	return entry, true
}

func (s *SpawnTokenStore) Get(token string) (spawnTokenEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tokens[token]
	return entry, ok
}

func (s *SpawnTokenStore) PruneExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.tokens {
		if now.Sub(v.createdAt) > spawnTokenTTL {
			delete(s.tokens, k)
		}
	}
}
