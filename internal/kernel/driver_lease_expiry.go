package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
)

const driverLeaseExpiryInterval = time.Second

type driverLeaseExpiryScheduler struct {
	repository agent.SessionRepository
	leases     *agent.DriverLeaseService
	supervisor *agent.DriverSupervisor
	tenantIDs  func() ([]string, error)
	logger     *slog.Logger
	interval   time.Duration
	now        func() time.Time
	afterTurn  func(agent.Record, string, string)

	mu                 sync.Mutex
	cancel             context.CancelFunc
	started            bool
	done               chan struct{}
	wg                 sync.WaitGroup
	ensuredGenerations map[agent.SessionKey]uint64
}

func newDriverLeaseExpiryScheduler(
	repository agent.SessionRepository,
	leases *agent.DriverLeaseService,
	supervisor *agent.DriverSupervisor,
	tenantIDs func() ([]string, error),
	logger *slog.Logger,
	interval time.Duration,
) *driverLeaseExpiryScheduler {
	return &driverLeaseExpiryScheduler{
		repository:         repository,
		leases:             leases,
		supervisor:         supervisor,
		tenantIDs:          tenantIDs,
		logger:             logger,
		interval:           interval,
		now:                func() time.Time { return time.Now().UTC() },
		done:               make(chan struct{}),
		ensuredGenerations: make(map[agent.SessionKey]uint64),
	}
}

// StartAgentLifecycle starts durable agent background lifecycle work.
func (h *Hub) StartAgentLifecycle(ctx context.Context) {
	h.startAgentLifecycle(ctx, driverLeaseExpiryInterval)
}

func (h *Hub) startAgentLifecycle(ctx context.Context, interval time.Duration) {
	if h == nil || h.driverLeaseExpiry == nil {
		return
	}
	h.driverLeaseExpiry.interval = interval
	h.driverLeaseExpiry.start(ctx)
}

// StopAgentLifecycle stops durable agent background lifecycle work and waits for exit.
func (h *Hub) StopAgentLifecycle() {
	if h == nil || h.driverLeaseExpiry == nil {
		return
	}
	h.driverLeaseExpiry.stop()
}

func (h *Hub) agentTenantIDs() ([]string, error) {
	if h == nil || h.tenants == nil {
		return nil, fmt.Errorf("tenant store is not configured")
	}
	tenants, err := h.tenants.List()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(tenants))
	for _, item := range tenants {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func (s *driverLeaseExpiryScheduler) start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.run(ctx)
}

func (s *driverLeaseExpiryScheduler) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	started := s.started
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		s.wg.Wait()
	}
}

func (s *driverLeaseExpiryScheduler) run(ctx context.Context) {
	defer s.wg.Done()
	defer close(s.done)

	s.sweep(ctx)
	timer := time.NewTimer(s.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.sweep(ctx)
			timer.Reset(s.interval)
		}
	}
}

func (s *driverLeaseExpiryScheduler) sweep(ctx context.Context) {
	if err := s.expireDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Warn("expire durable driver leases", "err", err)
	}
}

func (s *driverLeaseExpiryScheduler) expireDue(ctx context.Context) error {
	if s == nil || s.repository == nil || s.leases == nil || s.supervisor == nil || s.tenantIDs == nil {
		return fmt.Errorf("driver lease expiry scheduler is not configured")
	}
	tenantIDs, err := s.tenantIDs()
	if err != nil {
		return fmt.Errorf("list tenants for driver lease expiry: %w", err)
	}
	var sweepErrors []error
	for _, tenantID := range tenantIDs {
		after := ""
		for {
			page, err := s.repository.List(ctx, agent.SessionQuery{
				TenantID: tenantID, Statuses: []agent.SessionStatus{agent.SessionOpen},
				IncludeArchived: false, AfterSessionID: after, Limit: 100,
			})
			if err != nil {
				sweepErrors = append(sweepErrors, fmt.Errorf("list tenant %s sessions: %w", tenantID, err))
				break
			}
			for _, record := range page.Records {
				reconciled := record
				if record.State.Driver.Status != agent.DriverAbsent {
					s.forgetEnsure(record.Key)
					if s.now().Before(record.State.Driver.Deadline) {
						continue
					}
					activeTurnID := record.State.ActiveTurnID
					reconciled, err = s.leases.Expire(ctx, record.Key)
					if errors.Is(err, agent.ErrInvalidTransition) {
						continue
					}
					if err != nil {
						sweepErrors = append(sweepErrors, fmt.Errorf("expire driver lease %s/%s: %w", record.Key.TenantID, record.Key.SessionID, err))
						continue
					}
					if activeTurnID != "" && reconciled.State.Turns[activeTurnID].Status == agent.TurnRecoveryRequired && s.afterTurn != nil {
						s.afterTurn(reconciled, activeTurnID, reconciled.State.Turns[activeTurnID].RecoveryReason)
					}
				}
				generation := reconciled.State.Driver.Generation + 1
				if reconciled.State.Driver.Status != agent.DriverAbsent || !s.shouldEnsure(record.Key, generation) {
					continue
				}
				if err := s.supervisor.Ensure(ctx, reconciled); err != nil {
					sweepErrors = append(sweepErrors, fmt.Errorf("ensure driver takeover %s/%s: %w", record.Key.TenantID, record.Key.SessionID, err))
					continue
				}
				s.markEnsured(record.Key, generation)
			}
			if page.NextAfterSessionID == "" {
				break
			}
			after = page.NextAfterSessionID
		}
	}
	return errors.Join(sweepErrors...)
}

func (s *driverLeaseExpiryScheduler) shouldEnsure(key agent.SessionKey, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensuredGenerations[key] != generation
}

func (s *driverLeaseExpiryScheduler) markEnsured(key agent.SessionKey, generation uint64) {
	s.mu.Lock()
	s.ensuredGenerations[key] = generation
	s.mu.Unlock()
}

func (s *driverLeaseExpiryScheduler) forgetEnsure(key agent.SessionKey) {
	s.mu.Lock()
	delete(s.ensuredGenerations, key)
	s.mu.Unlock()
}
