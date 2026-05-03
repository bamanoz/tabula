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

func TestPoolCapabilitiesUpgradeFromManifestToWorkerReady(t *testing.T) {
	p, _ := testPool(t)

	caps := p.Capabilities()
	if len(caps) != 1 || caps[0].Target.ID != "fs" || caps[0].State != wire.CapabilityStateManifestLoaded || caps[0].Source != wire.CapabilitySourceManifest {
		t.Fatalf("initial capabilities = %#v", caps)
	}
	if len(caps[0].Tools) != 2 || caps[0].Tools[0].Name != "echo" || caps[0].Tools[1].Name != "slow" {
		t.Fatalf("initial tools = %#v", caps[0].Tools)
	}

	resp, err := p.Invoke(context.Background(), invoke("call-ready", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}

	caps = p.Capabilities()
	if len(caps) != 1 || caps[0].State != wire.CapabilityStateReady || caps[0].Source != wire.CapabilitySourceWorker || caps[0].Revision != 2 {
		t.Fatalf("ready capabilities = %#v", caps)
	}
	if len(caps[0].Hooks) != 1 || caps[0].Hooks[0].Event != "before_tool_call" {
		t.Fatalf("ready hooks = %#v", caps[0].Hooks)
	}
}

func TestPoolCapabilitiesTrackWorkerToolsUpdated(t *testing.T) {
	p, fake := testPool(t)
	resp, err := p.Invoke(context.Background(), invoke("call-tools", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	fake.lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerToolsUpdated{Op: workerwire.OpToolsUpdated, Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic_extra"}, {Name: "echo"}}}})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		caps := p.Capabilities()
		if len(caps) == 1 && caps[0].Revision == 7 && len(caps[0].Tools) == 2 && caps[0].Tools[0].Name == "dynamic_extra" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tools update not reflected: %#v", p.Capabilities())
}

func TestPoolAsyncFramesPublishCatalogAndLifecycle(t *testing.T) {
	p, _ := testPool(t)
	resp, err := p.Invoke(context.Background(), invoke("call-async", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	var sawStarting, sawReady, sawCatalog bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && (!sawStarting || !sawReady || !sawCatalog) {
		select {
		case frame := <-p.AsyncFrames():
			switch msg := frame.(type) {
			case wire.LifecycleNotice:
				sawStarting = sawStarting || msg.State == wire.LifecycleStateStarting
				sawReady = sawReady || msg.State == wire.LifecycleStateReady
			case wire.CatalogUpdate:
				sawCatalog = msg.Revision == 2 && msg.State == wire.CapabilityStateReady && len(msg.Hooks) == 1
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !sawStarting || !sawReady || !sawCatalog {
		t.Fatalf("expected starting/ready/catalog frames, got starting=%v ready=%v catalog=%v", sawStarting, sawReady, sawCatalog)
	}
}

func TestPoolAsyncFramesPublishWorkerReplySendAndLog(t *testing.T) {
	p, fake := testPool(t)
	resp, err := p.Invoke(context.Background(), invoke("call-events", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	fake.lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerEventReply{Op: workerwire.OpEventReply, CallID: "hook-1", Action: wire.HookActionRewrite, Data: json.RawMessage(`{"tool":"safe"}`)}})
	fake.lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerSend{Op: workerwire.OpSend, Channel: "bus", Type: "notify", Payload: json.RawMessage(`{"x":1}`), SessionID: "sess-1"}})
	fake.lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerLog{Op: workerwire.OpLog, Level: "info", Message: "ready", Fields: json.RawMessage(`{"worker":1}`)}})

	var sawReply, sawSend, sawLog bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && (!sawReply || !sawSend || !sawLog) {
		select {
		case frame := <-p.AsyncFrames():
			switch msg := frame.(type) {
			case wire.HookEventReply:
				sawReply = msg.CallID == "hook-1" && msg.Action == wire.HookActionRewrite
			case wire.PluginSend:
				sawSend = msg.Type == "notify" && msg.Target.ID == "fs"
			case wire.PluginLog:
				sawLog = msg.Message == "ready" && msg.Target.ID == "fs"
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !sawReply || !sawSend || !sawLog {
		t.Fatalf("expected reply/send/log frames, got reply=%v send=%v log=%v", sawReply, sawSend, sawLog)
	}
}

func TestPoolHookEventRoutesToWorkerReply(t *testing.T) {
	p, _ := testPool(t)
	reply, err := p.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, CallID: "hook-1", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"echo"}`)})
	if err != nil {
		t.Fatalf("HookEvent: %v", err)
	}
	if reply == nil || reply.CallID != "hook-1" || reply.Action != wire.HookActionRewrite {
		t.Fatalf("unexpected reply: %#v", reply)
	}
}

func TestPoolPrimeTargetsInitializesWithoutInvoke(t *testing.T) {
	p, fake := testPool(t)
	p.PrimeTargets(context.Background(), nil)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		caps := p.Capabilities()
		if len(caps) == 1 && caps[0].State == wire.CapabilityStateReady && caps[0].Source == wire.CapabilitySourceWorker {
			if got := fake.spawnCount.Load(); got != 1 {
				t.Fatalf("spawn count = %d, want 1", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("prime did not make target ready: %#v", p.Capabilities())
}

func TestPoolPrimeTargetsRepublishesReadyCatalogWithoutRespawn(t *testing.T) {
	p, fake := testPool(t)
	p.PrimeTargets(context.Background(), nil)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		caps := p.Capabilities()
		if len(caps) == 1 && caps[0].State == wire.CapabilityStateReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	drainAsyncFrames(p)

	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	p.PrimeTargets(context.Background(), &target)

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-p.AsyncFrames():
			if update, ok := frame.(wire.CatalogUpdate); ok && update.Target.ID == "fs" && update.State == wire.CapabilityStateReady && len(update.Hooks) == 1 {
				if got := fake.spawnCount.Load(); got != 1 {
					t.Fatalf("spawn count = %d, want 1", got)
				}
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("prime did not republish ready catalog update")
}

func TestPoolInitTimeAsyncFramesAreNotDropped(t *testing.T) {
	p, fake := testPool(t)
	fake.nextWorker = newFakeWorker()
	fake.nextWorker.initEvents = []policy.WorkerAsyncEvent{
		{Frame: &workerwire.WorkerLog{Op: workerwire.OpLog, Level: "info", Message: "init ready"}},
		{Frame: &workerwire.WorkerToolsUpdated{Op: workerwire.OpToolsUpdated, Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic_extra"}, {Name: "echo"}}}},
	}

	resp, err := p.Invoke(context.Background(), invoke("call-init-async", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}

	var sawInitLog, sawInitTools bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && (!sawInitLog || !sawInitTools) {
		select {
		case frame := <-p.AsyncFrames():
			switch msg := frame.(type) {
			case wire.PluginLog:
				sawInitLog = msg.Message == "init ready"
			case wire.CatalogUpdate:
				sawInitTools = msg.Revision == 7 && len(msg.Tools) == 2 && msg.Tools[0].Name == "dynamic_extra"
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !sawInitLog || !sawInitTools {
		t.Fatalf("expected init-time async frames, got log=%v tools=%v", sawInitLog, sawInitTools)
	}

	caps := p.Capabilities()
	if len(caps) != 1 || caps[0].Revision != 7 || len(caps[0].Tools) != 2 || caps[0].Tools[0].Name != "dynamic_extra" {
		t.Fatalf("capabilities after init-time update = %#v", caps)
	}
}

func TestPoolInitFailureMarksTargetFailed(t *testing.T) {
	p, fake := testPool(t)
	fake.nextWorker = newFakeWorker()
	fake.nextWorker.initErr = errors.New("boom")

	resp, err := p.Invoke(context.Background(), invoke("call-fail", "tenant-a", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorInternal {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}

	caps := p.Capabilities()
	if len(caps) != 1 || caps[0].State != wire.CapabilityStateFailed {
		t.Fatalf("failed capabilities = %#v", caps)
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
	w.finish("slow", workerwire.WorkerResult{Op: workerwire.OpResult, CallID: "slow", OK: true, Data: json.RawMessage(`{"late":true}`)})
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
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-p.AsyncFrames():
			if notice, ok := frame.(wire.LifecycleNotice); ok && notice.Target.ID == "fs" && notice.State == wire.LifecycleStateStopping {
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("reload did not publish stopping lifecycle notice")
}

func invoke(callID, tenantID, tool string) wire.Invoke {
	return wire.Invoke{Op: wire.OpInvoke, CallID: callID, TenantID: tenantID, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: tool, Args: json.RawMessage(`{"x":1}`)}
}

func drainAsyncFrames(p *Pool) {
	for {
		select {
		case <-p.AsyncFrames():
		default:
			return
		}
	}
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
	mu         sync.Mutex
	alive      bool
	init       bool
	initErr    error
	initEvents []policy.WorkerAsyncEvent
	callErr    error
	waiters    map[string]chan workerwire.WorkerResult
	events     chan policy.WorkerAsyncEvent
	shutdown   atomic.Bool
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{alive: true, waiters: map[string]chan workerwire.WorkerResult{}, events: make(chan policy.WorkerAsyncEvent, 16)}
}

func (w *fakeWorker) Init(context.Context, workerwire.WorkerInit) (workerwire.WorkerInitAck, error) {
	w.init = true
	if w.initErr != nil {
		w.alive = false
		return workerwire.WorkerInitAck{}, w.initErr
	}
	for _, event := range w.initEvents {
		w.emit(event)
	}
	return workerwire.WorkerInitAck{Op: workerwire.OpInitAck, Ready: true, Tools: []wire.ToolSpec{{Name: "echo"}, {Name: "slow"}}, Subscriptions: []wire.HookSpec{{Event: "before_tool_call", Priority: 100}}}, nil
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
	return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: call.CallID, OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
}

func (w *fakeWorker) HookEvent(_ context.Context, event workerwire.WorkerEvent) (*workerwire.WorkerEventReply, error) {
	if event.ReplyMode == workerwire.ReplyModeNone {
		return nil, nil
	}
	return &workerwire.WorkerEventReply{Op: workerwire.OpEventReply, CallID: event.CallID, Action: wire.HookActionRewrite, Data: json.RawMessage(`{"tool":"safe_echo"}`), Reason: "rewritten"}, nil
}

func (w *fakeWorker) Events() <-chan policy.WorkerAsyncEvent { return w.events }

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
	if w.events != nil {
		close(w.events)
		w.events = nil
	}
	return nil
}

func (w *fakeWorker) Wait() (policy.ExitInfo, error) { return policy.ExitInfo{}, nil }

func (w *fakeWorker) IsAlive() bool { return w.alive }

func (w *fakeWorker) emit(event policy.WorkerAsyncEvent) {
	w.events <- event
}
