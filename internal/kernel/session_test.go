package kernel

import (
	"testing"
)

func TestSessionLifecycle(t *testing.T) {
	s := newSession("test-1")

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

func TestSessionTurnLifecycle(t *testing.T) {
	s := newSession("turn-1")
	s.AddClient("gateway")

	if !s.BeginTurn() {
		t.Fatal("expected first turn to start")
	}
	if !s.IsBusy() {
		t.Fatal("session should be busy while turn is in flight")
	}
	if s.BeginTurn() {
		t.Fatal("second turn should be rejected while busy")
	}
	if !s.RequestCancel() {
		t.Fatal("cancel should be accepted for inflight turn")
	}
	if !s.CancelRequested() {
		t.Fatal("session should record cancel request")
	}
	if s.RequestCancel() {
		t.Fatal("duplicate cancel should be rejected")
	}

	s.EndTurn()
	if s.IsBusy() {
		t.Fatal("session should become idle after turn ends")
	}
	if s.CancelRequested() {
		t.Fatal("cancel state should reset after turn ends")
	}
}

func TestSessionRegistryGetOrCreate(t *testing.T) {
	r := NewSessionRegistry()

	s1 := r.GetOrCreate("s1")
	if s1.ID != "s1" || s1.State != SessionIdle {
		t.Fatalf("expected new idle session s1, got %s/%s", s1.ID, s1.State)
	}

	s2 := r.GetOrCreate("s1")
	if s1 != s2 {
		t.Fatal("GetOrCreate should return same instance for existing session")
	}

	s3 := r.GetOrCreate("s2")
	if s3.ID != "s2" {
		t.Fatalf("expected new session s2, got %s", s3.ID)
	}
}

func TestSessionRegistryGet(t *testing.T) {
	r := NewSessionRegistry()
	r.GetOrCreate("s1")

	s, ok := r.Get("s1")
	if !ok || s.ID != "s1" {
		t.Fatal("should find existing session")
	}

	_, ok = r.Get("missing")
	if ok {
		t.Fatal("should not find missing session")
	}
}

func TestSessionRegistryRemove(t *testing.T) {
	r := NewSessionRegistry()
	s := r.GetOrCreate("s1")
	s.AddClient("alice")

	r.Remove("s1")

	_, ok := r.Get("s1")
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
	r.GetOrCreate("s1")
	r.GetOrCreate("s2")

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(all))
	}

	r.Remove("s1")
	all = r.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session after removal, got %d", len(all))
	}
}

func TestSessionEndEmittedOnLastClientLeave(t *testing.T) {
	env := newTestEnv(t)

	env.connectAndJoin("alice", "s1", []string{"message"}, []string{})
	env.connectAndJoin("bob", "s1", []string{"message"}, []string{})

	// Session should exist with 2 clients
	sess, ok := env.Hub.sessions.Get("s1")
	if !ok {
		t.Fatal("session s1 should exist")
	}
	if sess.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", sess.ClientCount())
	}

	// Disconnect alice — session should still exist
	env.disconnectClient("alice")
	sess, ok = env.Hub.sessions.Get("s1")
	if !ok {
		t.Fatal("session s1 should still exist after first client leaves")
	}
	if sess.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", sess.ClientCount())
	}

	// Disconnect bob — session should be removed and session_end emitted
	env.disconnectClient("bob")
	_, ok = env.Hub.sessions.Get("s1")
	if ok {
		t.Fatal("session s1 should be removed after last client leaves")
	}
}
