package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestServeAuthenticatedRuntimeRegistersAndDetachesRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer client.CloseNow()

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
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []string{"read", "write"}}},
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
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer client.CloseNow()

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
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	store := runtimeauth.NewMemoryStore()
	if err := store.Set(runtimeauth.TokenRecord{RuntimeID: runtimeauth.LocalRuntimeID, Token: "rtk_good"}); err != nil {
		t.Fatalf("store token: %v", err)
	}
	clientWS, serverWS := runtimeConnWebsocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer client.CloseNow()

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
		Capabilities:    []wire.Capability{{Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []string{"read", "write"}}},
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
