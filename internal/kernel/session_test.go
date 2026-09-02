package kernel

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionLifecycle(t *testing.T) {
	s := newSession("test-1", "")

	if s.State != SessionIdle {
		t.Fatalf("new session should be idle, got %s", s.State)
	}
	if s.LastActiveAt.IsZero() {
		t.Fatal("new session should record last activity")
	}

	s.AddClient("alice")
	if s.State != SessionActive {
		t.Fatalf("session with client should be active, got %s", s.State)
	}
	if s.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", s.ClientCount())
	}

	s.AddClient("bob")
	if s.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", s.ClientCount())
	}

	s.RemoveClient("alice")
	if s.State != SessionActive {
		t.Fatalf("session with remaining client should stay active, got %s", s.State)
	}

	s.RemoveClient("bob")
	if s.State != SessionIdle {
		t.Fatalf("session with no clients should be idle, got %s", s.State)
	}
}

func TestSetSessionStoreHydratesPersistedSessions(t *testing.T) {
	home := t.TempDir()
	store := NewDiskSessionStore(home)
	previous := newSession("codegraph", "alpha")
	previous.SetPreferredRuntime("local")
	previous.AddClient("driver")
	if err := store.Save(previous); err != nil {
		t.Fatalf("save previous session: %v", err)
	}

	hub := NewHub(nil, slog.Default())
	hub.SetSessionStore(NewDiskSessionStore(home))

	sess, ok := hub.sessions.Get("codegraph", "alpha")
	if !ok {
		t.Fatal("expected persisted session to hydrate")
	}
	if got := sess.PreferredRuntime(); got != "local" {
		t.Fatalf("preferred runtime = %q, want local", got)
	}
	if got := sess.ClientCount(); got != 0 {
		t.Fatalf("hydrated session must not restore stale clients, got %d", got)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(hub.SnapshotSessions(), &snapshot); err != nil {
		t.Fatalf("snapshot sessions: %v", err)
	}
	if _, ok := snapshot["alpha/codegraph"]; !ok {
		t.Fatalf("snapshot missing hydrated session: %s", string(hub.SnapshotSessions()))
	}
}

func TestSessionArchiveLifecyclePersistsAndBroadcasts(t *testing.T) {
	hub := NewHub(nil, slog.Default())
	home := t.TempDir()
	hub.SetSessionStore(NewDiskSessionStore(home))
	client := addTenantCaptureClient(t, hub, "tenant", "gateway", "main", []string{TopicSessionArchived}, nil)
	client.sends[TopicSessionArchive] = true

	hub.handleSessionMessage(client, &BusMessage{Type: string(MsgEvent), Topic: TopicSessionArchive, Session: "main", TenantID: "tenant"})

	msg := waitForMessage(t, client.recvCh)
	if msg.Topic != TopicSessionArchived {
		t.Fatalf("topic = %q, want %q", msg.Topic, TopicSessionArchived)
	}
	record := readSessionStateFile(t, home, "tenant", "main")
	if record["archived_at"] == "" {
		t.Fatalf("expected archived_at in state: %+v", record)
	}
}

func TestSessionDeleteLifecycleTombstonesAndHidesSession(t *testing.T) {
	hub := NewHub(nil, slog.Default())
	home := t.TempDir()
	hub.SetSessionStore(NewDiskSessionStore(home))
	client := addTenantCaptureClient(t, hub, "tenant", "gateway", "main", []string{TopicSessionDeleted}, nil)
	client.sends[TopicSessionDelete] = true
	client.sends[testExtensionTopic] = true

	hub.handleSessionMessage(client, &BusMessage{Type: string(MsgEvent), Topic: TopicSessionDelete, Session: "main", TenantID: "tenant"})

	msg := waitForMessage(t, client.recvCh)
	if msg.Topic != TopicSessionDeleted {
		t.Fatalf("topic = %q, want %q", msg.Topic, TopicSessionDeleted)
	}
	record := readSessionStateFile(t, home, "tenant", "main")
	if record["deleted_at"] == "" {
		t.Fatalf("expected deleted_at in state: %+v", record)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(hub.SnapshotSessions(), &snapshot); err != nil {
		t.Fatalf("snapshot sessions: %v", err)
	}
	if _, ok := snapshot["tenant/main"]; ok {
		t.Fatalf("deleted session should be hidden from snapshot: %s", string(hub.SnapshotSessions()))
	}
}

func readSessionStateFile(t *testing.T, home, tenantID, session string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "tenants", tenantID, "state", "sessions", session+".json"))
	if err != nil {
		t.Fatalf("read session state: %v", err)
	}
	record := map[string]any{}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode session state: %v", err)
	}
	return record
}

func TestSessionPreferredRuntimeBindsFirstValueOnly(t *testing.T) {
	s := newSession("runtime-affinity", "default")
	if !s.BindPreferredRuntime("remote") {
		t.Fatal("expected first preferred runtime bind to succeed")
	}
	if got := s.PreferredRuntime(); got != "remote" {
		t.Fatalf("preferred runtime = %q, want remote", got)
	}
	if s.BindPreferredRuntime("local") {
		t.Fatal("expected second preferred runtime bind to be ignored")
	}
	if got := s.PreferredRuntime(); got != "remote" {
		t.Fatalf("preferred runtime changed to %q, want remote", got)
	}
}

func TestSessionPreferredRuntimeCanBeUpdated(t *testing.T) {
	s := newSession("runtime-affinity", "default")
	s.BindPreferredRuntime("remote")
	if !s.SetPreferredRuntime("local") {
		t.Fatal("expected preferred runtime update to succeed")
	}
	if got := s.PreferredRuntime(); got != "local" {
		t.Fatalf("preferred runtime = %q, want local", got)
	}
	if s.SetPreferredRuntime("local") {
		t.Fatal("expected same preferred runtime update to be ignored")
	}
}

func TestSessionRegistryGetOrCreate(t *testing.T) {
	r := NewSessionRegistry()

	s1 := r.GetOrCreate("s1", "default")
	if s1.ID != "s1" || s1.State != SessionIdle {
		t.Fatalf("expected new idle session s1, got %s/%s", s1.ID, s1.State)
	}

	s2 := r.GetOrCreate("s1", "default")
	if s1 != s2 {
		t.Fatal("GetOrCreate should return same instance for existing session")
	}

	s3 := r.GetOrCreate("s2", "default")
	if s3.ID != "s2" {
		t.Fatalf("expected new session s2, got %s", s3.ID)
	}
}

func TestSessionRegistryGet(t *testing.T) {
	r := NewSessionRegistry()
	r.GetOrCreate("s1", "default")

	s, ok := r.Get("s1", "default")
	if !ok || s.ID != "s1" {
		t.Fatal("should find existing session")
	}

	_, ok = r.Get("missing", "default")
	if ok {
		t.Fatal("should not find missing session")
	}
}

func TestSessionRegistryRemove(t *testing.T) {
	r := NewSessionRegistry()
	s := r.GetOrCreate("s1", "default")
	s.AddClient("alice")

	r.Remove("s1", "default")

	_, ok := r.Get("s1", "default")
	if ok {
		t.Fatal("session should be removed from registry")
	}
	// State should have been set to closing before removal
	if s.State != SessionClosing {
		t.Fatalf("removed session should be in closing state, got %s", s.State)
	}
}

func TestSessionRegistryAll(t *testing.T) {
	r := NewSessionRegistry()
	r.GetOrCreate("s1", "default")
	r.GetOrCreate("s2", "default")

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(all))
	}

	r.Remove("s1", "default")
	all = r.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session after removal, got %d", len(all))
	}
}

func TestSessionEndEmittedOnLastClientLeave(t *testing.T) {
	env := newTestEnv(t)

	env.connectAndJoin("alice", "s1", []string{testExtensionTopic}, []string{})
	env.connectAndJoin("bob", "s1", []string{testExtensionTopic}, []string{})

	// Session should exist with 2 clients
	sess, ok := env.Hub.sessions.Get("s1", "default")
	if !ok {
		t.Fatal("session s1 should exist")
	}
	if sess.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", sess.ClientCount())
	}

	// Disconnect alice — session should still exist
	env.disconnectClient("alice")
	sess, ok = env.Hub.sessions.Get("s1", "default")
	if !ok {
		t.Fatal("session s1 should still exist after first client leaves")
	}
	if sess.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", sess.ClientCount())
	}

	// Disconnect bob — session should be removed and session_end emitted
	env.disconnectClient("bob")
	_, ok = env.Hub.sessions.Get("s1", "default")
	if ok {
		t.Fatal("session s1 should be removed after last client leaves")
	}
}

func TestClientRejoinLeavesPreviousSession(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("alice", "s1", []string{testExtensionTopic}, []string{})

	writeJSON(t, conn, BusMessage{Type: "join", Session: "s2"})
	msg := readMsg(t, conn)
	if msg.Type != "joined" || msg.Session != "s2" {
		t.Fatalf("expected joined s2, got %+v", msg)
	}

	if s1, ok := env.Hub.sessions.Get("s1", "default"); ok && s1.ClientCount() != 0 {
		t.Fatalf("expected rejoin to remove old session client, got %d", s1.ClientCount())
	}
	s2, ok := env.Hub.sessions.Get("s2", "default")
	if !ok {
		t.Fatal("session s2 should exist")
	}
	if s2.ClientCount() != 1 {
		t.Fatalf("expected one client in s2, got %d", s2.ClientCount())
	}
}
