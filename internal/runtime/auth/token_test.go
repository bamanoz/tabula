package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestIssueLocalTokenFilePermissionsAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run", TokenFileName)
	store := NewMemoryStore()

	record, err := IssueLocalTokenFile(store, path, LocalRuntimeID, time.Unix(123, 0))
	if err != nil {
		t.Fatalf("IssueLocalTokenFile: %v", err)
	}
	if !strings.HasPrefix(record.Token, "rtk_") || len(record.Token) <= len("rtk_") {
		t.Fatalf("unexpected token format: %q", record.Token)
	}
	if record.RuntimeID != LocalRuntimeID || !record.CreatedAt.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := fileMode(t, filepath.Dir(path)); got != 0o700 {
		t.Fatalf("token dir mode = %#o, want 0700", got)
	}
	if got := fileMode(t, path); got != 0o600 {
		t.Fatalf("token file mode = %#o, want 0600", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(raw)) != record.Token {
		t.Fatalf("token file did not contain issued token")
	}
	if err := store.Validate(LocalRuntimeID, record.Token); err != nil {
		t.Fatalf("Validate correct token: %v", err)
	}
	if err := store.Validate(LocalRuntimeID, record.Token+"x"); err != ErrUnauthorized {
		t.Fatalf("Validate wrong token = %v, want ErrUnauthorized", err)
	}
}

func TestFileStoreIssueValidateListAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime-tokens.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := store.Issue("remote", time.Time{}, time.Unix(123, 0))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if record.Token == "" || !strings.HasPrefix(record.Token, "rtk_") {
		t.Fatalf("unexpected token: %#v", record)
	}
	if got := fileMode(t, filepath.Dir(path)); got != 0o700 {
		t.Fatalf("store dir mode = %#o, want 0700", got)
	}
	if got := fileMode(t, path); got != 0o600 {
		t.Fatalf("store file mode = %#o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), record.Token) {
		t.Fatalf("token store leaked plaintext token: %s", string(data))
	}
	var raw struct {
		Tokens map[string]struct {
			Hash string `json:"hash"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal store: %v", err)
	}
	if raw.Tokens["remote"].Hash == "" {
		t.Fatalf("missing token hash: %s", string(data))
	}
	if err := store.Validate("remote", record.Token); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reload NewFileStore: %v", err)
	}
	if err := reloaded.Validate("remote", record.Token); err != nil {
		t.Fatalf("Validate after reload: %v", err)
	}
	items := reloaded.List()
	if len(items) != 1 || items[0].RuntimeID != "remote" || !items[0].CreatedAt.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("list = %#v", items)
	}
}

func TestFileStoreRevokeRejectsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime-tokens.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := store.Issue("remote", time.Time{}, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.Revoke("remote", time.Unix(2, 0)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := store.Validate("remote", record.Token); err != ErrUnauthorized {
		t.Fatalf("Validate revoked = %v, want ErrUnauthorized", err)
	}
	items := store.List()
	if len(items) != 1 || !items[0].RevokedAt.Equal(time.Unix(2, 0).UTC()) {
		t.Fatalf("revoked list = %#v", items)
	}
}

func TestFileStoreRejectsExpiredToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime-tokens.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := store.Issue("remote", time.Now().Add(-time.Hour), time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.Validate("remote", record.Token); err != ErrUnauthorized {
		t.Fatalf("Validate expired = %v, want ErrUnauthorized", err)
	}
}

func TestChainStoreAcceptsAnyStore(t *testing.T) {
	mem := NewMemoryStore()
	if err := mem.Set(TokenRecord{RuntimeID: LocalRuntimeID, Token: "rtk_local"}); err != nil {
		t.Fatalf("Set memory: %v", err)
	}
	fileStore, err := NewFileStore(filepath.Join(t.TempDir(), "state", "runtime-tokens.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	remote, err := fileStore.Issue("remote", time.Time{}, time.Now())
	if err != nil {
		t.Fatalf("Issue remote: %v", err)
	}
	chain := NewChainStore(mem, fileStore)
	if err := chain.Validate(LocalRuntimeID, "rtk_local"); err != nil {
		t.Fatalf("Validate local: %v", err)
	}
	if err := chain.Validate("remote", remote.Token); err != nil {
		t.Fatalf("Validate remote: %v", err)
	}
	if err := chain.Validate("missing", "rtk_missing"); err != ErrUnauthorized {
		t.Fatalf("Validate missing = %v, want ErrUnauthorized", err)
	}
}

func TestIssueLocalTokenFileRegeneratesAndRejectsOldToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", TokenFileName)
	store := NewMemoryStore()
	first, err := IssueLocalTokenFile(store, path, LocalRuntimeID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	second, err := IssueLocalTokenFile(store, path, LocalRuntimeID, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("second issue: %v", err)
	}
	if first.Token == second.Token {
		t.Fatal("token was not regenerated")
	}
	if err := store.Validate(LocalRuntimeID, first.Token); err != ErrUnauthorized {
		t.Fatalf("old token validation = %v, want ErrUnauthorized", err)
	}
	if err := store.Validate(LocalRuntimeID, second.Token); err != nil {
		t.Fatalf("new token validation: %v", err)
	}
}

func TestAuthenticatorHelloAckDoesNotLeakToken(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Set(TokenRecord{RuntimeID: LocalRuntimeID, Token: "rtk_good", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	authn := Authenticator{Store: store, KernelID: "main"}

	accepted := authn.HelloAck(wire.Hello{Op: wire.OpHello, RuntimeID: LocalRuntimeID, Token: "rtk_good", ProtocolVersion: "1"})
	if !accepted.Accepted || accepted.KernelID != "main" || accepted.Error != nil {
		t.Fatalf("accepted ack = %#v", accepted)
	}
	rejected := authn.HelloAck(wire.Hello{Op: wire.OpHello, RuntimeID: LocalRuntimeID, Token: "rtk_bad_secret", ProtocolVersion: "1"})
	if rejected.Accepted || rejected.Error == nil || rejected.Error.Code != wire.ErrorUnauthorized {
		t.Fatalf("rejected ack = %#v", rejected)
	}
	if strings.Contains(rejected.Error.Message, "rtk_bad_secret") || strings.Contains(rejected.Error.Message, "rtk_good") {
		t.Fatalf("auth error leaked token material: %q", rejected.Error.Message)
	}
}

func TestAuthenticatorHelloAckContextRejectsMismatchedPeerCertCN(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Set(TokenRecord{RuntimeID: LocalRuntimeID, Token: "rtk_good", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	authn := Authenticator{Store: store, KernelID: "main"}
	ack := authn.HelloAckContext(ContextWithPeerCertCN(context.Background(), "remote"), wire.Hello{Op: wire.OpHello, RuntimeID: LocalRuntimeID, Token: "rtk_good", ProtocolVersion: "1"})
	if ack.Accepted || ack.Error == nil || ack.Error.Code != wire.ErrorUnauthorized {
		t.Fatalf("expected unauthorized mismatched cert cn, got %#v", ack)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
