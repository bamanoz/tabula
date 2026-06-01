package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
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

func TestPoolInvokesColdPluginWithoutBlockingSecondCall(t *testing.T) {
	dir := t.TempDir()
	body := `id = "question"
name = "Question"
version = "0.1.0"
runtime = "python"
entry = "run.py"
worker_mode = "cold"

[[tools]]
name = "question"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	writePoolPluginBody(t, filepath.Join(dir, "question", "plugin.toml"), body)
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"tenant-a": {PluginDirs: []string{dir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	slowDone := make(chan struct{})
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 10), workerFactory: func(req policy.SpawnReq) *fakeWorker {
		if req.Mode != policy.SpawnModeCold {
			t.Fatalf("spawn mode = %q, want cold", req.Mode)
		}
		w := newFakeWorker()
		w.callFn = func(ctx context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error) {
			if call.CallID == "call-1" {
				select {
				case <-slowDone:
				case <-ctx.Done():
					return workerwire.WorkerResult{}, ctx.Err()
				}
			}
			return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: call.CallID, OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
		}
		return w
	}}
	p := New("main", store, fake, Options{AllowedTenants: []string{"tenant-a"}})
	t.Cleanup(p.Close)

	firstDone := make(chan wire.InvokeResult, 1)
	go func() {
		resp, _ := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "call-1", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "question"}, Tool: "question"})
		firstDone <- resp
	}()
	fake.waitForWorker(t)

	secondDone := make(chan wire.InvokeResult, 1)
	go func() {
		resp, _ := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "call-2", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "question"}, Tool: "question"})
		secondDone <- resp
	}()

	select {
	case resp := <-secondDone:
		if !resp.OK {
			t.Fatalf("second invoke = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("second cold plugin invoke blocked behind first call")
	}
	if got := fake.spawnCount.Load(); got != 2 {
		t.Fatalf("spawn count = %d, want 2", got)
	}
	close(slowDone)
	resp := <-firstDone
	if !resp.OK {
		t.Fatalf("first invoke = %#v", resp)
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

func TestPoolUsesTenantScopedDuplicatePluginCatalogs(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	writePoolPlugin(t, filepath.Join(alphaDir, "fs", "plugin.toml"), "fs", "alpha_read")
	writePoolPlugin(t, filepath.Join(betaDir, "fs", "plugin.toml"), "fs", "beta_read")
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"alpha": {PluginDirs: []string{alphaDir}},
		"beta":  {PluginDirs: []string{betaDir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 10), workerFactory: func(req policy.SpawnReq) *fakeWorker {
		worker := newFakeWorker()
		worker.callFn = func(context.Context, workerwire.WorkerCall) (workerwire.WorkerResult, error) {
			return workerwire.WorkerResult{CallID: req.TenantID, OK: true, Data: json.RawMessage(`"` + req.TenantID + `"`)}, nil
		}
		return worker
	}}
	p := New("main", store, fake, Options{AllowedTenants: []string{"alpha", "beta"}})

	alphaResp, err := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "alpha-call", TenantID: "alpha", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "alpha_read"})
	if err != nil || !alphaResp.OK || string(alphaResp.Data) != `"alpha"` {
		t.Fatalf("alpha invoke = %#v err=%v", alphaResp, err)
	}
	betaResp, err := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "beta-call", TenantID: "beta", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "beta_read"})
	if err != nil || !betaResp.OK || string(betaResp.Data) != `"beta"` {
		t.Fatalf("beta invoke = %#v err=%v", betaResp, err)
	}
	wrongResp, err := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "wrong-call", TenantID: "alpha", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "beta_read"})
	if err != nil || wrongResp.OK || wrongResp.Error == nil || wrongResp.Error.Code != wire.ErrorToolNotFound {
		t.Fatalf("cross-tenant tool invoke = %#v err=%v", wrongResp, err)
	}
	requests := fake.spawnRequests()
	if len(requests) != 2 || requests[0].TenantID != "alpha" || requests[1].TenantID != "beta" {
		t.Fatalf("spawn requests = %#v", requests)
	}
}

func TestPoolRoutesTenantHookEventToTenantCatalog(t *testing.T) {
	alphaDir := t.TempDir()
	writePoolPlugin(t, filepath.Join(alphaDir, "fs", "plugin.toml"), "fs", "alpha_read")
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"alpha": {PluginDirs: []string{alphaDir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	p := New("main", store, &fakePolicy{spawned: make(chan *fakeWorker, 10)}, Options{AllowedTenants: []string{"alpha"}})
	t.Cleanup(p.Close)

	reply, err := p.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, TenantID: "alpha", CallID: "hook-alpha", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"alpha_read"}`)})
	if err != nil {
		t.Fatalf("HookEvent: %v", err)
	}
	if reply == nil || reply.CallID != "hook-alpha" {
		t.Fatalf("reply = %#v", reply)
	}
}

func TestPoolAllowsTenantDiscoveredAfterStartupReload(t *testing.T) {
	home := t.TempDir()
	alphaPlugins := filepath.Join(home, "tenants", "alpha", "plugins")
	writePoolPlugin(t, filepath.Join(alphaPlugins, "fs", "plugin.toml"), "fs", "alpha_read")
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"bootstrap": {PluginDirs: []string{filepath.Join(home, "tenants", "bootstrap", "plugins")}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	store.SetTabulaHome(home)
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 10), workerFactory: func(req policy.SpawnReq) *fakeWorker {
		worker := newFakeWorker()
		worker.callFn = func(context.Context, workerwire.WorkerCall) (workerwire.WorkerResult, error) {
			return workerwire.WorkerResult{CallID: req.TenantID, OK: true, Data: json.RawMessage(`"alpha"`)}, nil
		}
		return worker
	}}
	p := New("main", store, fake, Options{AllowedTenants: []string{"bootstrap"}})
	t.Cleanup(p.Close)

	if err := store.ReloadTenant("alpha"); err != nil {
		t.Fatalf("ReloadTenant: %v", err)
	}
	p.Reload(nil, "alpha")
	resp, err := p.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "alpha-call", TenantID: "alpha", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "alpha_read"})
	if err != nil || !resp.OK || string(resp.Data) != `"alpha"` {
		t.Fatalf("late tenant invoke = %#v err=%v", resp, err)
	}
}

func TestPoolDoesNotExposeSkillCapabilities(t *testing.T) {
	p, _ := testSkillPool(t, Options{})
	if caps := p.Capabilities(); len(caps) != 0 {
		t.Fatalf("skill targets must not publish executable capabilities: %#v", caps)
	}
}

func TestPoolCapabilitiesTrackWorkerToolsUpdated(t *testing.T) {
	p, fake := testPool(t)
	resp, err := p.Invoke(context.Background(), invoke("call-tools", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	fake.lastSpawnedWorker().emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerToolsUpdated{Op: workerwire.OpToolsUpdated, Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic_extra"}, {Name: "echo"}}}})

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

func TestPoolAcceptsFirstToolsUpdatedAtReadyRevision(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.initEvents = []policy.WorkerAsyncEvent{
		{Frame: &workerwire.WorkerToolsUpdated{Op: workerwire.OpToolsUpdated, Revision: 2, Tools: []wire.ToolSpec{{Name: "mcp__filesystem__list_allowed_directories"}, {Name: "mcp_call"}}}},
	}
	fake.setNextWorker(w)

	resp, err := p.Invoke(context.Background(), invoke("call-mcp", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		caps := p.Capabilities()
		if len(caps) == 1 && caps[0].Revision == 2 && len(caps[0].Tools) == 2 && caps[0].Tools[0].Name == "mcp__filesystem__list_allowed_directories" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("same-revision dynamic tools update not reflected: %#v", p.Capabilities())
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
	lastWorker := fake.lastSpawnedWorker()
	lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerEventReply{Op: workerwire.OpEventReply, CallID: "hook-1", Action: wire.HookActionRewrite, Data: json.RawMessage(`{"tool":"safe"}`)}})
	lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerSend{Op: workerwire.OpSend, Channel: "bus", Type: "notify", Payload: json.RawMessage(`{"x":1}`), SessionID: "sess-1"}})
	lastWorker.emit(policy.WorkerAsyncEvent{Frame: &workerwire.WorkerLog{Op: workerwire.OpLog, Level: "info", Message: "ready", Fields: json.RawMessage(`{"worker":1}`)}})

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
	w := newFakeWorker()
	w.initEvents = []policy.WorkerAsyncEvent{
		{Frame: &workerwire.WorkerLog{Op: workerwire.OpLog, Level: "info", Message: "init ready"}},
		{Frame: &workerwire.WorkerToolsUpdated{Op: workerwire.OpToolsUpdated, Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic_extra"}, {Name: "echo"}}}},
	}
	fake.setNextWorker(w)

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
	w := newFakeWorker()
	w.initErr = errors.New("boom")
	fake.setNextWorker(w)

	resp, err := p.Invoke(context.Background(), invoke("call-fail", "tenant-a", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorInternal {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}

	caps := p.Capabilities()
	if len(caps) != 1 || caps[0].State != wire.CapabilityStateFailed {
		t.Fatalf("failed capabilities = %#v", caps)
	}
}

func TestPoolInitFailureReturnsRedactedWorkerDiagnostics(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.initErr = errors.New("bare policy: read worker init ack: worker wire: eof (worker stderr captured: 2 lines, 122 bytes; hint: likely legacy register_request/stdio plugin SDK, not the M2 worker protocol; raw secret_token=super-secret)")
	fake.setNextWorker(w)

	resp, err := p.Invoke(context.Background(), invoke("call-redacted", "tenant-a", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorInternal {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	message := resp.Error.Message
	if !strings.Contains(message, "worker initialization failed") || !strings.Contains(message, "worker stderr captured: 2 lines, 122 bytes") {
		t.Fatalf("expected bounded stderr diagnostic, got %q", message)
	}
	if !strings.Contains(message, "likely legacy register_request/stdio plugin SDK") {
		t.Fatalf("expected legacy hint, got %q", message)
	}
	if strings.Contains(message, "super-secret") || strings.Contains(message, "worker wire: eof") {
		t.Fatalf("expected client-facing message to redact raw stderr/error detail, got %q", message)
	}
}

func TestPoolHookEventInitFailureReturnsRedactedWorkerDiagnostics(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.initErr = errors.New("bare policy: read worker init ack: worker wire: eof (worker stderr captured: 2 lines, 122 bytes; hint: likely legacy register_request/stdio plugin SDK, not the M2 worker protocol; raw api_key=super-secret)")
	fake.setNextWorker(w)

	_, err := p.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, CallID: "hook-redacted", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"echo"}`)})
	if err == nil {
		t.Fatal("expected hook init error")
	}
	message := err.Error()
	if !strings.Contains(message, "worker initialization failed") || !strings.Contains(message, "worker stderr captured: 2 lines, 122 bytes") {
		t.Fatalf("expected bounded stderr diagnostic, got %q", message)
	}
	if !strings.Contains(message, "likely legacy register_request/stdio plugin SDK") {
		t.Fatalf("expected legacy hint, got %q", message)
	}
	if strings.Contains(message, "super-secret") || strings.Contains(message, "worker wire: eof") {
		t.Fatalf("expected hook init message to redact raw stderr/error detail, got %q", message)
	}
}

func TestPoolHookEventFailureReturnsRedactedWorkerDiagnostics(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.hookErr = errors.New("bare policy: read worker event reply: worker wire: eof (worker stderr captured: 1 line, 44 bytes; raw token=super-secret)")
	fake.setNextWorker(w)

	_, err := p.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, CallID: "hook-call-redacted", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"echo"}`)})
	if err == nil {
		t.Fatal("expected hook event error")
	}
	message := err.Error()
	if !strings.Contains(message, "worker hook event failed") || !strings.Contains(message, "worker stderr captured: 1 line, 44 bytes") {
		t.Fatalf("expected bounded stderr diagnostic, got %q", message)
	}
	if strings.Contains(message, "super-secret") || strings.Contains(message, "worker wire: eof") {
		t.Fatalf("expected hook event message to redact raw stderr/error detail, got %q", message)
	}
}

func TestPoolPrimeTargetsLogsRuntimeAndEntryPathOnFailure(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.initErr = errors.New("bare policy: read worker init ack: worker wire: eof (worker stderr captured: 2 lines, 122 bytes; hint: likely legacy register_request/stdio plugin SDK, not the M2 worker protocol; raw secret_token=super-secret)")
	fake.setNextWorker(w)
	var buf bytes.Buffer
	p.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))

	p.PrimeTargets(context.Background(), nil)

	logLine := buf.String()
	if !strings.Contains(logLine, "runtime target priming failed") {
		t.Fatalf("expected priming failure log, got %q", logLine)
	}
	if !strings.Contains(logLine, "target=fs") {
		t.Fatalf("expected target field, got %q", logLine)
	}
	if !strings.Contains(logLine, "runtime=python") {
		t.Fatalf("expected runtime field, got %q", logLine)
	}
	if !strings.Contains(logLine, "entry_path=") || !strings.Contains(logLine, filepath.Join("fs", "run.py")) {
		t.Fatalf("expected entry_path field, got %q", logLine)
	}
	if !strings.Contains(logLine, "worker stderr captured: 2 lines, 122 bytes") {
		t.Fatalf("expected bounded stderr diagnostic in log, got %q", logLine)
	}
	if !strings.Contains(logLine, "likely legacy register_request/stdio plugin SDK") {
		t.Fatalf("expected legacy protocol hint in log, got %q", logLine)
	}
	if strings.Contains(logLine, "super-secret") || strings.Contains(logLine, "worker wire: eof") {
		t.Fatalf("expected log diagnostic to redact raw stderr/error detail, got %q", logLine)
	}
}

func TestPoolLifecycleCrashRedactsWorkerDiagnostics(t *testing.T) {
	p, fake := testPool(t)
	resp, err := p.Invoke(context.Background(), invoke("call-ready", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	fake.lastSpawnedWorker().emit(policy.WorkerAsyncEvent{Err: errors.New("bare policy: worker exited before result (worker stderr captured: 1 line, 25 bytes; raw password=hunter2)")})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-p.AsyncFrames():
			notice, ok := frame.(wire.LifecycleNotice)
			if !ok || notice.State != wire.LifecycleStateCrashed {
				continue
			}
			if !strings.Contains(notice.Message, "worker failed") || !strings.Contains(notice.Message, "worker stderr captured: 1 line, 25 bytes") {
				t.Fatalf("expected bounded lifecycle diagnostic, got %#v", notice)
			}
			if strings.Contains(notice.Message, "hunter2") || strings.Contains(notice.Message, "worker exited before result") {
				t.Fatalf("expected lifecycle message to redact raw stderr/error detail, got %q", notice.Message)
			}
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("timed out waiting for crashed lifecycle notice")
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

func TestPoolHonorsTenantAllowlistBeforeSpawn(t *testing.T) {
	p, fake := testPoolWithOptions(t, Options{AllowedTenants: []string{"alpha"}})
	resp, err := p.Invoke(context.Background(), invoke("allowed", "alpha", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("allowed tenant = %#v, %v", resp, err)
	}
	if got := fake.spawnCount.Load(); got != 1 {
		t.Fatalf("spawn count after allowed tenant = %d, want 1", got)
	}
	resp, err = p.Invoke(context.Background(), invoke("forbidden", "beta", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorTenantForbidden {
		t.Fatalf("forbidden tenant = %#v, %v", resp, err)
	}
	if got := fake.spawnCount.Load(); got != 1 {
		t.Fatalf("spawn count after forbidden tenant = %d, want 1", got)
	}
}

func TestPoolSetsTenantDirOnWarmWorkers(t *testing.T) {
	home := t.TempDir()
	p, fake := testPoolWithOptions(t, Options{TabulaHome: home})
	if resp, err := p.Invoke(context.Background(), invoke("warm-env", "alpha", "echo")); err != nil || !resp.OK {
		t.Fatalf("warm invoke = %#v, %v", resp, err)
	}
	reqs := fake.spawnRequests()
	if len(reqs) != 1 || reqs[0].Env["TABULA_TENANT_DIR"] != filepath.Join(home, "tenants", "alpha") {
		t.Fatalf("warm spawn env = %#v", reqs)
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
	waitForWorkerStopped(t, w)
	w.finish("slow", workerwire.WorkerResult{Op: workerwire.OpResult, CallID: "slow", OK: true, Data: json.RawMessage(`{"late":true}`)})
	if resp, err := p.Invoke(context.Background(), invoke("after-cancel", "tenant-a", "echo")); err != nil || !resp.OK {
		t.Fatalf("invoke after cancel = %#v, %v", resp, err)
	}
}

func waitForWorkerStopped(t *testing.T, w *fakeWorker) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !w.IsAlive() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker should be evicted after cancelled warm call")
}

func TestPoolTimeoutEvictsWarmWorkerAndNextInvokeSucceeds(t *testing.T) {
	p, fake := testPool(t)
	fake.setNextWorker(newFakeWorker())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	resp, err := p.Invoke(ctx, invoke("slow-timeout", "tenant-a", "slow"))
	if err != nil {
		t.Fatalf("Invoke timeout = %#v, %v", resp, err)
	}
	if resp.Error == nil || resp.Error.Code != wire.ErrorTimeout {
		t.Fatalf("expected timeout, got %#v", resp)
	}
	if got := fake.spawnCount.Load(); got != 1 {
		t.Fatalf("spawn count after timeout = %d, want 1", got)
	}
	fake.setNextWorker(newFakeWorker())
	resp, err = p.Invoke(context.Background(), invoke("after-timeout", "tenant-a", "echo"))
	if err != nil || !resp.OK {
		t.Fatalf("invoke after timeout = %#v, %v", resp, err)
	}
	if got := fake.spawnCount.Load(); got != 2 {
		t.Fatalf("spawn count after recovery = %d, want 2", got)
	}
}

func TestPoolCrashEvictsWorkerAndNextInvokeRespawns(t *testing.T) {
	p, fake := testPool(t)
	w := newFakeWorker()
	w.callErr = errors.New("worker exited: boom")
	fake.setNextWorker(w)
	resp, err := p.Invoke(context.Background(), invoke("crash", "tenant-a", "echo"))
	if err != nil || resp.Error == nil || resp.Error.Code != wire.ErrorInternal {
		t.Fatalf("crash resp = %#v, %v", resp, err)
	}
	fake.setNextWorker(newFakeWorker())
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
	if !fake.lastSpawnedWorker().shutdown.Load() {
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

func TestPoolRejectsSkillInvokes(t *testing.T) {
	p, _ := testSkillPool(t, Options{})
	resp, err := p.Invoke(context.Background(), skillInvoke("skill-0", "tenant-a", "testbed_echo"))
	if err != nil {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	if resp.Error == nil || resp.Error.Code != wire.ErrorTargetForbidden {
		t.Fatalf("expected target_forbidden, got %#v", resp)
	}
}

func invoke(callID, tenantID, tool string) wire.Invoke {
	return wire.Invoke{Op: wire.OpInvoke, CallID: callID, TenantID: tenantID, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: tool, Args: json.RawMessage(`{"x":1}`)}
}

func skillInvoke(callID, tenantID, tool string) wire.Invoke {
	return wire.Invoke{Op: wire.OpInvoke, CallID: callID, TenantID: tenantID, Target: wire.Target{Kind: wire.TargetKindSkill, ID: "skill:testbed-echo"}, Tool: tool, Args: json.RawMessage(`{"text":"hello"}`)}
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
	return testPoolWithOptions(t, Options{})
}

func testPoolWithOptions(t *testing.T, opts Options) (*Pool, *fakePolicy) {
	t.Helper()
	store, err := manifest.NewStore([]string{testPluginDir(t)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 10), nextWorker: newFakeWorker()}
	return New("main", store, fake, opts), fake
}

func testSkillPool(t *testing.T, opts Options) (*Pool, *fakePolicy) {
	t.Helper()
	store, err := manifest.NewStore([]string{testSkillDir(t)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 64)}
	return New("main", store, fake, opts), fake
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

func writePoolPlugin(t *testing.T, path, id, tool string) {
	t.Helper()
	body := `id = "` + id + `"
name = "Plugin"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[[tools]]
name = "` + tool + `"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	writePoolPluginBody(t, path, body)
}

func writePoolPluginBody(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "testbed-echo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
name: testbed-echo
description: Deterministic test skill.
tools:
  - name: testbed_echo
    description: Echo input.
    params:
      text: { type: string }
    exec: "python3 ${SKILL_DIR}/scripts/run.py tool testbed_echo"
---
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fakePolicy struct {
	mu            sync.Mutex
	spawnCount    atomic.Int32
	spawned       chan *fakeWorker
	spawnReqs     []policy.SpawnReq
	workerFactory func(policy.SpawnReq) *fakeWorker
	nextWorker    *fakeWorker
	lastWorker    *fakeWorker
}

func (f *fakePolicy) Spawn(_ context.Context, req policy.SpawnReq) (policy.Worker, error) {
	f.mu.Lock()
	f.spawnReqs = append(f.spawnReqs, req)
	f.spawnCount.Add(1)
	w := f.nextWorker
	if f.workerFactory != nil {
		w = f.workerFactory(req)
	}
	if w == nil {
		w = newFakeWorker()
	}
	f.lastWorker = w
	f.mu.Unlock()
	select {
	case f.spawned <- w:
	default:
	}
	return w, nil
}

func (f *fakePolicy) setNextWorker(w *fakeWorker) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextWorker = w
}

func (f *fakePolicy) lastSpawnedWorker() *fakeWorker {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWorker
}

func (f *fakePolicy) spawnRequests() []policy.SpawnReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]policy.SpawnReq, len(f.spawnReqs))
	copy(out, f.spawnReqs)
	return out
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
	callFn     func(context.Context, workerwire.WorkerCall) (workerwire.WorkerResult, error)
	hookErr    error
	waiters    map[string]chan workerwire.WorkerResult
	events     chan policy.WorkerAsyncEvent
	shutdown   atomic.Bool
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{alive: true, waiters: map[string]chan workerwire.WorkerResult{}, events: make(chan policy.WorkerAsyncEvent, 16)}
}

func (w *fakeWorker) Init(context.Context, workerwire.WorkerInit) (workerwire.WorkerInitAck, error) {
	w.mu.Lock()
	w.init = true
	initErr := w.initErr
	initEvents := append([]policy.WorkerAsyncEvent(nil), w.initEvents...)
	w.mu.Unlock()
	if initErr != nil {
		w.mu.Lock()
		w.alive = false
		w.mu.Unlock()
		return workerwire.WorkerInitAck{}, initErr
	}
	for _, event := range initEvents {
		w.emit(event)
	}
	return workerwire.WorkerInitAck{Op: workerwire.OpInitAck, Ready: true, Tools: []wire.ToolSpec{{Name: "echo"}, {Name: "slow"}}, Subscriptions: []wire.HookSpec{{Event: "before_tool_call", Priority: 100}}}, nil
}

func (w *fakeWorker) Call(ctx context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error) {
	w.mu.Lock()
	callFn := w.callFn
	callErr := w.callErr
	w.mu.Unlock()
	if callFn != nil {
		return callFn(ctx, call)
	}
	if callErr != nil {
		w.mu.Lock()
		w.alive = false
		w.mu.Unlock()
		return workerwire.WorkerResult{}, callErr
	}
	if call.Tool == "slow" {
		ch := make(chan workerwire.WorkerResult, 1)
		w.mu.Lock()
		w.waiters[call.CallID] = ch
		w.mu.Unlock()
		select {
		case result := <-ch:
			return result, nil
		case <-ctx.Done():
			w.mu.Lock()
			delete(w.waiters, call.CallID)
			w.mu.Unlock()
			return workerwire.WorkerResult{}, ctx.Err()
		}
	}
	return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: call.CallID, OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
}

func (w *fakeWorker) HookEvent(_ context.Context, event workerwire.WorkerEvent) (*workerwire.WorkerEventReply, error) {
	w.mu.Lock()
	hookErr := w.hookErr
	if hookErr != nil {
		w.alive = false
	}
	w.mu.Unlock()
	if hookErr != nil {
		return nil, hookErr
	}
	if event.ReplyMode == workerwire.ReplyModeNone {
		return nil, nil
	}
	return &workerwire.WorkerEventReply{Op: workerwire.OpEventReply, CallID: event.CallID, Action: wire.HookActionRewrite, Data: json.RawMessage(`{"tool":"safe_echo"}`), Reason: "rewritten"}, nil
}

func (w *fakeWorker) Events() <-chan policy.WorkerAsyncEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.events
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
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shutdown.Store(true)
	w.alive = false
	if w.events != nil {
		close(w.events)
		w.events = nil
	}
	return nil
}

func (w *fakeWorker) Wait() (policy.ExitInfo, error) { return policy.ExitInfo{}, nil }

func (w *fakeWorker) IsAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

func (w *fakeWorker) emit(event policy.WorkerAsyncEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.events != nil {
		w.events <- event
	}
}
