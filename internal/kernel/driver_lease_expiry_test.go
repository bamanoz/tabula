package kernel

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestDriverLeaseExpirySchedulerRestoresDurableCrashStateAfterKernelRestart(t *testing.T) {
	tests := []struct {
		name           string
		permit         bool
		wantTurn       agent.TurnStatus
		wantAttempt    agent.AttemptStatus
		wantActiveTurn bool
	}{
		{name: "crash before permit retries turn", wantTurn: agent.TurnQueued, wantAttempt: agent.AttemptAbandoned},
		{name: "crash after permit requires recovery", permit: true, wantTurn: agent.TurnRecoveryRequired, wantAttempt: agent.AttemptUncertain, wantActiveTurn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := t.TempDir() + "/sessions.db"
			key, turnID, attemptID := persistExpiredDriverAttempt(t, path, tt.permit)

			repository, err := agent.OpenSQLiteRepository(context.Background(), path)
			if err != nil {
				t.Fatalf("OpenSQLiteRepository after restart: %v", err)
			}
			t.Cleanup(func() { _ = repository.Close() })

			runtimeConn := newLeaseExpiryRuntimeConn()
			hub := NewHub(json.RawMessage(`[]`), nil)
			hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: key.TenantID}))
			if err := hub.ConfigureRuntimeRegistry(
				[]runtimeconfig.Definition{{ID: "runtime-a", Backend: "attach"}},
				map[string]runtimeconfig.Binding{key.TenantID: {AllowedRuntimes: []string{"runtime-a"}, DefaultRuntime: "runtime-a"}},
			); err != nil {
				t.Fatalf("ConfigureRuntimeRegistry: %v", err)
			}
			if err := hub.runtimes.RegisterHello("runtime-a", runtimeConn, nil, 0, []string{key.TenantID}); err != nil {
				t.Fatalf("RegisterHello: %v", err)
			}
			hub.SetAgentSessionRepository(repository)
			hub.startAgentLifecycle(context.Background(), time.Hour)

			ensure := runtimeConn.waitEnsure(t)
			if ensure.TenantID != key.TenantID || ensure.SessionID != key.SessionID || ensure.DesiredGeneration != 2 {
				t.Fatalf("ensure = %+v", ensure)
			}
			record, err := repository.Load(context.Background(), key)
			if err != nil {
				t.Fatalf("Load expired session: %v", err)
			}
			turn := record.State.Turns[turnID]
			attempt := findKernelTestAttempt(t, turn, attemptID)
			if record.State.Driver.Status != agent.DriverAbsent || turn.Status != tt.wantTurn || attempt.Status != tt.wantAttempt {
				t.Fatalf("driver=%+v turn=%+v attempt=%+v", record.State.Driver, turn, attempt)
			}
			if got := record.State.ActiveTurnID != ""; got != tt.wantActiveTurn {
				t.Fatalf("active turn present = %v, want %v", got, tt.wantActiveTurn)
			}

			version := record.Version
			if err := hub.driverLeaseExpiry.expireDue(context.Background()); err != nil {
				t.Fatalf("second expiry sweep: %v", err)
			}
			after, err := repository.Load(context.Background(), key)
			if err != nil {
				t.Fatalf("Load after second sweep: %v", err)
			}
			if after.Version != version || runtimeConn.ensureCount() != 1 {
				t.Fatalf("duplicate expiry changed state: version %d -\u003e %d, ensures=%d", version, after.Version, runtimeConn.ensureCount())
			}
			messages, err := repository.ReadOutbox(context.Background(), key, 0, 100)
			if err != nil {
				t.Fatalf("ReadOutbox: %v", err)
			}
			driverStateChanges := 0
			for _, message := range messages {
				if message.Message.Topic == "driver.state_changed" {
					driverStateChanges++
				}
			}
			if driverStateChanges != 1 {
				t.Fatalf("driver.state_changed messages = %d, want 1", driverStateChanges)
			}

			scheduler := hub.driverLeaseExpiry
			hub.Shutdown()
			select {
			case <-scheduler.done:
			case <-time.After(time.Second):
				t.Fatal("lease expiry scheduler did not stop with Hub shutdown")
			}
		})
	}
}

func persistExpiredDriverAttempt(t *testing.T, path string, permit bool) (agent.SessionKey, string, string) {
	t.Helper()
	ctx := context.Background()
	repository, err := agent.OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatalf("OpenSQLiteRepository: %v", err)
	}
	key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
	state := agent.NewState()
	create := agent.CreateSession{
		Meta: agent.CommandMeta{ID: "create", Actor: agent.ActorClient}, TenantID: key.TenantID, SessionID: key.SessionID,
		DriverComponentID: "driver", AgentSpecRevision: "sha256:spec",
	}
	commitKernelTestCommand(t, repository, key, state, 0, create)

	clock := fixedKernelTestClock{now: time.Unix(100, 0).UTC()}
	leases, err := agent.NewDriverLeaseService(repository, agent.LeaseOptions{TTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second, Clock: clock})
	if err != nil {
		t.Fatalf("NewDriverLeaseService: %v", err)
	}
	grant, err := leases.Register(ctx, agent.DriverIdentity{
		RuntimeID: "runtime-a", TenantID: key.TenantID, SessionID: key.SessionID,
		ComponentID: "driver", AgentSpecRevision: "sha256:spec", DriverInstanceID: "driver-1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := leases.Ready(ctx, key, "runtime-a", grant.Fence); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	inputs, err := agent.NewInputProcessor(repository)
	if err != nil {
		t.Fatalf("NewInputProcessor: %v", err)
	}
	if _, err := inputs.Submit(ctx, agent.InputSubmitRequest{Key: key, CommandID: "submit", InputID: "input", Content: json.RawMessage(`{"text":"hello"}`)}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	execution, err := agent.NewTurnExecutionService(repository)
	if err != nil {
		t.Fatalf("NewTurnExecutionService: %v", err)
	}
	assignment, err := execution.AssignNext(ctx, key, nil)
	if err != nil {
		t.Fatalf("AssignNext: %v", err)
	}
	if permit {
		if _, err := execution.Prepare(ctx, key, "runtime-a", assignment.TurnID, assignment.AttemptID, grant.Fence); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if _, err := execution.Permit(ctx, key, assignment.TurnID, assignment.AttemptID, grant.Fence); err != nil {
			t.Fatalf("Permit: %v", err)
		}
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("Close repository before restart: %v", err)
	}
	return key, assignment.TurnID, assignment.AttemptID
}

func commitKernelTestCommand(t *testing.T, repository agent.SessionRepository, key agent.SessionKey, state agent.State, version uint64, command agent.Command) {
	t.Helper()
	events, err := agent.Decide(state, command)
	if err != nil {
		t.Fatalf("Decide %T: %v", command, err)
	}
	projection, err := agent.ApplyAll(state, events)
	if err != nil {
		t.Fatalf("ApplyAll %T: %v", command, err)
	}
	digest, err := agent.DigestCommand(command)
	if err != nil {
		t.Fatalf("DigestCommand %T: %v", command, err)
	}
	if _, err := repository.Commit(context.Background(), agent.Commit{
		Key: key, CommandID: command.Metadata().ID, CommandDigest: digest,
		ExpectedVersion: version, Events: events, Projection: projection,
	}); err != nil {
		t.Fatalf("Commit %T: %v", command, err)
	}
}

func findKernelTestAttempt(t *testing.T, turn agent.Turn, attemptID string) agent.Attempt {
	t.Helper()
	for _, attempt := range turn.Attempts {
		if attempt.ID == attemptID {
			return attempt
		}
	}
	t.Fatalf("attempt %q missing from %+v", attemptID, turn)
	return agent.Attempt{}
}

type fixedKernelTestClock struct{ now time.Time }

func (c fixedKernelTestClock) Now() time.Time { return c.now }

type leaseExpiryRuntimeConn struct {
	*runtimemock.RuntimeConn
	mu      sync.Mutex
	ensures []runtimeapi.DriverEnsureReq
	notify  chan struct{}
}

func newLeaseExpiryRuntimeConn() *leaseExpiryRuntimeConn {
	return &leaseExpiryRuntimeConn{RuntimeConn: runtimemock.New(), notify: make(chan struct{}, 1)}
}

func (c *leaseExpiryRuntimeConn) PrepareTenant(context.Context, runtimeapi.PrepareTenantReq) error {
	return nil
}

func (c *leaseExpiryRuntimeConn) EnsureDriver(_ context.Context, req runtimeapi.DriverEnsureReq) error {
	c.mu.Lock()
	c.ensures = append(c.ensures, req)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
	return nil
}

func (c *leaseExpiryRuntimeConn) StopDriver(context.Context, runtimeapi.DriverStopReq) error {
	return nil
}

func (c *leaseExpiryRuntimeConn) waitEnsure(t *testing.T) runtimeapi.DriverEnsureReq {
	t.Helper()
	select {
	case <-c.notify:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for driver takeover ensure")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensures[len(c.ensures)-1]
}

func (c *leaseExpiryRuntimeConn) ensureCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ensures)
}
