package plugin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisorRestartsWithExponentialBackoffAndCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &scriptedSupervisorRuntime{exitErrors: []error{
		errors.New("crash 1"),
		errors.New("crash 2"),
		errors.New("crash 3"),
		errors.New("crash 4"),
		errors.New("crash 5"),
		errors.New("crash 6"),
	}}
	clock := &testSupervisorClock{now: time.Unix(100, 0)}
	sup := NewSupervisor(runtime, SupervisorPolicy{
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		MaxRestarts:    5,
		RestartWindow:  time.Minute,
		CleanRunReset:  120 * time.Second,
	})
	sup.clock = clock

	var starts int
	var slept []time.Duration
	err := sup.Supervise(ctx, testManifestForSupervisor("flaky"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
		OnStart: func(*Handle) {
			starts++
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			return clock.Sleep(ctx, d)
		},
	})

	if err == nil || !strings.Contains(err.Error(), "restart budget exhausted") {
		t.Fatalf("expected restart budget exhaustion, got %v", err)
	}
	if starts != 6 { // initial start + 5 allowed restarts
		t.Fatalf("starts=%d want 6", starts)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("sleep count=%d want %d (%v)", len(slept), len(want), slept)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("sleep[%d]=%s want %s (all=%v)", i, slept[i], want[i], slept)
		}
	}
}

func TestSupervisorBackoffCapsAtPolicyMax(t *testing.T) {
	ctx := context.Background()
	runtime := &scriptedSupervisorRuntime{exitErrors: []error{
		errors.New("crash 1"), errors.New("crash 2"), errors.New("crash 3"),
		errors.New("crash 4"), errors.New("crash 5"), errors.New("crash 6"),
	}}
	clock := &testSupervisorClock{now: time.Unix(200, 0)}
	sup := NewSupervisor(runtime, SupervisorPolicy{
		InitialBackoff: time.Second,
		MaxBackoff:     3 * time.Second,
		MaxRestarts:    5,
		RestartWindow:  time.Minute,
		CleanRunReset:  120 * time.Second,
	})
	sup.clock = clock

	var slept []time.Duration
	_ = sup.Supervise(ctx, testManifestForSupervisor("capped"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			return clock.Sleep(ctx, d)
		},
	})

	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("sleep count=%d want %d (%v)", len(slept), len(want), slept)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("sleep[%d]=%s want %s (all=%v)", i, slept[i], want[i], slept)
		}
	}
}

func TestSupervisorCleanRunResetsRestartBudget(t *testing.T) {
	ctx := context.Background()
	runtime := &scriptedSupervisorRuntime{exitErrors: []error{
		errors.New("crash 1"),
		errors.New("crash 2"),
		errors.New("crash after clean run"),
		errors.New("final crash"),
	}}
	runtime.startedAt = []time.Time{
		time.Unix(130, 0),
		time.Unix(131, 0),
		time.Unix(2, 0), // this one appears to have run cleanly for >120s
		time.Unix(134, 0),
		time.Unix(136, 0),
	}
	clock := &testSupervisorClock{now: time.Unix(130, 0)}
	sup := NewSupervisor(runtime, SupervisorPolicy{
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		MaxRestarts:    2,
		RestartWindow:  time.Minute,
		CleanRunReset:  120 * time.Second,
	})
	sup.clock = clock

	var starts int
	var slept []time.Duration
	err := sup.Supervise(ctx, testManifestForSupervisor("reset"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
		OnStart:      func(*Handle) { starts++ },
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			return clock.Sleep(ctx, d)
		},
	})

	if err == nil || !strings.Contains(err.Error(), "restart budget exhausted") {
		t.Fatalf("expected final restart budget exhaustion, got %v", err)
	}
	if starts != 5 {
		t.Fatalf("starts=%d want 5", starts)
	}
	want := []time.Duration{time.Second, 2 * time.Second, time.Second, 2 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("sleep count=%d want %d (%v)", len(slept), len(want), slept)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("sleep[%d]=%s want %s (all=%v)", i, slept[i], want[i], slept)
		}
	}
}

func TestSupervisorDoesNotRestartManifestErrors(t *testing.T) {
	runtime := &scriptedSupervisorRuntime{spawnErrs: []error{&ManifestError{Path: "plugin.toml", Err: errors.New("id required")}}}
	sup := NewSupervisor(runtime, SupervisorPolicy{InitialBackoff: time.Millisecond})

	err := sup.Supervise(context.Background(), testManifestForSupervisor("bad"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
	})
	if err == nil {
		t.Fatal("expected manifest error")
	}
	if runtime.SpawnCount() != 1 {
		t.Fatalf("spawn count=%d want 1", runtime.SpawnCount())
	}
}

func TestSupervisorDoesNotRestartNonRestartableErrors(t *testing.T) {
	runtime := &scriptedSupervisorRuntime{spawnErrs: []error{NonRestartable(errors.New("protocol mismatch"))}}
	sup := NewSupervisor(runtime, SupervisorPolicy{InitialBackoff: time.Millisecond})

	err := sup.Supervise(context.Background(), testManifestForSupervisor("bad-protocol"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "protocol mismatch") {
		t.Fatalf("expected protocol mismatch, got %v", err)
	}
	if runtime.SpawnCount() != 1 {
		t.Fatalf("spawn count=%d want 1", runtime.SpawnCount())
	}
}

func TestSupervisorReportsRestartAttempts(t *testing.T) {
	ctx := context.Background()
	runtime := &scriptedSupervisorRuntime{exitErrors: []error{errors.New("crash")}}
	clock := &testSupervisorClock{now: time.Unix(300, 0)}
	sup := NewSupervisor(runtime, SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	})
	sup.clock = clock
	var restartCounts []int
	var restartErrs []error

	_ = sup.Supervise(ctx, testManifestForSupervisor("notify"), nil, SupervisorOptions{
		SpawnOptions: SpawnOptions{ProtocolVersion: 1},
		OnRestart: func(_ *Handle, err error, restartCount int, _ time.Duration) {
			restartCounts = append(restartCounts, restartCount)
			restartErrs = append(restartErrs, err)
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			return clock.Sleep(ctx, d)
		},
	})

	if len(restartCounts) != 1 || restartCounts[0] != 1 {
		t.Fatalf("restart notifications=%+v want [1]", restartCounts)
	}
	if len(restartErrs) != 1 || restartErrs[0] == nil || restartErrs[0].Error() != "crash" {
		t.Fatalf("restart error not reported: %+v", restartErrs)
	}
}

func testManifestForSupervisor(id string) *Manifest {
	return &Manifest{ID: id, Name: id, Version: "1.0.0", Runtime: "python", Entry: "run.py"}
}

type scriptedSupervisorRuntime struct {
	mu         sync.Mutex
	spawnErrs  []error
	exitErrors []error
	startedAt  []time.Time
	spawnCount int
}

func (r *scriptedSupervisorRuntime) Spawn(_ context.Context, manifest *Manifest, _ map[string]any, opts SpawnOptions) (*Handle, error) {
	r.mu.Lock()
	idx := r.spawnCount
	r.spawnCount++
	var spawnErr error
	if idx < len(r.spawnErrs) {
		spawnErr = r.spawnErrs[idx]
	}
	var exitErr error
	if idx < len(r.exitErrors) {
		exitErr = r.exitErrors[idx]
	} else {
		exitErr = errors.New("scripted crash")
	}
	var startedAt time.Time
	if idx < len(r.startedAt) {
		startedAt = r.startedAt[idx]
	}
	r.mu.Unlock()

	if spawnErr != nil {
		return nil, spawnErr
	}
	h := NewHandle(manifest.ID, nil)
	h.MarkAlive()
	if !startedAt.IsZero() {
		h.mu.Lock()
		h.startedAt = startedAt
		h.mu.Unlock()
	}
	_ = h.MarkRegistered(&RegisterParams{ProtocolVersion: opts.ProtocolVersion, PluginID: manifest.ID})
	if opts.OnExit != nil {
		opts.OnExit(h, exitErr)
	}
	return h, nil
}

func (r *scriptedSupervisorRuntime) SpawnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spawnCount
}

type testSupervisorClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testSupervisorClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now.IsZero() {
		c.now = time.Unix(0, 0)
	}
	return c.now
}

func (c *testSupervisorClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

func (c *testSupervisorClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	if c.now.IsZero() {
		c.now = time.Unix(0, 0)
	}
	c.now = c.now.Add(d)
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return errSleepCancelled(ctx.Err())
	default:
		return nil
	}
}

func errSleepCancelled(err error) error {
	return errors.Join(errors.New("plugin supervisor: sleep cancelled"), err)
}
