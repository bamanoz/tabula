package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestServeAuthenticatedRuntimeRegistersAndDetachesRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	ack, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       runtimeauth.LocalRuntimeID,
		Token:           "rtk_good",
		ProtocolVersion: "1",
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []wire.ToolSpec{{Name: "read"}, {Name: "write"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}},
	})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted || ack.KernelID != "main" {
		t.Fatalf("unexpected ack: %#v", ack)
	}

	waitForRuntimeSnapshot(t, hub, true)
	_ = client.Close(websocket.StatusNormalClosure, "test close")
	<-done
	assertRuntimeSnapshot(t, hub.SnapshotRuntimes(), false)
}

func TestServeAuthenticatedRuntimeRejectsWrongTokenWithoutRegistering(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	ack, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_bad_secret", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if ack.Accepted || ack.Error == nil || ack.Error.Code != wire.ErrorUnauthorized {
		t.Fatalf("expected unauthorized rejection, got %#v", ack)
	}
	if ack.Error.Message == "rtk_bad_secret" {
		t.Fatalf("auth error leaked token material: %#v", ack.Error)
	}
	if err := <-done; err != nil {
		t.Fatalf("ServeAuthenticatedRuntime returned error: %v", err)
	}
	assertNoRuntimes(t, hub.SnapshotRuntimes())
}

func assertRuntimeSnapshot(t *testing.T, raw []byte, attached bool) {
	t.Helper()
	var body struct {
		Runtimes []struct {
			ID           string   `json:"id"`
			Attached     bool     `json:"attached"`
			PID          int      `json:"pid"`
			Capabilities []string `json:"capabilities"`
			Targets      []struct {
				Kind  string   `json:"kind"`
				ID    string   `json:"id"`
				Tools []string `json:"tools"`
			} `json:"targets"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal runtime snapshot: %v", err)
	}
	if len(body.Runtimes) != 1 {
		t.Fatalf("expected one runtime, got %s", string(raw))
	}
	got := body.Runtimes[0]
	if got.ID != runtimeauth.LocalRuntimeID || got.Attached != attached {
		t.Fatalf("unexpected runtime attachment: %+v", got)
	}
	if attached && (len(got.Capabilities) != 2 || got.Capabilities[0] != "read" || got.Capabilities[1] != "write" || len(got.Targets) != 1 || got.Targets[0].ID != "fs") {
		t.Fatalf("runtime capabilities not captured: %+v", got)
	}
}

func TestServeAuthenticatedRuntimeSnapshotsRuntimePIDWhenKnown(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth:       runtimeauth.Authenticator{Store: store, KernelID: "main"},
			RuntimePID: 12346,
		})
	}()

	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       runtimeauth.LocalRuntimeID,
		Token:           "rtk_good",
		ProtocolVersion: "1",
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []wire.ToolSpec{{Name: "read"}, {Name: "write"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}},
	}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	waitForRuntimeSnapshot(t, hub, true)

	var body struct {
		Runtimes []struct {
			PID int `json:"pid"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(hub.SnapshotRuntimes(), &body); err != nil {
		t.Fatalf("unmarshal runtime snapshot: %v", err)
	}
	if len(body.Runtimes) != 1 || body.Runtimes[0].PID != 12346 {
		t.Fatalf("expected runtime pid 12346 in snapshot, got %s", string(hub.SnapshotRuntimes()))
	}
	_ = client.Close(websocket.StatusNormalClosure, "test close")
	<-done
}

func TestServeAuthenticatedRuntimeCatalogUpdatePopulatesRuntimeDispatchAndSnapshot(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       runtimeauth.LocalRuntimeID,
		Token:           "rtk_good",
		ProtocolVersion: "1",
		Capabilities: []wire.Capability{{
			Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
			Tools:  []wire.ToolSpec{{Name: "seed"}},
			State:  wire.CapabilityStateManifestLoaded,
			Source: wire.CapabilitySourceManifest,
		}},
	}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if err := client.Write(context.Background(), wire.CatalogUpdate{
		Op:         wire.OpCatalogUpdate,
		Target:     wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:      []wire.ToolSpec{{Name: "echo", DeadlineMS: 2500}},
		Hooks:      []wire.HookSpec{{Event: "before_tool_call", Priority: 10}},
		Revision:   2,
		State:      wire.CapabilityStateReady,
		Source:     wire.CapabilitySourceWorker,
		Diagnostic: "worker ready",
	}); err != nil {
		t.Fatalf("catalog_update: %v", err)
	}
	waitForToolDispatch(t, hub, "seed", func(entry toolDispatch) bool {
		return entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeauth.LocalRuntimeID
	})
	if err := client.Write(context.Background(), wire.LifecycleNotice{
		Op:      wire.OpLifecycleNotice,
		Target:  wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		State:   wire.LifecycleStateReady,
		PID:     4242,
		Message: "running",
	}); err != nil {
		t.Fatalf("lifecycle_notice: %v", err)
	}

	entry := waitForToolDispatch(t, hub, "echo", func(entry toolDispatch) bool {
		return entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeauth.LocalRuntimeID
	})
	if entry.Source != toolSourceRuntime || entry.RuntimeID != runtimeauth.LocalRuntimeID {
		t.Fatalf("runtime dispatch not installed: entry=%+v", entry)
	}
	if entries := hub.hooks.entries("before_tool_call"); len(entries) != 1 || entries[0].sub.Name() != "runtime:local:plugin:fs" {
		t.Fatalf("runtime hook subscriber not indexed: %+v", entries)
	}

	var body struct {
		Runtimes []struct {
			Targets []struct {
				ID             string   `json:"id"`
				Tools          []string `json:"tools"`
				Hooks          []string `json:"hooks"`
				State          string   `json:"state"`
				LifecycleState string   `json:"lifecycle_state"`
				PID            int      `json:"pid"`
				Diagnostic     string   `json:"diagnostic"`
			} `json:"targets"`
		} `json:"runtimes"`
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := json.Unmarshal(hub.SnapshotRuntimes(), &body); err == nil && len(body.Runtimes) == 1 && len(body.Runtimes[0].Targets) == 1 {
			target := body.Runtimes[0].Targets[0]
			if target.PID == 4242 && target.LifecycleState == string(wire.LifecycleStateReady) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(body.Runtimes) != 1 || len(body.Runtimes[0].Targets) != 1 {
		t.Fatalf("unexpected runtime snapshot: %s", string(hub.SnapshotRuntimes()))
	}
	target := body.Runtimes[0].Targets[0]
	if target.ID != "fs" || len(target.Tools) != 1 || target.Tools[0] != "echo" || len(target.Hooks) != 1 || target.Hooks[0] != "before_tool_call" || target.State != string(wire.CapabilityStateReady) || target.LifecycleState != string(wire.LifecycleStateReady) || target.PID != 4242 || target.Diagnostic != string(wire.LifecycleStateReady) {
		t.Fatalf("unexpected target snapshot: %+v", target)
	}

	_ = client.Close(websocket.StatusNormalClosure, "test close")
	<-done
}

func TestServeAuthenticatedRuntimeInitialCapabilitiesReachInitTools(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       runtimeauth.LocalRuntimeID,
		Token:           "rtk_good",
		ProtocolVersion: "1",
		Capabilities: []wire.Capability{{
			Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "recorder"},
			Tools:  []wire.ToolSpec{{Name: "testbed_hook_recorder_clear"}, {Name: "testbed_hook_recorder_events"}},
			State:  wire.CapabilityStateManifestLoaded,
			Source: wire.CapabilitySourceManifest,
		}, {
			Target:      wire.Target{Kind: wire.TargetKindSkill, ID: "skill:timer"},
			Tools:       []wire.ToolSpec{{Name: "timer_start"}},
			State:       wire.CapabilityStateManifestLoaded,
			Source:      wire.CapabilitySourceManifest,
			WorkerMode:  wire.WorkerModeCold,
			HarnessKind: wire.HarnessKindPython,
		}},
	}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw := hub.initToolsJSON()
		if strings.Contains(string(raw), `"testbed_hook_recorder_clear"`) && strings.Contains(string(raw), `"testbed_hook_recorder_events"`) && !strings.Contains(string(raw), `"timer_start"`) {
			_ = client.CloseNow()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = client.CloseNow()
	<-done
	t.Fatalf("initial runtime capabilities missing from init tools: %s", string(hub.initToolsJSON()))
}

func TestServeAuthenticatedRuntimeAsyncFramesRouteHookRepliesAndBusMessages(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	sessionRecv := addCaptureClient(t, hub, "session-recorder", "s1", []string{"plugin_event"}, nil)
	globalRecv := addCaptureClient(t, hub, "global-recorder", "", nil, []string{"plugin_event"})

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good", ProtocolVersion: "1"}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}

	hookResultCh := make(chan *HookResult, 1)
	hub.hooks.addPendingRuntimeHook("hook-1", runtimeauth.LocalRuntimeID, hookResultCh)
	defer hub.hooks.removePendingHook("hook-1")

	if err := client.Write(context.Background(), wire.PluginSend{
		Op:        wire.OpPluginSend,
		Target:    wire.Target{Kind: wire.TargetKindPlugin, ID: "observer"},
		Channel:   "bus",
		Type:      "plugin_event",
		SessionID: "s1",
		Payload:   json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("plugin_send: %v", err)
	}
	if err := client.Write(context.Background(), wire.HookEventReply{
		Op:     wire.OpHookEventReply,
		CallID: "hook-1",
		Action: wire.HookActionRewrite,
		Data:   json.RawMessage(`{"tool":"safe"}`),
		Reason: "rewritten",
	}); err != nil {
		t.Fatalf("hook_event_reply: %v", err)
	}

	for name, ch := range map[string]<-chan *Message{
		"session": sessionRecv.recvCh,
		"global":  globalRecv.recvCh,
	} {
		got := waitForMessage(t, ch)
		if got.Type != "plugin_event" || got.Session != "s1" || string(got.Payload) != `{"ok":true}` {
			t.Fatalf("%s receiver got unexpected message: %+v payload=%s", name, got, string(got.Payload))
		}
	}
	select {
	case result := <-hookResultCh:
		if result == nil || result.Action != string(ActionModify) || string(result.Payload) != `{"tool":"safe"}` || result.Reason != "rewritten" {
			t.Fatalf("unexpected hook result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hook result")
	}

	_ = client.Close(websocket.StatusNormalClosure, "test close")
	<-done
}

func TestServeAuthenticatedRuntimeDetachRemovesRuntimeTools(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()
	driver := addTenantCaptureClient(t, hub, tenant.DefaultID, "driver", "main", []string{TopicSessionInit}, nil)

	done := make(chan error, 1)
	go func() {
		done <- hub.ServeAuthenticatedRuntime(context.Background(), server, RuntimeAttachOptions{
			Auth: runtimeauth.Authenticator{Store: store, KernelID: "main"},
		})
	}()

	if _, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good", ProtocolVersion: "1"}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if err := client.Write(context.Background(), wire.CatalogUpdate{
		Op:       wire.OpCatalogUpdate,
		Target:   wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:    []wire.ToolSpec{{Name: "echo"}},
		Revision: 1,
		State:    wire.CapabilityStateReady,
		Source:   wire.CapabilitySourceWorker,
	}); err != nil {
		t.Fatalf("catalog_update: %v", err)
	}
	waitForToolDispatch(t, hub, "echo", func(entry toolDispatch) bool {
		return entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeauth.LocalRuntimeID
	})
	if msg := readCaptureMessageTimeout(driver.recvCh, time.Second); msg == nil {
		t.Fatal("expected session.init after catalog update")
	}

	_ = client.Close(websocket.StatusNormalClosure, "test close")
	if err := <-done; err != nil {
		t.Fatalf("ServeAuthenticatedRuntime returned error: %v", err)
	}
	if entry, ok := toolDispatchEntry(hub, "echo"); ok {
		t.Fatalf("runtime tool dispatch still present after detach: %+v", entry)
	}
	msg := readCaptureMessageTimeout(driver.recvCh, time.Second)
	if msg == nil {
		t.Fatal("expected session.init refresh after runtime detach")
	}
	if msg.Type != string(MsgEvent) || msg.Topic != TopicSessionInit {
		t.Fatalf("expected session.init after detach, got %+v", msg)
	}
	if strings.Contains(string(msg.Tools), "echo") {
		t.Fatalf("expected refreshed catalog without echo, got %s", string(msg.Tools))
	}
}

func TestReloadAttachedRuntimeUsesAttachedConn(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	rc := runtimemock.New()
	if err := hub.runtimes.RegisterHello(runtimeauth.LocalRuntimeID, rc, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	attempted, err := hub.ReloadAttachedRuntime(context.Background(), runtimeauth.LocalRuntimeID, &target, "alpha")
	if err != nil {
		t.Fatalf("ReloadAttachedRuntime: %v", err)
	}
	if !attempted {
		t.Fatal("expected attached runtime reload to be attempted")
	}
	reloads := rc.RecordedReloads()
	if len(reloads) != 1 || reloads[0].Target == nil || reloads[0].Target.ID != "fs" || len(reloads[0].Tenants) != 1 || reloads[0].Tenants[0] != "alpha" {
		t.Fatalf("recorded reloads = %#v", reloads)
	}

	attempted, err = hub.ReloadAttachedRuntime(context.Background(), "missing", nil)
	if err != nil {
		t.Fatalf("missing runtime reload: %v", err)
	}
	if attempted {
		t.Fatal("unexpected reload attempt for missing runtime")
	}
}

func TestRuntimeAttachedTracksRegistryState(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	if hub.RuntimeAttached(runtimeauth.LocalRuntimeID) {
		t.Fatal("expected runtime to start detached")
	}
	hub.runtimes = NewRuntimeRegistry()
	rc := runtimemock.New()
	if err := hub.runtimes.RegisterHello(runtimeauth.LocalRuntimeID, rc, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	if !hub.RuntimeAttached(runtimeauth.LocalRuntimeID) {
		t.Fatal("expected attached runtime to be reported")
	}
	hub.runtimes.MarkDetached(runtimeauth.LocalRuntimeID, nil)
	if hub.RuntimeAttached(runtimeauth.LocalRuntimeID) {
		t.Fatal("expected detached runtime to be reported as detached")
	}
}

func TestServeAuthenticatedRuntimeUnixAndWSSCoexist(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: "unix-runtime", Token: "rtk_unix"}); err != nil {
		t.Fatalf("store unix token: %v", err)
	}
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: "wss-runtime", Token: "rtk_wss"}); err != nil {
		t.Fatalf("store wss token: %v", err)
	}
	auth := runtimeauth.Authenticator{Store: store, KernelID: "main"}

	unixClientWS, unixServerWS := runtimeConnWebsocketNetPipe(t)
	unixClient := codec.New(unixClientWS)
	defer func() { _ = unixClient.CloseNow() }()
	unixServer := codec.New(unixServerWS)
	unixDone := make(chan error, 1)
	go func() {
		unixDone <- hub.ServeAuthenticatedRuntime(context.Background(), unixServer, RuntimeAttachOptions{Auth: auth})
	}()
	if _, err := runtimeconn.Handshake(context.Background(), unixClient, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       "unix-runtime",
		Token:           "rtk_unix",
		ProtocolVersion: "1",
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "unix-fs"}, Tools: []wire.ToolSpec{{Name: "unix_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}},
	}); err != nil {
		t.Fatalf("unix Handshake: %v", err)
	}

	mux := http.NewServeMux()
	wssAccepted := make(chan struct{})
	wssDone := make(chan error, 1)
	wss.Listener{}.Mount(mux, func(ctx context.Context, c *codec.Conn) {
		close(wssAccepted)
		wssDone <- hub.ServeAuthenticatedRuntime(ctx, c, RuntimeAttachOptions{Auth: auth})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	wssClient, err := wss.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+wss.DefaultPath, wss.DialOptions{})
	if err != nil {
		t.Fatalf("wss Dial: %v", err)
	}
	defer func() { _ = wssClient.CloseNow() }()
	waitForRuntimeTest(t, wssAccepted, "wss runtime accept")
	if _, err := runtimeconn.Handshake(context.Background(), wssClient, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       "wss-runtime",
		Token:           "rtk_wss",
		ProtocolVersion: "1",
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "wss-fs"}, Tools: []wire.ToolSpec{{Name: "wss_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}},
	}); err != nil {
		t.Fatalf("wss Handshake: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var body struct {
			Runtimes []struct {
				ID       string `json:"id"`
				Attached bool   `json:"attached"`
			} `json:"runtimes"`
		}
		if err := json.Unmarshal(hub.SnapshotRuntimes(), &body); err == nil && len(body.Runtimes) == 2 {
			seen := map[string]bool{}
			for _, runtime := range body.Runtimes {
				if runtime.Attached {
					seen[runtime.ID] = true
				}
			}
			if seen["unix-runtime"] && seen["wss-runtime"] {
				if _, ok := toolDispatchEntry(hub, "unix_read"); !ok {
					t.Fatalf("unix tool dispatch missing")
				}
				if _, ok := toolDispatchEntry(hub, "wss_read"); !ok {
					t.Fatalf("wss tool dispatch missing")
				}
				_ = unixClient.Close(websocket.StatusNormalClosure, "test close")
				_ = wssClient.Close(websocket.StatusNormalClosure, "test close")
				<-unixDone
				<-wssDone
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected unix and wss runtimes attached, got %s", string(hub.SnapshotRuntimes()))
}

func TestSetAttachedRuntimePIDUpdatesSnapshot(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	rc := runtimemock.New()
	if err := hub.runtimes.RegisterHello(runtimeauth.LocalRuntimeID, rc, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.SetAttachedRuntimePID(runtimeauth.LocalRuntimeID, 4567)
	var body struct {
		Runtimes []struct {
			PID int `json:"pid"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(hub.SnapshotRuntimes(), &body); err != nil {
		t.Fatalf("unmarshal runtime snapshot: %v", err)
	}
	if len(body.Runtimes) != 1 || body.Runtimes[0].PID != 4567 {
		t.Fatalf("expected runtime pid 4567 in snapshot, got %s", string(hub.SnapshotRuntimes()))
	}
}

func TestDetachRuntimeForRevokeClosesRuntimeAndRemovesTools(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	rc := runtimemock.New()
	if err := hub.runtimes.RegisterHello("remote", rc, []runtimeapi.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("remote", runtimeapi.Capability{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})
	driver := addTenantCaptureClient(t, hub, "code-immune-tabula-dev", "driver", "main", []string{TopicSessionInit}, nil)
	if _, ok := toolDispatchEntry(hub, "fs_read"); !ok {
		t.Fatal("expected fs_read dispatch before revoke")
	}
	hub.DetachRuntimeForRevoke("remote")
	if hub.RuntimeAttached("remote") {
		t.Fatal("runtime still attached after revoke")
	}
	if _, ok := toolDispatchEntry(hub, "fs_read"); ok {
		t.Fatal("runtime tool dispatch still present after revoke")
	}
	resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "after-revoke"})
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable {
		t.Fatalf("expected closed runtime unavailable, got resp=%#v err=%v", resp, err)
	}
	msg := readCaptureMessageTimeout(driver.recvCh, time.Second)
	if msg == nil {
		t.Fatal("expected session.init refresh after revoke")
	}
	if msg.Type != string(MsgEvent) || msg.Topic != TopicSessionInit {
		t.Fatalf("expected session.init after revoke, got %+v", msg)
	}
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(msg.Tools, &tools); err != nil {
		t.Fatalf("unmarshal session tools: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "fs_read" {
			t.Fatalf("expected refreshed catalog without fs_read, got %s", string(msg.Tools))
		}
	}
}

func waitForRuntimeSnapshot(t *testing.T, hub *Hub, attached bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw := hub.SnapshotRuntimes()
		var body struct {
			Runtimes []struct {
				Attached bool `json:"attached"`
			} `json:"runtimes"`
		}
		if json.Unmarshal(raw, &body) == nil && len(body.Runtimes) == 1 && body.Runtimes[0].Attached == attached {
			assertRuntimeSnapshot(t, raw, attached)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertRuntimeSnapshot(t, hub.SnapshotRuntimes(), attached)
}

func waitForRuntimeTest(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func assertNoRuntimes(t *testing.T, raw []byte) {
	t.Helper()
	var body struct {
		Runtimes []any `json:"runtimes"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal runtime snapshot: %v", err)
	}
	if len(body.Runtimes) != 0 {
		t.Fatalf("expected no runtimes, got %s", string(raw))
	}
}

func runtimeConnWebsocketNetPipe(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	var serverConn *websocket.Conn
	transport := runtimeConnFakeTransport{handler: func(w http.ResponseWriter, r *http.Request) {
		var err error
		serverConn, err = websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{codec.Subprotocol}, InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
		}
	}}
	clientConn, _, err := websocket.Dial(context.Background(), "ws://runtime.local/runtime", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, Subprotocols: []string{codec.Subprotocol}})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if serverConn == nil {
		t.Fatal("server websocket was not accepted")
	}
	return clientConn, serverConn
}

type runtimeConnFakeTransport struct {
	handler http.HandlerFunc
}

func (t runtimeConnFakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientConn, serverConn := net.Pipe()
	hj := runtimeConnTestHijacker{ResponseRecorder: httptest.NewRecorder(), serverConn: serverConn}
	t.handler.ServeHTTP(hj, r)
	resp := hj.ResponseRecorder.Result()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		resp.Body = clientConn
	}
	return resp, nil
}

type runtimeConnTestHijacker struct {
	*httptest.ResponseRecorder
	serverConn net.Conn
}

var _ http.Hijacker = runtimeConnTestHijacker{}

func (h runtimeConnTestHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.serverConn, bufio.NewReadWriter(bufio.NewReader(h.serverConn), bufio.NewWriter(h.serverConn)), nil
}
