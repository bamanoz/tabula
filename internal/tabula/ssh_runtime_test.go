package tabula

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestSSHRuntimeSupervisorsAttachMultipleRuntimes(t *testing.T) {
	hub := kernel.NewHub(nil, 0, 0, nil)
	hub.ConfigureRuntimeRegistryForTest([]kernel.RuntimeDefinition{{ID: "a", Backend: "ssh", Host: "host-a"}, {ID: "b", Backend: "ssh", Host: "host-b"}})
	store := runtimeauth.NewMemoryStore()
	for _, id := range []string{"a", "b"} {
		if err := store.Set(runtimeauth.TokenRecord{RuntimeID: id, Token: "rtk_" + id}); err != nil {
			t.Fatalf("Set %s: %v", id, err)
		}
	}
	connector := &fakeSSHConnector{tokens: map[string]string{"a": "rtk_a", "b": "rtk_b"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSSHRuntimeSupervisorsWithConnector(ctx, hub, []kernel.RuntimeDefinition{{ID: "a", Backend: "ssh", Host: "host-a"}, {ID: "b", Backend: "ssh", Host: "host-b"}}, runtimeauth.Authenticator{Store: store, KernelID: "main"}, nil, connector)
	waitRuntimeAttached(t, hub, "a")
	waitRuntimeAttached(t, hub, "b")
	cancel()
	for _, ch := range done {
		<-ch
	}
}

func TestSSHRuntimeSupervisorReconnectsAfterDisconnect(t *testing.T) {
	hub := kernel.NewHub(nil, 0, 0, nil)
	hub.ConfigureRuntimeRegistryForTest([]kernel.RuntimeDefinition{{ID: "remote", Backend: "ssh", Host: "host"}})
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: "remote", Token: "rtk_remote"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	connector := &fakeSSHConnector{tokens: map[string]string{"remote": "rtk_remote"}, closeAfterHello: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSSHRuntimeSupervisorsWithConnector(ctx, hub, []kernel.RuntimeDefinition{{ID: "remote", Backend: "ssh", Host: "host"}}, runtimeauth.Authenticator{Store: store, KernelID: "main"}, nil, connector)
	waitConnectCount(t, connector, 2)
	cancel()
	for _, ch := range done {
		<-ch
	}
}

type fakeSSHConnector struct {
	mu              sync.Mutex
	count           int
	tokens          map[string]string
	closeAfterHello bool
}

func (f *fakeSSHConnector) connect(ctx context.Context, def kernel.RuntimeDefinition) (*codec.Conn, error) {
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

func waitConnectCount(t *testing.T, connector *fakeSSHConnector, want int) {
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
