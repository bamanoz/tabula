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
