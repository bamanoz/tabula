package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
)

// RuntimeSelector chooses the authenticated runtime attachment for one session.
type RuntimeSelector interface {
	RuntimeForSession(ctx context.Context, record Record) (runtimeapi.DriverControlConn, string, error)
}

// DriverSupervisor reconciles durable session projections with runtime workers.
type DriverSupervisor struct {
	repository SessionRepository
	runtimes   RuntimeSelector

	mu         sync.Mutex
	desired    map[SessionKey]runtimeapi.DriverEnsureReq
	operations map[SessionKey]*driverOperation
}

type driverOperation struct {
	mu   sync.Mutex
	refs int
}

func NewDriverSupervisor(repository SessionRepository, runtimes RuntimeSelector) *DriverSupervisor {
	return &DriverSupervisor{
		repository: repository,
		runtimes:   runtimes,
		desired:    make(map[SessionKey]runtimeapi.DriverEnsureReq),
		operations: make(map[SessionKey]*driverOperation),
	}
}

// Ensure asks the selected runtime to converge the pinned driver worker.
func (s *DriverSupervisor) Ensure(ctx context.Context, record Record) error {
	if s == nil || s.runtimes == nil {
		return fmt.Errorf("driver supervisor is not configured")
	}
	unlock := s.lockOperation(record.Key)
	defer unlock()
	if record.State.Status != SessionOpen || record.State.DriverComponentID == "" || record.State.AgentSpecRevision == "" {
		return fmt.Errorf("%w: session has no runnable driver specification", ErrInvalidTransition)
	}
	conn, _, err := s.runtimes.RuntimeForSession(ctx, record)
	if err != nil {
		return err
	}
	req := driverEnsureRequest(record, record.State.Driver.Generation+1)
	if err := conn.PrepareTenant(ctx, runtimeapi.PrepareTenantReq{RequestID: prepareTenantRequestID(record), TenantID: record.Key.TenantID}); err != nil {
		return fmt.Errorf("prepare tenant plugins: %w", err)
	}
	if err := conn.EnsureDriver(ctx, req); err != nil {
		return fmt.Errorf("ensure driver: %w", err)
	}
	s.mu.Lock()
	s.desired[record.Key] = req
	s.mu.Unlock()
	return nil
}

// Stop removes the desired worker for one session and asks its current runtime to stop it.
func (s *DriverSupervisor) Stop(ctx context.Context, key SessionKey) error {
	if s == nil || s.runtimes == nil {
		return nil
	}
	unlock := s.lockOperation(key)
	defer unlock()
	record, err := s.repository.Load(ctx, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, ErrNotFound) {
		record = Record{Key: key}
	}
	conn, _, err := s.runtimes.RuntimeForSession(ctx, record)
	if err != nil {
		return err
	}
	if err := conn.StopDriver(ctx, runtimeapi.DriverStopReq{RequestID: stopRequestID(key), TenantID: key.TenantID, SessionID: key.SessionID}); err != nil {
		return fmt.Errorf("stop driver: %w", err)
	}
	s.mu.Lock()
	delete(s.desired, key)
	s.mu.Unlock()
	return nil
}

// ReconcileRuntime rebuilds desired state from durable projections after runtime reconnect.
func (s *DriverSupervisor) ReconcileRuntime(ctx context.Context, tenantIDs []string, runtimeID string) error {
	if s == nil || s.repository == nil || s.runtimes == nil {
		return fmt.Errorf("driver supervisor is not configured")
	}
	for _, tenantID := range tenantIDs {
		prepared := false
		after := ""
		for {
			page, err := s.repository.List(ctx, SessionQuery{TenantID: tenantID, Statuses: []SessionStatus{SessionOpen}, IncludeArchived: false, AfterSessionID: after, Limit: 100})
			if err != nil {
				return fmt.Errorf("list sessions for tenant %s: %w", tenantID, err)
			}
			for _, record := range page.Records {
				conn, selectedRuntimeID, err := s.runtimes.RuntimeForSession(ctx, record)
				if err != nil {
					return err
				}
				if selectedRuntimeID != runtimeID {
					continue
				}
				desiredGeneration := record.State.Driver.Generation + 1
				if record.State.Driver.Status != DriverAbsent {
					if record.State.Driver.RuntimeID != runtimeID {
						continue
					}
					desiredGeneration = record.State.Driver.Generation
				}
				req := driverEnsureRequest(record, desiredGeneration)
				if !prepared {
					if err := conn.PrepareTenant(ctx, runtimeapi.PrepareTenantReq{RequestID: prepareTenantReconcileRequestID(tenantID, runtimeID), TenantID: tenantID}); err != nil {
						return fmt.Errorf("prepare tenant plugins for %s: %w", tenantID, err)
					}
					prepared = true
				}
				unlock := s.lockOperation(record.Key)
				if err := conn.EnsureDriver(ctx, req); err != nil {
					unlock()
					return fmt.Errorf("reconcile driver %s/%s: %w", record.Key.TenantID, record.Key.SessionID, err)
				}
				s.mu.Lock()
				s.desired[record.Key] = req
				s.mu.Unlock()
				unlock()
			}
			if page.NextAfterSessionID == "" {
				break
			}
			after = page.NextAfterSessionID
		}
	}
	return nil
}

func (s *DriverSupervisor) lockOperation(key SessionKey) func() {
	s.mu.Lock()
	operation := s.operations[key]
	if operation == nil {
		operation = &driverOperation{}
		s.operations[key] = operation
	}
	operation.refs++
	s.mu.Unlock()

	operation.mu.Lock()
	return func() {
		operation.mu.Unlock()
		s.mu.Lock()
		operation.refs--
		if operation.refs == 0 && s.operations[key] == operation {
			delete(s.operations, key)
		}
		s.mu.Unlock()
	}
}

func driverEnsureRequest(record Record, generation uint64) runtimeapi.DriverEnsureReq {
	return runtimeapi.DriverEnsureReq{
		RequestID:         ensureRequestID(record, generation),
		TenantID:          record.Key.TenantID,
		SessionID:         record.Key.SessionID,
		ComponentID:       record.State.DriverComponentID,
		AgentSpecRevision: record.State.AgentSpecRevision,
		DesiredGeneration: generation,
	}
}

func prepareTenantRequestID(record Record) string {
	return fmt.Sprintf("prepare-tenant:%s:%s:%d", record.Key.TenantID, record.Key.SessionID, record.Version)
}

func prepareTenantReconcileRequestID(tenantID, runtimeID string) string {
	return fmt.Sprintf("prepare-tenant-reconcile:%s:%s", tenantID, runtimeID)
}

func ensureRequestID(record Record, generation uint64) string {
	return fmt.Sprintf("driver-ensure:%s:%s:%d:%d", record.Key.TenantID, record.Key.SessionID, record.Version, generation)
}

func stopRequestID(key SessionKey) string {
	return "driver-stop:" + key.TenantID + ":" + key.SessionID
}
