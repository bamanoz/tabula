package tabula

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimecodec "github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestRuntimeTokenIssueListRevokeJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	var out bytes.Buffer
	if code := runtimeTokenIssueCmd([]string{"--runtime-id", "remote", "--json"}, &out); code != 0 {
		t.Fatalf("issue exit = %d", code)
	}
	var issued runtimeTokenIssueRecord
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &issued); err != nil {
		t.Fatalf("unmarshal issued: %v", err)
	}
	if issued.RuntimeID != "remote" || !strings.HasPrefix(issued.Token, "rtk_") || issued.CreatedAt == "" {
		t.Fatalf("issued = %#v", issued)
	}
	storePath := runtimeauth.RuntimeTokenStorePath(home)
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read token store: %v", err)
	}
	if strings.Contains(string(data), issued.Token) {
		t.Fatalf("token store leaked plaintext token: %s", string(data))
	}
	if _, err := os.Stat(filepath.Dir(storePath)); err != nil {
		t.Fatalf("stat token store dir: %v", err)
	}

	out.Reset()
	if code := runtimeTokenListCmd([]string{"--json"}, &out); code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	var listed []runtimeTokenListRecord
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &listed); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listed) != 1 || listed[0].RuntimeID != "remote" || listed[0].CreatedAt == "" || strings.Contains(out.String(), issued.Token) {
		t.Fatalf("listed = %#v raw=%s", listed, out.String())
	}

	out.Reset()
	detached := ""
	if code := runtimeTokenRevokeCmd([]string{"--runtime-id", "remote", "--json"}, &out, func(runtimeID string) { detached = runtimeID }); code != 0 {
		t.Fatalf("revoke exit = %d", code)
	}
	if detached != "remote" {
		t.Fatalf("detach callback = %q", detached)
	}
	if !strings.Contains(out.String(), `"revoked":true`) {
		t.Fatalf("unexpected revoke output: %s", out.String())
	}
	out.Reset()
	if code := runtimeTokenListCmd([]string{"--json"}, &out); code != 0 {
		t.Fatalf("list after revoke exit = %d", code)
	}
	if !strings.Contains(out.String(), `"revoked_at"`) {
		t.Fatalf("revoked_at missing from list: %s", out.String())
	}
}

func TestRuntimeTokenIssueRejectsInvalidExpiresIn(t *testing.T) {
	t.Setenv("TABULA_HOME", t.TempDir())
	if code := runtimeTokenIssueCmd([]string{"--runtime-id", "remote", "--expires-in", "90d"}, &bytes.Buffer{}); code == 0 {
		t.Fatal("expected invalid Go duration to fail")
	}
}

func TestWatchRuntimeTokenRevocationsDetachesRuntime(t *testing.T) {
	home := t.TempDir()
	store, err := runtimeauth.NewFileStore(runtimeauth.RuntimeTokenStorePath(home))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := store.Issue("remote", time.Time{}, time.Now())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	hub := kernel.NewHub(nil, nil)
	writeRuntimeConfigForTest(t, home, `[[runtime]]
id = "remote"
backend = "wss"
`)
	if err := configureKernelRuntimeRegistry(home, hub, nil); err != nil {
		t.Fatalf("ConfigureRuntimeRegistry: %v", err)
	}
	clientConn, serverConn := net.Pipe()
	client := runtimecodec.NewReadWriteCloser(clientConn)
	server := runtimecodec.NewReadWriteCloser(serverConn)
	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, kernel.RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()
	defer func() {
		_ = client.CloseNow()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("runtime attach did not stop")
		}
	}()
	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: record.Token, ProtocolVersion: "1"}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go watchRuntimeTokenRevocations(store, hub, stop)
	if err := store.Revoke("remote", time.Now()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !hub.RuntimeAttached("remote") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("runtime was not detached after revoke")
}

func writeRuntimeConfigForTest(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, "config", "global.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
}
