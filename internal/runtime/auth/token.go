// Package auth provides kernel-side Runtime API authentication primitives.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const (
	// LocalRuntimeID is the M2 local runtime placeholder id.
	LocalRuntimeID = "local"
	// DefaultKernelID is the M2 kernel id until M11 config loading lands.
	DefaultKernelID = "main"
	// TokenFileName is the local bearer token filename under TABULA_HOME/run.
	TokenFileName = "runtime-token"
)

var (
	// ErrUnauthorized is returned when a Runtime API Hello does not authenticate.
	ErrUnauthorized = errors.New("runtime auth: unauthorized")
)

// TokenRecord is the kernel-side token metadata retained in memory for M2. M6
// can replace this store with hashed persistent entries without changing the
// Hello validation call sites.
type TokenRecord struct {
	Token     string
	RuntimeID string
	CreatedAt time.Time
}

// Store validates Runtime API Hello bearer tokens.
type Store interface {
	Validate(runtimeID, token string) error
}

// MemoryStore is the M2 in-memory runtime-token store.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]TokenRecord
}

// NewMemoryStore creates an empty in-memory token store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]TokenRecord)}
}

// Set replaces the token record for a runtime id.
func (s *MemoryStore) Set(record TokenRecord) error {
	if s == nil {
		return fmt.Errorf("runtime auth store is nil")
	}
	record.RuntimeID = strings.TrimSpace(record.RuntimeID)
	record.Token = strings.TrimSpace(record.Token)
	if err := wire.ValidateRuntimeID(record.RuntimeID); err != nil {
		return err
	}
	if record.Token == "" {
		return fmt.Errorf("runtime token is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	s.records[record.RuntimeID] = record
	s.mu.Unlock()
	return nil
}

// Validate reports ErrUnauthorized unless runtimeID and token match one record.
func (s *MemoryStore) Validate(runtimeID, token string) error {
	if s == nil {
		return ErrUnauthorized
	}
	runtimeID = strings.TrimSpace(runtimeID)
	token = strings.TrimSpace(token)
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return ErrUnauthorized
	}
	if token == "" {
		return ErrUnauthorized
	}
	s.mu.RLock()
	record, ok := s.records[runtimeID]
	s.mu.RUnlock()
	if !ok {
		return ErrUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(record.Token), []byte(token)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

// Authenticator converts Hello token validation into canonical HelloAck frames.
type Authenticator struct {
	Store    Store
	KernelID string
}

// HelloAck validates hello and returns an accepted or unauthorized response. The
// rejection message intentionally excludes token material.
func (a Authenticator) HelloAck(hello wire.Hello) wire.HelloAck {
	if a.Store == nil || a.Store.Validate(hello.RuntimeID, hello.Token) != nil {
		return wire.HelloAck{
			Op:       wire.OpHelloAck,
			Accepted: false,
			Error: &wire.Error{
				Code:      wire.ErrorUnauthorized,
				Retryable: false,
				Message:   "runtime authentication failed",
			},
		}
	}
	kernelID := strings.TrimSpace(a.KernelID)
	if kernelID == "" {
		kernelID = DefaultKernelID
	}
	return wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: kernelID}
}

// RuntimeTokenPath returns the local runtime token path under TABULA_HOME.
func RuntimeTokenPath(tabulaHome string) string {
	return filepath.Join(tabulaHome, "run", TokenFileName)
}

// IssueLocalTokenFile generates a fresh local runtime token, writes it to path
// with parent/file permissions required by M2-05, and stores the in-memory
// validation record. Existing token files are overwritten and chmodded to 0600.
func IssueLocalTokenFile(store *MemoryStore, path, runtimeID string, now time.Time) (TokenRecord, error) {
	if store == nil {
		return TokenRecord{}, fmt.Errorf("runtime auth store is nil")
	}
	token, err := GenerateToken()
	if err != nil {
		return TokenRecord{}, err
	}
	if err := WriteTokenFile(path, token); err != nil {
		return TokenRecord{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record := TokenRecord{Token: token, RuntimeID: runtimeID, CreatedAt: now.UTC()}
	if err := store.Set(record); err != nil {
		return TokenRecord{}, err
	}
	return record, nil
}

// GenerateToken returns an M2 local bearer token with the required rtk_ prefix.
func GenerateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate runtime token: %w", err)
	}
	return "rtk_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// WriteTokenFile writes token with run-dir 0700 and file 0600 permissions.
func WriteTokenFile(path, token string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("runtime token path is required")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("runtime token is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create runtime token dir: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("chmod runtime token dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write runtime token file: %w", err)
	}
	_, writeErr := f.WriteString(token + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write runtime token file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close runtime token file: %w", closeErr)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod runtime token file: %w", err)
	}
	return nil
}
