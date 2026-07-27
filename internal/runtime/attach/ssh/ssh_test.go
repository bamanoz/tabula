package sshattach

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestSupervisorsAttachMultipleRuntimes(t *testing.T) {
	hub := kernel.NewHub(nil, nil)
	configureKernelRuntimes(t, hub, `[[runtime]]
id = "a"
backend = "ssh"
host = "host-a"

[[runtime]]
id = "b"
backend = "ssh"
host = "host-b"
`)
	store := runtimeauth.NewMemoryStore()
	for _, id := range []string{"a", "b"} {
		if err := store.Set(runtimeauth.TokenRecord{RuntimeID: id, Token: "rtk_" + id}); err != nil {
			t.Fatalf("Set %s: %v", id, err)
		}
	}
	connector := &fakeConnector{tokens: map[string]string{"a": "rtk_a", "b": "rtk_b"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serve := serveWithHub(hub, runtimeauth.Authenticator{Store: store, KernelID: "main"})
	done := StartSupervisorsWithConnector(ctx, []Definition{{ID: "a", Backend: "ssh", Host: "host-a"}, {ID: "b", Backend: "ssh", Host: "host-b"}}, serve, nil, connector)
	waitRuntimeAttached(t, hub, "a")
	waitRuntimeAttached(t, hub, "b")
	cancel()
	for _, ch := range done {
		<-ch
	}
}

func TestSupervisorReconnectsAfterDisconnect(t *testing.T) {
	hub := kernel.NewHub(nil, nil)
	configureKernelRuntimes(t, hub, `[[runtime]]
id = "remote"
backend = "ssh"
host = "host"
`)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: "remote", Token: "rtk_remote"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	connector := &fakeConnector{tokens: map[string]string{"remote": "rtk_remote"}, closeAfterHello: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serve := serveWithHub(hub, runtimeauth.Authenticator{Store: store, KernelID: "main"})
	done := StartSupervisorsWithConnector(ctx, []Definition{{ID: "remote", Backend: "ssh", Host: "host"}}, serve, nil, connector)
	waitConnectCount(t, connector, 2)
	cancel()
	for _, ch := range done {
		<-ch
	}
}

func configureKernelRuntimes(t *testing.T, hub *kernel.Hub, body string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config", "global.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir kernel config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write kernel config: %v", err)
	}
	defs, err := runtimeconfig.LoadDefinitions(home)
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	if err := hub.ConfigureRuntimeRegistry(defs, nil); err != nil {
		t.Fatalf("ConfigureRuntimeRegistry: %v", err)
	}
}

func serveWithHub(hub *kernel.Hub, auth runtimeauth.Authenticator) ServeFunc {
	return func(ctx context.Context, conn *codec.Conn) error {
		return hub.ServeAuthenticatedRuntime(ctx, conn, kernel.RuntimeAttachOptions{Auth: auth})
	}
}

type fakeConnector struct {
	mu              sync.Mutex
	count           int
	tokens          map[string]string
	closeAfterHello bool
}

func (f *fakeConnector) Connect(ctx context.Context, def Definition) (*codec.Conn, error) {
	f.mu.Lock()
	f.count++
	f.mu.Unlock()
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	client := stdio.NewConn(clientRead, clientWrite)
	server := stdio.NewConn(serverRead, serverWrite)
	go func() {
		defer func() { _ = server.CloseNow() }()
		if err := server.Write(ctx, wire.Hello{Op: wire.OpHello, RuntimeID: def.ID, Token: f.tokens[def.ID], ProtocolVersion: "1", Capabilities: []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: def.ID + "-plugin"}, Tools: []wire.ToolSpec{{Name: def.ID + "_tool"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}}); err != nil {
			return
		}
		_, _, _ = server.Read(ctx)
		if f.closeAfterHello {
			return
		}
		<-ctx.Done()
	}()
	return client, nil
}

func waitRuntimeAttached(t *testing.T, hub *kernel.Hub, runtimeID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.RuntimeAttached(runtimeID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime %s did not attach", runtimeID)
}

func waitConnectCount(t *testing.T, connector *fakeConnector, want int) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		connector.mu.Lock()
		got := connector.count
		connector.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("connect count did not reach %d", want)
}
