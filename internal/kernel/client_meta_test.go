package kernel

import (
	"encoding/json"
	"github.com/bamanoz/tabula/internal/kernel/clientmeta"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

func TestDecodeClientMetaNormalizesRuntimeID(t *testing.T) {
	meta := clientmeta.Decode(json.RawMessage(`{"tabula.client_role":"user","tabula.runtime_id":" remote "}`))
	if meta.Role != "user" || meta.RuntimeID != "remote" {
		t.Fatalf("unexpected client meta: %+v", meta)
	}
	if got := clientmeta.Decode(json.RawMessage(`{"tabula.runtime_id":"bad runtime"}`)).RuntimeID; got != "" {
		t.Fatalf("invalid runtime id should be ignored, got %q", got)
	}
}

func TestJoinBindsSessionPreferredRuntimeFromFirstClientMeta(t *testing.T) {
	hub := NewHub(nil, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))

	first := &Client{hub: hub, name: "ui-1", meta: json.RawMessage(`{"tabula.client_role":"user","tabula.runtime_id":"remote"}`), recvCh: make(chan *BusMessage, 4), receives: map[string]bool{}, sends: map[string]bool{}, state: ClientProtocolReady, done: make(chan struct{})}
	second := &Client{hub: hub, name: "ui-2", meta: json.RawMessage(`{"tabula.client_role":"user","tabula.runtime_id":"local"}`), recvCh: make(chan *BusMessage, 4), receives: map[string]bool{}, sends: map[string]bool{}, state: ClientProtocolReady, done: make(chan struct{})}
	if !hub.addClient(first) || !hub.addClient(second) {
		t.Fatal("addClient failed")
	}

	hub.applyJoinPlan(first, hub.buildJoinPlan(first, "s1", "alpha"))
	hub.applyJoinPlan(second, hub.buildJoinPlan(second, "s1", "alpha"))

	sess, ok := hub.sessions.Get("s1", "alpha")
	if !ok {
		t.Fatal("expected joined session")
	}
	if got := sess.PreferredRuntime(); got != "remote" {
		t.Fatalf("preferred runtime = %q, want remote", got)
	}
}

func TestConnectionOpenPreservesRuntimeAffinityMeta(t *testing.T) {
	hub := NewHub(nil, nil)
	hub.SetClientAuthToken("test-kernel-token")
	client := &Client{hub: hub, recvCh: make(chan *BusMessage, 1), state: ClientSocketConnected, done: make(chan struct{})}
	if !hub.addClient(client) {
		t.Fatal("addClient failed")
	}

	_, err := hub.handleClientConnectionOpen(client, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: "connection.open", ID: "open-1",
		Data: mustMarshalRaw(map[string]any{
			"name":           "user",
			"send_topics":    []string{testExtensionTopic},
			"receive_topics": []string{testExtensionTopic},
			"auth_token":     "test-kernel-token",
			"meta": map[string]any{
				"tabula.client_role": "user",
				"tabula.runtime_id":  "remote",
			},
		}),
	})
	if err != nil {
		t.Fatalf("connection.open rejected: %v", err)
	}

	meta := clientmeta.Decode(client.meta)
	if meta.Role != "user" || meta.RuntimeID != "remote" {
		t.Fatalf("unexpected client meta: %+v", meta)
	}
}
