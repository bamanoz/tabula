// Package auth provides kernel-side Runtime API authentication primitives.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Token      string
	RuntimeID  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	RevokedAt  time.Time
}

type TokenMetadata struct {
	RuntimeID  string    `json:"runtime_id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
}

// Store validates Runtime API Hello bearer tokens.
type Store interface {
	Validate(runtimeID, token string) error
}

type ChainStore struct {
	stores []Store
}

func NewChainStore(stores ...Store) ChainStore {
	out := make([]Store, 0, len(stores))
	for _, store := range stores {
		if store != nil {
			out = append(out, store)
		}
	}
	return ChainStore{stores: out}
}

func (s ChainStore) Validate(runtimeID, token string) error {
	for _, store := range s.stores {
		if store.Validate(runtimeID, token) == nil {
			return nil
		}
	}
	return ErrUnauthorized
}

// MemoryStore is the M2 in-memory runtime-token store.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]TokenRecord
}

// FileStore persists runtime token hashes under TABULA_HOME/state.
type FileStore struct {
	mu      sync.Mutex
	path    string
	records map[string]fileTokenRecord
}

type fileTokenRecord struct {
	RuntimeID  string    `json:"runtime_id"`
	Hash       string    `json:"hash"`
	Salt       string    `json:"salt"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
}

type fileTokenData struct {
	Version int                        `json:"version"`
	Tokens  map[string]fileTokenRecord `json:"tokens"`
}

func RuntimeTokenStorePath(tabulaHome string) string {
	return filepath.Join(tabulaHome, "state", "runtime-tokens.json")
}

func NewFileStore(path string) (*FileStore, error) {
	store := &FileStore{path: strings.TrimSpace(path), records: map[string]fileTokenRecord{}}
	if store.path == "" {
		return nil, fmt.Errorf("runtime token store path is required")
	}
	if err := store.loadLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) Issue(runtimeID string, expiresAt time.Time, now time.Time) (TokenRecord, error) {
	if s == nil {
		return TokenRecord{}, fmt.Errorf("runtime auth store is nil")
	}
	runtimeID = strings.TrimSpace(runtimeID)
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return TokenRecord{}, err
	}
	token, err := GenerateToken()
	if err != nil {
		return TokenRecord{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	salt, err := randomSalt()
	if err != nil {
		return TokenRecord{}, err
	}
	record := fileTokenRecord{RuntimeID: runtimeID, Salt: salt, Hash: hashToken(salt, token), CreatedAt: now.UTC()}
	if !expiresAt.IsZero() {
		record.ExpiresAt = expiresAt.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[runtimeID] = record
	if err := s.saveLocked(); err != nil {
		return TokenRecord{}, err
	}
	return TokenRecord{RuntimeID: runtimeID, Token: token, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt}, nil
}

func (s *FileStore) Validate(runtimeID, token string) error {
	if s == nil {
		return ErrUnauthorized
	}
	runtimeID = strings.TrimSpace(runtimeID)
	token = strings.TrimSpace(token)
	if runtimeID == "" || token == "" || wire.ValidateRuntimeID(runtimeID) != nil {
		return ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return ErrUnauthorized
	}
	record, ok := s.records[runtimeID]
	if !ok || !record.RevokedAt.IsZero() || (!record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt)) {
		return ErrUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(record.Hash), []byte(hashToken(record.Salt, token))) != 1 {
		return ErrUnauthorized
	}
	record.LastSeenAt = time.Now().UTC()
	s.records[runtimeID] = record
	_ = s.saveLocked()
	return nil
}

func (s *FileStore) List() []TokenMetadata {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	out := make([]TokenMetadata, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, TokenMetadata{RuntimeID: record.RuntimeID, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt, LastSeenAt: record.LastSeenAt, RevokedAt: record.RevokedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuntimeID < out[j].RuntimeID })
	return out
}

func (s *FileStore) Revoke(runtimeID string, now time.Time) error {
	if s == nil {
		return fmt.Errorf("runtime auth store is nil")
	}
	runtimeID = strings.TrimSpace(runtimeID)
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[runtimeID]
	if !ok {
		return ErrUnauthorized
	}
	record.RevokedAt = now.UTC()
	s.records[runtimeID] = record
	return s.saveLocked()
}

func (s *FileStore) loadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read runtime token store: %w", err)
	}
	var file fileTokenData
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse runtime token store: %w", err)
	}
	if file.Tokens == nil {
		file.Tokens = map[string]fileTokenRecord{}
	}
	s.records = file.Tokens
	return nil
}

func (s *FileStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create runtime token store dir: %w", err)
	}
	file := fileTokenData{Version: 1, Tokens: s.records}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}

func randomSalt() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate token salt: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func hashToken(salt, token string) string {
	sum := sha256.Sum256([]byte(salt + ":" + token))
	return "sha256:" + base64.RawURLEncoding.EncodeToString(sum[:])
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
	return a.HelloAckContext(context.Background(), hello)
}

// HelloAckContext validates hello and optionally binds it to transport identity.
func (a Authenticator) HelloAckContext(ctx context.Context, hello wire.Hello) wire.HelloAck {
	if peerCN := peerCertCN(ctx); peerCN != "" && peerCN != hello.RuntimeID {
		return wire.HelloAck{
			Op:       wire.OpHelloAck,
			Accepted: false,
			Error: &wire.Error{
				Code:      wire.ErrorUnauthorized,
				Retryable: false,
				Message:   "runtime certificate identity does not match runtime_id",
			},
		}
	}
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

type peerCertCNKey struct{}

// ContextWithPeerCertCN annotates one auth context with the peer cert CN.
func ContextWithPeerCertCN(ctx context.Context, cn string) context.Context {
	if strings.TrimSpace(cn) == "" {
		return ctx
	}
	return context.WithValue(ctx, peerCertCNKey{}, strings.TrimSpace(cn))
}

func peerCertCN(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(peerCertCNKey{}).(string)
	return strings.TrimSpace(value)
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
