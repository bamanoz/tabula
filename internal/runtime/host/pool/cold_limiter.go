package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

type coldWorkerLimiter struct {
	mu             sync.Mutex
	defaultLimit   int
	acquireTimeout time.Duration
	limits         map[string]int
	tenants        map[string]*coldTenantState
}

type coldTenantState struct {
	sem    chan struct{}
	active int
	queued int
}

func newColdWorkerLimiter(defaultLimit int, acquireTimeout time.Duration, limits map[string]int) *coldWorkerLimiter {
	if defaultLimit <= 0 {
		defaultLimit = defaultColdWorkersPerTenantMax
	}
	if acquireTimeout <= 0 {
		acquireTimeout = defaultColdAcquireTimeout
	}
	return &coldWorkerLimiter{
		defaultLimit:   defaultLimit,
		acquireTimeout: acquireTimeout,
		limits:         cloneTenantLimits(limits),
		tenants:        map[string]*coldTenantState{},
	}
}

func (l *coldWorkerLimiter) acquire(ctx context.Context, tenantID string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return func() {}, nil
	}
	state := l.stateFor(tenantID)
	select {
	case state.sem <- struct{}{}:
		l.adjust(tenantID, 1, 0)
		return func() { l.release(tenantID, state) }, nil
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.adjust(tenantID, 0, 1)
	waitCtx, cancel := context.WithTimeout(ctx, l.acquireTimeout)
	defer cancel()
	acquired := false
	defer func() {
		if !acquired {
			l.adjust(tenantID, 0, -1)
		}
	}()
	select {
	case state.sem <- struct{}{}:
		acquired = true
		l.adjust(tenantID, 1, -1)
		return func() { l.release(tenantID, state) }, nil
	case <-waitCtx.Done():
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return nil, errColdWorkerBusy
		}
		return nil, waitCtx.Err()
	}
}

func (l *coldWorkerLimiter) limit(tenantID string) int {
	if l == nil {
		return defaultColdWorkersPerTenantMax
	}
	if override, ok := l.limits[tenantID]; ok && override > 0 {
		return override
	}
	return l.defaultLimit
}

func (l *coldWorkerLimiter) countsFor(tenantID string) (active, queued int) {
	if l == nil {
		return 0, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.tenants[tenantID]
	if state == nil {
		return 0, 0
	}
	return state.active, state.queued
}

func (l *coldWorkerLimiter) counts() (active, queued int) {
	if l == nil {
		return 0, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, state := range l.tenants {
		active += state.active
		queued += state.queued
	}
	return active, queued
}

func (l *coldWorkerLimiter) stateFor(tenantID string) *coldTenantState {
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing := l.tenants[tenantID]; existing != nil {
		return existing
	}
	state := &coldTenantState{sem: make(chan struct{}, l.limit(tenantID))}
	l.tenants[tenantID] = state
	return state
}

func (l *coldWorkerLimiter) adjust(tenantID string, activeDelta, queuedDelta int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.tenants[tenantID]
	if state == nil {
		state = &coldTenantState{sem: make(chan struct{}, l.limit(tenantID))}
		l.tenants[tenantID] = state
	}
	state.active += activeDelta
	if state.active < 0 {
		state.active = 0
	}
	state.queued += queuedDelta
	if state.queued < 0 {
		state.queued = 0
	}
	if state.active == 0 && state.queued == 0 && len(state.sem) == 0 {
		delete(l.tenants, tenantID)
	}
}

func (l *coldWorkerLimiter) release(tenantID string, state *coldTenantState) {
	if l == nil || state == nil {
		return
	}
	select {
	case <-state.sem:
		l.adjust(tenantID, -1, 0)
	default:
	}
}
