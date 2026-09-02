package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDriverLeaseServiceRegistrationHeartbeatDisconnectReconnect(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	clock := &fakeClock{now: time.Unix(100, 0)}
	service := newLeaseService(t, repository, clock)
	identity := leaseIdentity("driver-1")

	grant, err := service.Register(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Fence.Generation != 1 || grant.Fence.DriverInstanceID != "driver-1" || grant.ExpiresAt != time.Unix(130, 0) {
		t.Fatalf("grant = %+v", grant)
	}
	if _, err := service.Ready(context.Background(), sessionKey(), "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Disconnect(context.Background(), sessionKey(), "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	clock.now = time.Unix(110, 0)
	renewed, err := service.Heartbeat(context.Background(), sessionKey(), "runtime-a", grant.Fence, 1)
	if err != nil {
		t.Fatal(err)
	}
	record, err := repository.Load(context.Background(), sessionKey())
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Driver.Status != DriverReady || record.State.Driver.HeartbeatSequence != 1 || renewed.ExpiresAt != time.Unix(140, 0) {
		t.Fatalf("driver = %+v renewed=%+v", record.State.Driver, renewed)
	}
}

func TestDriverLeaseServiceDisconnectRuntimeMarksOwnedLeasesSuspect(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	service := newLeaseService(t, repository, &fakeClock{now: time.Unix(100, 0)})
	grant, err := service.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ready(context.Background(), sessionKey(), "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}

	if err := service.DisconnectRuntime(context.Background(), []string{"tenant"}, "runtime-a"); err != nil {
		t.Fatal(err)
	}
	record, err := repository.Load(context.Background(), sessionKey())
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Driver.Status != DriverSuspect || record.State.Driver.Fence != grant.Fence {
		t.Fatalf("driver after runtime disconnect = %+v", record.State.Driver)
	}
	if err := service.DisconnectRuntime(context.Background(), []string{"tenant"}, "runtime-a"); err != nil {
		t.Fatalf("duplicate runtime disconnect: %v", err)
	}
}

func TestDriverLeaseServiceRejectsWrongIdentityAndStaleHeartbeat(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	clock := &fakeClock{now: time.Unix(100, 0)}
	service := newLeaseService(t, repository, clock)
	grant, err := service.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Heartbeat(context.Background(), sessionKey(), "runtime-b", grant.Fence, 1); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("wrong runtime error = %v", err)
	}
	clock.now = time.Unix(101, 0)
	if _, err := service.Heartbeat(context.Background(), sessionKey(), "runtime-a", grant.Fence, 1); err != nil {
		t.Fatal(err)
	}
	clock.now = time.Unix(102, 0)
	if _, err := service.Heartbeat(context.Background(), sessionKey(), "runtime-a", grant.Fence, 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("stale sequence error = %v", err)
	}
}

func TestDriverLeaseServiceReleaseFencesLeaseAndWritesOutbox(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	service := newLeaseService(t, repository, &fakeClock{now: time.Unix(100, 0)})
	grant, err := service.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := service.Release(context.Background(), sessionKey(), "runtime-a", grant.Fence)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Driver.Status != DriverAbsent || record.State.Driver.Generation != 1 {
		t.Fatalf("driver = %+v", record.State.Driver)
	}
	messages, err := repository.ReadOutbox(context.Background(), sessionKey(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Message.Topic != "driver.state_changed" {
		t.Fatalf("outbox = %+v", messages)
	}
}

func TestDriverLeaseServiceExpiryFencesAndTakeoverIncrementsGeneration(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	clock := &fakeClock{now: time.Unix(100, 0)}
	service := newLeaseService(t, repository, clock)
	first, err := service.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	clock.now = time.Unix(130, 0)
	if _, err := service.Expire(context.Background(), sessionKey()); err != nil {
		t.Fatal(err)
	}
	second, err := service.Register(context.Background(), leaseIdentity("driver-2"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Fence.Generation != first.Fence.Generation+1 {
		t.Fatalf("generations first=%d second=%d", first.Fence.Generation, second.Fence.Generation)
	}
	if _, err := service.Ready(context.Background(), sessionKey(), "runtime-a", first.Fence); !errors.Is(err, ErrStaleDriver) {
		t.Fatalf("stale ready error = %v", err)
	}
}

func TestDriverLeaseServicePersistsAcrossSQLiteReopen(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	commitSession(t, repository, "session")
	clock := &fakeClock{now: time.Unix(100, 0)}
	service := newLeaseService(t, repository, clock)
	grant, err := service.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ready(context.Background(), sessionKey(), "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	clock.now = time.Unix(110, 0)
	if _, err := service.Heartbeat(context.Background(), sessionKey(), "runtime-a", grant.Fence, 1); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	record, err := reopened.Load(context.Background(), sessionKey())
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Driver.RuntimeID != "runtime-a" || record.State.Driver.ComponentID != "driver" || record.State.Driver.AgentSpecRevision != "sha256:spec" || record.State.Driver.HeartbeatSequence != 1 {
		t.Fatalf("driver after reopen = %+v", record.State.Driver)
	}
}

func TestConcurrentDriverRegistrationGrantsOnlyOneLease(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	service := newLeaseService(t, repository, &fakeClock{now: time.Unix(100, 0)})

	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			identity := leaseIdentity("driver-" + string(rune('a'+i)))
			_, err := service.Register(context.Background(), identity)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
			continue
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("registration error = %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("successful registrations = %d, want 1", success)
	}
}

func newLeaseService(t *testing.T, repository SessionRepository, clock Clock) *DriverLeaseService {
	t.Helper()
	service, err := NewDriverLeaseService(repository, LeaseOptions{TTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func leaseIdentity(instanceID string) DriverIdentity {
	return DriverIdentity{
		RuntimeID:         "runtime-a",
		TenantID:          "tenant",
		SessionID:         "session",
		ComponentID:       "driver",
		AgentSpecRevision: "sha256:spec",
		DriverInstanceID:  instanceID,
	}
}

func sessionKey() SessionKey { return SessionKey{TenantID: "tenant", SessionID: "session"} }

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
