package pool

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestColdWorkerLimiterAcquireReleaseCleansTenantState(t *testing.T) {
	limiter := newColdWorkerLimiter(1, time.Second, nil)

	release, err := limiter.acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, queued := limiter.countsFor("tenant-a")
	if active != 1 || queued != 0 {
		t.Fatalf("counts while acquired = active %d queued %d, want 1/0", active, queued)
	}

	release()
	active, queued = limiter.countsFor("tenant-a")
	if active != 0 || queued != 0 {
		t.Fatalf("counts after release = active %d queued %d, want 0/0", active, queued)
	}
	if len(limiter.tenants) != 0 {
		t.Fatalf("tenant state should be removed after final release: %#v", limiter.tenants)
	}
}

func TestColdWorkerLimiterQueuesUntilSlotReleased(t *testing.T) {
	limiter := newColdWorkerLimiter(1, time.Second, nil)
	firstRelease, err := limiter.acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	second := make(chan acquireResult, 1)
	go func() {
		release, err := limiter.acquire(context.Background(), "tenant-a")
		second <- acquireResult{release: release, err: err}
	}()
	waitForColdCounts(t, limiter, "tenant-a", 1, 1)

	firstRelease()
	result := waitForAcquire(t, second)
	if result.err != nil {
		t.Fatalf("second acquire: %v", result.err)
	}
	waitForColdCounts(t, limiter, "tenant-a", 1, 0)

	result.release()
	waitForColdCounts(t, limiter, "tenant-a", 0, 0)
}

func TestColdWorkerLimiterTimeoutRemovesQueuedCount(t *testing.T) {
	limiter := newColdWorkerLimiter(1, 20*time.Millisecond, nil)
	release, err := limiter.acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	_, err = limiter.acquire(context.Background(), "tenant-a")
	if !errors.Is(err, errColdWorkerBusy) {
		t.Fatalf("second acquire err = %v, want %v", err, errColdWorkerBusy)
	}
	active, queued := limiter.countsFor("tenant-a")
	if active != 1 || queued != 0 {
		t.Fatalf("counts after timeout = active %d queued %d, want 1/0", active, queued)
	}
}

func TestColdWorkerLimiterContextCancelRemovesQueuedCount(t *testing.T) {
	limiter := newColdWorkerLimiter(1, time.Second, nil)
	release, err := limiter.acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := limiter.acquire(ctx, "tenant-a")
		second <- err
	}()
	waitForColdCounts(t, limiter, "tenant-a", 1, 1)
	cancel()

	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second acquire err = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled acquire")
	}
	waitForColdCounts(t, limiter, "tenant-a", 1, 0)
}

func TestColdWorkerLimiterTenantOverrides(t *testing.T) {
	limiter := newColdWorkerLimiter(2, time.Second, map[string]int{"tenant-a": 1})

	if got := limiter.limit("tenant-a"); got != 1 {
		t.Fatalf("tenant override limit = %d, want 1", got)
	}
	if got := limiter.limit("tenant-b"); got != 2 {
		t.Fatalf("default tenant limit = %d, want 2", got)
	}
}

type acquireResult struct {
	release func()
	err     error
}

func waitForAcquire(t *testing.T, ch <-chan acquireResult) acquireResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for acquire")
		return acquireResult{}
	}
}

func waitForColdCounts(t *testing.T, limiter *coldWorkerLimiter, tenantID string, wantActive, wantQueued int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, queued := limiter.countsFor(tenantID)
		if active == wantActive && queued == wantQueued {
			return
		}
		time.Sleep(time.Millisecond)
	}
	active, queued := limiter.countsFor(tenantID)
	t.Fatalf("counts = active %d queued %d, want %d/%d", active, queued, wantActive, wantQueued)
}
