package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
)

func TestDriverSupervisorEnsureUsesPinnedSpecAndNextGeneration(t *testing.T) {
	conn := &recordingDriverConn{}
	selector := staticRuntimeSelector{conn: conn, runtimeID: "local"}
	supervisor := NewDriverSupervisor(NewMemoryRepository(), selector)
	record := driverRecord("session", 4, 7)

	if err := supervisor.Ensure(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	request := conn.lastEnsure()
	if request.ComponentID != "driver" || request.AgentSpecRevision != "sha256:spec" || request.DesiredGeneration != 8 {
		t.Fatalf("ensure = %+v", request)
	}
	conn.mu.Lock()
	calls := append([]string(nil), conn.calls...)
	prepare := conn.prepares[0]
	conn.mu.Unlock()
	if len(calls) != 2 || calls[0] != "prepare" || calls[1] != "ensure" {
		t.Fatalf("calls = %v, want prepare then ensure", calls)
	}
	if prepare.TenantID != "tenant" || prepare.RequestID == "" {
		t.Fatalf("prepare = %+v", prepare)
	}
}

func TestDriverSupervisorEnsureFailsClosedWhenTenantPreparationFails(t *testing.T) {
	conn := &recordingDriverConn{prepareErr: context.DeadlineExceeded}
	supervisor := NewDriverSupervisor(NewMemoryRepository(), staticRuntimeSelector{conn: conn, runtimeID: "local"})

	if err := supervisor.Ensure(context.Background(), driverRecord("session", 4, 7)); err == nil {
		t.Fatal("Ensure succeeded despite tenant preparation failure")
	}
	if got := conn.ensureCount(); got != 0 {
		t.Fatalf("ensure count = %d, want 0", got)
	}
}

func TestDriverSupervisorReconcileRuntimeRestoresDurableSessions(t *testing.T) {
	repository := NewMemoryRepository()
	for _, sessionID := range []string{"a", "b"} {
		commitSession(t, repository, sessionID)
	}
	conn := &recordingDriverConn{}
	supervisor := NewDriverSupervisor(repository, staticRuntimeSelector{conn: conn, runtimeID: "local"})

	if err := supervisor.ReconcileRuntime(context.Background(), []string{"tenant"}, "local"); err != nil {
		t.Fatal(err)
	}
	if got := conn.ensureCount(); got != 2 {
		t.Fatalf("ensure count = %d, want 2", got)
	}
	conn.mu.Lock()
	prepareCount := len(conn.prepares)
	conn.mu.Unlock()
	if prepareCount != 1 {
		t.Fatalf("prepare count = %d, want 1 per tenant reconciliation", prepareCount)
	}
}

func TestDriverSupervisorSerializesConcurrentEnsureForOneSession(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	record, err := repository.Load(context.Background(), sessionKey())
	if err != nil {
		t.Fatal(err)
	}
	conn := &blockingDriverConn{
		recordingDriverConn: recordingDriverConn{},
		firstEntered:        make(chan struct{}),
		secondEntered:       make(chan struct{}),
		releaseFirst:        make(chan struct{}),
	}
	supervisor := NewDriverSupervisor(repository, staticRuntimeSelector{conn: conn, runtimeID: "local"})
	reconcileDone := make(chan error, 1)
	go func() {
		reconcileDone <- supervisor.ReconcileRuntime(context.Background(), []string{"tenant"}, "local")
	}()
	<-conn.firstEntered
	ensureDone := make(chan error, 1)
	go func() {
		ensureDone <- supervisor.Ensure(context.Background(), record)
	}()
	select {
	case <-conn.secondEntered:
		t.Fatal("concurrent ensure reached runtime before prior ensure completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(conn.releaseFirst)
	if err := <-reconcileDone; err != nil {
		t.Fatal(err)
	}
	if err := <-ensureDone; err != nil {
		t.Fatal(err)
	}
	if got := conn.ensureCount(); got != 2 {
		t.Fatalf("ensure count = %d, want 2 serialized requests", got)
	}
}

func TestDriverSupervisorReconcileRuntimePreservesActiveGenerationDuringGrace(t *testing.T) {
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	leases := newLeaseService(t, repository, &fakeClock{now: time.Unix(100, 0)})
	grant, err := leases.Register(context.Background(), DriverIdentity{
		RuntimeID: "local", TenantID: "tenant", SessionID: "session", ComponentID: "driver",
		AgentSpecRevision: "sha256:spec", DriverInstanceID: "driver-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Ready(context.Background(), sessionKey(), "local", grant.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Disconnect(context.Background(), sessionKey(), "local", grant.Fence); err != nil {
		t.Fatal(err)
	}

	conn := &recordingDriverConn{}
	supervisor := NewDriverSupervisor(repository, staticRuntimeSelector{conn: conn, runtimeID: "local"})
	if err := supervisor.ReconcileRuntime(context.Background(), []string{"tenant"}, "local"); err != nil {
		t.Fatal(err)
	}
	if request := conn.lastEnsure(); request.DesiredGeneration != grant.Fence.Generation {
		t.Fatalf("desired generation = %d, want active generation %d", request.DesiredGeneration, grant.Fence.Generation)
	}
}

func commitSession(t *testing.T, repository SessionRepository, sessionID string) {
	t.Helper()
	base := NewState()
	command := CreateSession{
		Meta:              CommandMeta{ID: "create-" + sessionID, Actor: ActorClient},
		TenantID:          "tenant",
		SessionID:         sessionID,
		DriverComponentID: "driver",
		AgentSpecRevision: "sha256:spec",
	}
	events, err := Decide(base, command)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := ApplyAll(base, events)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Commit(context.Background(), Commit{
		Key:             SessionKey{TenantID: "tenant", SessionID: sessionID},
		CommandID:       command.Meta.ID,
		CommandDigest:   digest,
		ExpectedVersion: 0,
		Events:          events,
		Projection:      projection,
	}); err != nil {
		t.Fatal(err)
	}
}

func driverRecord(sessionID string, version, generation uint64) Record {
	state := NewState()
	state.TenantID = "tenant"
	state.SessionID = sessionID
	state.Status = SessionOpen
	state.DriverComponentID = "driver"
	state.AgentSpecRevision = "sha256:spec"
	state.Driver.Generation = generation
	return Record{Key: SessionKey{TenantID: "tenant", SessionID: sessionID}, Version: version, State: state}
}

type staticRuntimeSelector struct {
	conn      runtimeapi.DriverControlConn
	runtimeID string
}

func (s staticRuntimeSelector) RuntimeForSession(context.Context, Record) (runtimeapi.DriverControlConn, string, error) {
	return s.conn, s.runtimeID, nil
}

type recordingDriverConn struct {
	mu         sync.Mutex
	calls      []string
	prepares   []runtimeapi.PrepareTenantReq
	prepareErr error
	ensures    []runtimeapi.DriverEnsureReq
	stops      []runtimeapi.DriverStopReq
}

func (c *recordingDriverConn) PrepareTenant(_ context.Context, req runtimeapi.PrepareTenantReq) error {
	c.mu.Lock()
	c.calls = append(c.calls, "prepare")
	c.prepares = append(c.prepares, req)
	err := c.prepareErr
	c.mu.Unlock()
	return err
}

func (c *recordingDriverConn) EnsureDriver(_ context.Context, req runtimeapi.DriverEnsureReq) error {
	c.mu.Lock()
	c.calls = append(c.calls, "ensure")
	c.ensures = append(c.ensures, req)
	c.mu.Unlock()
	return nil
}

func (c *recordingDriverConn) StopDriver(_ context.Context, req runtimeapi.DriverStopReq) error {
	c.mu.Lock()
	c.stops = append(c.stops, req)
	c.mu.Unlock()
	return nil
}

func (c *recordingDriverConn) lastEnsure() runtimeapi.DriverEnsureReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensures[len(c.ensures)-1]
}

func (c *recordingDriverConn) ensureCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ensures)
}

type blockingDriverConn struct {
	recordingDriverConn
	firstOnce     sync.Once
	secondOnce    sync.Once
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
}

func (c *blockingDriverConn) EnsureDriver(_ context.Context, req runtimeapi.DriverEnsureReq) error {
	c.mu.Lock()
	c.calls = append(c.calls, "ensure")
	c.ensures = append(c.ensures, req)
	count := len(c.ensures)
	c.mu.Unlock()
	if count == 1 {
		c.firstOnce.Do(func() { close(c.firstEntered) })
		<-c.releaseFirst
	} else if count == 2 {
		c.secondOnce.Do(func() { close(c.secondEntered) })
	}
	return nil
}
