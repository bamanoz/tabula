package pool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bamanoz/tabula/cmd/tabula-runtime/manifest"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestPoolInvokesWarmWorkerAndReusesByTenantTarget(t *testing.T) {
	p, fake := testPool(t)
	for _, callID := range []string{"call-1", "call-2"} {
		resp, err := p.Invoke(context.Background(), invoke(callID, "tenant-a", "echo"))
		if err != nil || !resp.OK {
			t.Fatalf("Invoke(%s) = %#v, %v", callID, resp, err)
		}
	}
	if got := fake.spawnCount.Load(); got != 1 {
		t.Fatalf("spawn count = %d, want 1", got)
	}

	resp, err := p.Invoke(context.Background(), invoke("call-3", "tenant-b", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("tenant-b invoke = %#v, %v", resp, err)
	}
	if got := fake.spawnCount.Load(); got != 2 {
		t.Fatalf("spawn count after second tenant = %d, want 2", got)
	}
}

func TestPoolRejectsUnknownTargetAndTool(t *testing.T) {
	p, _ := testPool(t)
	resp, err := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "missing", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "missing"}, Tool: "echo"})
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorTargetUnknown {
		t.Fatalf("unknown target = %#v, %v", resp, err)
	}
	resp, err = p.Invoke(context.Background(), invoke("bad-tool", "tenant-a", "missing"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorToolNotFound {
		t.Fatalf("unknown tool = %#v, %v", resp, err)
	}
}

func TestPoolCancelAbandonsCallWithoutKillingWorker(t *testing.T) {
	p, fake := testPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan wire.InvokeResult, 1)
	go func() {
		resp, _ := p.Invoke(ctx, invoke("slow", "tenant-a", "slow"))
		done <- resp
	}()
	w := fake.waitForWorker(t)
	cancel()
	select {
	case resp := <-done:
		if resp.Error == nil || resp.Error.Code != wire.ErrorCancelled {
			t.Fatalf("cancel resp = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled invoke")
	}
	if !w.IsAlive() {
		t.Fatal("worker should remain alive after M2 cancel-abandon")
	}
	w.finish("slow", workerwire.WorkerResult{CallID: "slow", OK: true, Data: json.RawMessage(`{"late":true}`)})
}

func TestPoolCrashEvictsWorkerAndNextInvokeRespawns(t *testing.T) {
	p, fake := testPool(t)
	w := fake.nextWorker
	w.callErr = errors.New("worker exited: boom")
	resp, err := p.Invoke(context.Background(), invoke("crash", "tenant-a", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorInternal {
		t.Fatalf("crash resp = %#v, %v", resp, err)
	}
	fake.nextWorker = newFakeWorker()
	resp, err = p.Invoke(context.Background(), invoke("after-crash", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("after crash = %#v, %v", resp, err)
	}
	if got := fake.spawnCount.Load(); got != 2 {
		t.Fatalf("spawn count = %d, want 2", got)
	}
}

func TestPoolReloadEvictsTarget(t *testing.T) {
	p, fake := testPool(t)
	if resp, err := p.Invoke(context.Background(), invoke("call-1", "tenant-a", "echo")); err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	evicted := p.Reload(&wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"})
	if len(evicted) != 1 || evicted[0].ID != "fs" {
		t.Fatalf("evicted = %#v", evicted)
	}
	if !fake.lastWorker.shutdown.Load() {
		t.Fatal("reload should shut down worker")
	}
}

func invoke(callID, tenantID, tool string) wire.Invoke {
	return wire.Invoke{Op: wire.OpInvoke, CallID: callID, TenantID: tenantID, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: tool, Args: json.RawMessage(`{"x":1}`)}
}

func testPool(t *testing.T) (*Pool, *fakePolicy) {
	t.Helper()
	store, err := manifest.NewStore([]string{testPluginDir(t)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 10), nextWorker: newFakeWorker()}
	return New("main", store, fake), fake
}

func testPluginDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[[tools]]
name = "echo"

[[tools]]
name = "slow"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	if err := os.MkdirAll(filepath.Join(dir, "fs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fs", "plugin.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fakePolicy struct {
	spawnCount atomic.Int32
	spawned    chan *fakeWorker
	nextWorker *fakeWorker
	lastWorker *fakeWorker
}

func (f *fakePolicy) Spawn(context.Context, policy.SpawnReq) (policy.Worker, error) {
	f.spawnCount.Add(1)
	w := f.nextWorker
	if w == nil {
		w = newFakeWorker()
	}
	f.lastWorker = w
	select {
	case f.spawned <- w:
	default:
	}
	return w, nil
}

func (f *fakePolicy) waitForWorker(t *testing.T) *fakeWorker {
	t.Helper()
	select {
	case w := <-f.spawned:
		return w
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker spawn")
		return nil
	}
}

type fakeWorker struct {
	mu       sync.Mutex
	alive    bool
	init     bool
	callErr  error
	waiters  map[string]chan workerwire.WorkerResult
	shutdown atomic.Bool
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{alive: true, waiters: map[string]chan workerwire.WorkerResult{}}
}

func (w *fakeWorker) Init(context.Context, workerwire.WorkerInit) error {
	w.init = true
	return nil
}

func (w *fakeWorker) Call(_ context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error) {
	if w.callErr != nil {
		w.alive = false
		return workerwire.WorkerResult{}, w.callErr
	}
	if call.Tool == "slow" {
		ch := make(chan workerwire.WorkerResult, 1)
		w.mu.Lock()
		w.waiters[call.CallID] = ch
		w.mu.Unlock()
		return <-ch, nil
	}
	return workerwire.WorkerResult{CallID: call.CallID, OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
}

func (w *fakeWorker) finish(callID string, result workerwire.WorkerResult) {
	w.mu.Lock()
	ch := w.waiters[callID]
	w.mu.Unlock()
	if ch != nil {
		ch <- result
	}
}

func (w *fakeWorker) Shutdown(context.Context) error {
	w.shutdown.Store(true)
	w.alive = false
	return nil
}

func (w *fakeWorker) Wait() (policy.ExitInfo, error) { return policy.ExitInfo{}, nil }

func (w *fakeWorker) IsAlive() bool { return w.alive }
