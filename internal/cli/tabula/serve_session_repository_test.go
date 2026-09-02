package tabula

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
)

func TestKernelSessionDatabasePathUsesKernelStateDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "tabula-home")
	want := filepath.Join(home, "state", "kernel", "sessions.db")
	if got := kernelSessionDatabasePath(home); got != want {
		t.Fatalf("kernelSessionDatabasePath() = %q, want %q", got, want)
	}
}

func TestOpenSessionRecordStoreCreatesDatabaseDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "tabula-home")
	store, err := openSessionRecordStore(context.Background(), home)
	if err != nil {
		t.Fatalf("openSessionRecordStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := os.Stat(kernelSessionDatabasePath(home)); err != nil {
		t.Fatalf("stat session database: %v", err)
	}
}

func TestConfigureAgentSessionRepositoryCreatesPathConfiguresHubAndPersistsAcrossReopen(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	hub := &recordingAgentSessionRepositorySetter{}
	repository, err := configureAgentSessionRepository(ctx, home, hub)
	if err != nil {
		t.Fatalf("configureAgentSessionRepository: %v", err)
	}
	if hub.repository != repository {
		t.Fatalf("configured repository = %T %p, want %T %p", hub.repository, hub.repository, repository, repository)
	}
	commitTestAgentSession(t, repository)
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}

	path := kernelSessionDatabasePath(home)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat repository at %s: %v", path, err)
	}
	reopened, err := openAgentSessionRepository(ctx, home)
	if err != nil {
		t.Fatalf("reopen agent session repository: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	record, err := reopened.Load(ctx, agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	if record.State.DriverComponentID != "driver" || record.State.AgentSpecRevision != "sha256:spec" {
		t.Fatalf("persisted state = %+v", record.State)
	}
}

func TestOpenAgentSessionRepositoryReportsCorruptMigrationActionably(t *testing.T) {
	home := t.TempDir()
	path := kernelSessionDatabasePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir repository directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt repository: %v", err)
	}

	_, err := openAgentSessionRepository(context.Background(), home)
	if err == nil {
		t.Fatal("openAgentSessionRepository unexpectedly accepted corrupt database")
	}
	if got := err.Error(); !containsAll(got, "open agent session repository", path) {
		t.Fatalf("error %q does not identify repository path %q", got, path)
	}
}

type recordingAgentSessionRepositorySetter struct {
	repository agent.SessionRepository
}

func (s *recordingAgentSessionRepositorySetter) SetAgentSessionRepository(repository agent.SessionRepository) {
	s.repository = repository
}

func commitTestAgentSession(t *testing.T, repository agent.SessionRepository) {
	t.Helper()
	state := agent.NewState()
	command := agent.CreateSession{
		Meta: agent.CommandMeta{ID: "create", Actor: agent.ActorClient}, TenantID: "tenant", SessionID: "session",
		DriverComponentID: "driver", AgentSpecRevision: "sha256:spec",
	}
	events, err := agent.Decide(state, command)
	if err != nil {
		t.Fatalf("decide create session: %v", err)
	}
	projection, err := agent.ApplyAll(state, events)
	if err != nil {
		t.Fatalf("apply create session: %v", err)
	}
	digest, err := agent.DigestCommand(command)
	if err != nil {
		t.Fatalf("digest create session: %v", err)
	}
	_, err = repository.Commit(context.Background(), agent.Commit{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: command.Meta.ID,
		CommandDigest: digest, ExpectedVersion: state.Version, Events: events, Projection: projection,
	})
	if err != nil {
		t.Fatalf("commit create session: %v", err)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
