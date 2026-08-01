package kernel

import (
	"context"
	"encoding/json"
	"errors"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestNewHubStartsWithoutDynamicDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	if _, ok := toolDispatchEntry(hub, "echo"); ok {
		t.Fatal("expected no dynamic dispatch entries before runtime attach")
	}
}

func TestResolveToolDeadlineCapsAtOneHour(t *testing.T) {
	if got := resolveToolDeadline(0); got != 30*time.Second {
		t.Fatalf("default deadline = %v, want 30s", got)
	}
	if got := resolveToolDeadline(900_000); got != 15*time.Minute {
		t.Fatalf("fifteen-minute deadline = %v, want 15m", got)
	}
	if got := resolveToolDeadline(3_600_000); got != time.Hour {
		t.Fatalf("one-hour deadline = %v, want 1h", got)
	}
	if got := resolveToolDeadline(7_200_000); got != time.Hour {
		t.Fatalf("capped deadline = %v, want 1h", got)
	}
}

func TestHandleDynamicTool_RuntimeSourceInvokesAttachedRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"ok"`))
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{
		hub:      hub,
		name:     "test",
		tenantID: "alpha",
		session:  "s1",
		recvCh:   make(chan *Message, 4),
		receives: map[string]bool{TopicToolResult: true},
		sends:    map[string]bool{},
		state:    ClientJoined,
		done:     make(chan struct{}),
	}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	meta := json.RawMessage(`{"actor":"agent/build"}`)
	hub.tools.handleDynamicToolWithMeta("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), meta, "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "ok" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	recorded := rc.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" || recorded[0].Tool != "mcp__echo" || string(recorded[0].Meta) != string(meta) || !sameRuntimeTarget(recorded[0].Target, target) {
		t.Fatalf("unexpected runtime invoke record: %+v", recorded)
	}
}

func TestRuntimeLifecycleCrashHidesToolFromPromptsButKeepsDispatchRoutable(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "memory"}
	ready := wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mempalace_checkpoint"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	failed := ready
	failed.State = wire.CapabilityStateFailed
	rc.WithCapabilities(ready)
	rc.OnInvoke("alpha", target, "mempalace_checkpoint").Return([]byte(`"restarted"`))
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{ready}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", ready)
	if !strings.Contains(string(hub.initToolsJSON("alpha")), "mempalace_checkpoint") {
		t.Fatalf("expected ready memory tool in prompt surface: %s", string(hub.initToolsJSON("alpha")))
	}

	rc.WithCapabilities(failed)
	if err := hub.runtimeAsyncSink().LifecycleNoticed("local", wire.LifecycleNotice{Target: target, State: wire.LifecycleStateCrashed}); err != nil {
		t.Fatalf("LifecycleNoticed: %v", err)
	}
	entry := waitForToolDispatch(t, hub, "mempalace_checkpoint", func(entry toolDispatch) bool {
		return !entry.Advertise
	})
	if !sameRuntimeTarget(entry.Target, target) {
		t.Fatalf("stale dispatch target = %#v, want %#v", entry.Target, target)
	}
	if strings.Contains(string(hub.initToolsJSON("alpha")), "mempalace_checkpoint") {
		t.Fatalf("failed runtime tool should be hidden from new prompt surface: %s", string(hub.initToolsJSON("alpha")))
	}

	c := addTenantCaptureClient(t, hub, "alpha", "test", "s1", []string{TopicToolResult}, nil)
	hub.tools.handleDynamicTool("alpha", "s1", "tid-after-crash", "mempalace_checkpoint", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "restarted" {
		t.Fatalf("unexpected runtime tool result after crash: %+v", msg)
	}
	if invokes := rc.RecordedInvokes(); len(invokes) != 1 || invokes[0].Tool != "mempalace_checkpoint" {
		t.Fatalf("expected stale dispatch to route invoke, got %+v", invokes)
	}
}

func TestHandleCancelCancelsInFlightRuntimeTool(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__slow").Delay(30 * time.Second).Return([]byte(`"late"`))
	capability := wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__slow"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-cancel", "mcp__slow", json.RawMessage(`{}`), "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rc.WaitForRecordedInvokes(ctx, 1); err != nil {
		t.Fatalf("WaitForRecordedInvokes: %v", err)
	}

	hub.handleCancel("alpha", "s1")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "cancel") {
		t.Fatalf("unexpected cancelled runtime tool result: %+v", msg)
	}
	if cancels := rc.RecordedCancels(); len(cancels) != 1 || cancels[0] != "tid-cancel" {
		t.Fatalf("unexpected recorded cancels: %+v", cancels)
	}
}

func TestHandleDynamicTool_LargeRuntimeResultWithoutRewriteFailsExplicitly(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(t.TempDir()))
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"` + strings.Repeat("x", 13000) + `"`))
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "before_tool_result") {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if msg.Artifact != nil || msg.Truncated {
		t.Fatalf("expected explicit error without artifact rewrite, got %+v", msg)
	}
}

func TestHandleDynamicTool_InvokeStreamCleansKernelSpool(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"ok"`))
	capability := wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-stream", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "ok" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	spoolDir := filepath.Join(home, "run", "tool-results")
	waitForEmptySpoolDir(t, spoolDir)
}

func TestHandleDynamicTool_BroadcastsBoundedToolResultStreamEvents(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"` + strings.Repeat("x", toolResultInlineLimitBytes+100) + `"`))
	capability := wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 8), receives: map[string]bool{TopicToolResult: true, TopicToolResultStart: true, TopicToolResultDelta: true, TopicToolResultEnd: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-stream-events", "mcp__echo", json.RawMessage(`{}`), "")
	seen := map[string]*Message{}
	for len(seen) < 4 {
		msg := waitForMessage(t, c.recvCh)
		seen[msg.Topic] = msg
	}
	if seen[TopicToolResultStart] == nil || seen[TopicToolResultDelta] == nil || seen[TopicToolResultEnd] == nil || seen[TopicToolResult] == nil {
		t.Fatalf("missing stream/result events: %+v", seen)
	}
	if len(seen[TopicToolResultDelta].Text) > toolResultSpoolPreviewBytes {
		t.Fatalf("delta preview exceeded cap: %d", len(seen[TopicToolResultDelta].Text))
	}
	if !strings.Contains(seen[TopicToolResult].Output, "before_tool_result") {
		t.Fatalf("expected final explicit delivery error, got %+v", seen[TopicToolResult])
	}
}

func TestHandleDynamicTool_FailedRuntimeResultDoesNotBroadcastStreamEnd(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(t.TempDir()))
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").ReturnError(wire.Error{Code: wire.ErrorInternal, Message: "boom"})
	capability := wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true, TopicToolResultEnd: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-failed", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "boom") {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	select {
	case msg := <-c.recvCh:
		t.Fatalf("failed non-streamed result should not emit stream terminal event: %+v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitForEmptySpoolDir(t *testing.T, spoolDir string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		entries, err := os.ReadDir(spoolDir)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("ReadDir spool: %v", err)
		}
		if len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected empty kernel spool dir, found %d entries", len(entries))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInvokeResultSpoolTracksTerminalStateAndPreview(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(t.TempDir()))
	spool, err := newInvokeResultSpool(hub, "alpha", "s1", "tid-preview")
	if err != nil {
		t.Fatalf("newInvokeResultSpool: %v", err)
	}
	defer spool.Close()
	if err := spool.Start("tid-preview"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := spool.Delta("tid-preview", []byte(`"hello world"`)); err != nil {
		t.Fatalf("Delta: %v", err)
	}
	if err := spool.End("tid-preview", int64(len(`"hello world"`))); err != nil {
		t.Fatalf("End: %v", err)
	}
	if !spool.MarkTerminal(invokeResultSpoolCompleted) {
		t.Fatal("expected completed terminal transition")
	}
	if spool.MarkTerminal(invokeResultSpoolFailed) {
		t.Fatal("terminal state should not change twice")
	}
	if got := spool.TerminalState(); got != invokeResultSpoolCompleted {
		t.Fatalf("terminal state = %s", got)
	}
	if source := spool.Source(); source == nil || source.Kind != "spool_file" || source.Path == "" || source.Bytes != int64(len(`"hello world"`)) || source.Preview == "" {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestInvokeResultSpoolJanitorRemovesStaleFiles(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	spoolDir := filepath.Join(home, "run", "tool-results")
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		t.Fatalf("MkdirAll spool: %v", err)
	}
	stalePath := filepath.Join(spoolDir, "stale.json")
	freshPath := filepath.Join(spoolDir, "fresh.json")
	if err := os.WriteFile(stalePath, []byte(`"old"`), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if err := os.WriteFile(freshPath, []byte(`"new"`), 0o600); err != nil {
		t.Fatalf("write fresh: %v", err)
	}
	old := time.Now().Add(-toolResultSpoolStaleAge - time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}
	spool, err := newInvokeResultSpool(hub, "alpha", "s1", "tid-janitor")
	if err != nil {
		t.Fatalf("newInvokeResultSpool: %v", err)
	}
	if err := spool.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale spool removed, err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("expected fresh spool to remain: %v", err)
	}
}

func TestInvokeResultSpoolIdleTimeoutCancelsStartedStream(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetSessionStore(NewDiskSessionStore(t.TempDir()))
	spool, err := newInvokeResultSpool(hub, "alpha", "s1", "tid-idle")
	if err != nil {
		t.Fatalf("newInvokeResultSpool: %v", err)
	}
	defer spool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := spool.watchIdle(ctx, cancel, 10*time.Millisecond)
	defer stop()
	if err := spool.Start("tid-idle"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected idle timeout to cancel context")
	}
	if got := spool.TerminalState(); got != invokeResultSpoolTimedOut {
		t.Fatalf("terminal state = %s", got)
	}
}

func TestBusyRuntimeTargetWaitsForPromptBuildHooks(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "subagents"}
	timeout := int64(100)
	capability := wire.Capability{
		Target: target,
		Tools:  []wire.ToolSpec{{Name: "subagent_spawn"}},
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn()
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()
	if entries := hub.hooks.Subscribers("before_prompt_build"); len(entries) != 1 {
		t.Fatalf("expected runtime hook subscriber, got %+v", entries)
	}

	release := hub.markRuntimeTargetBusy("local", target)
	go func() {
		time.Sleep(20 * time.Millisecond)
		release()
	}()
	started := time.Now()
	result, ok := hub.dispatchHook("before_prompt_build", json.RawMessage(`{"context":"base"}`), tenant.DefaultID, "s1")
	if !ok {
		t.Fatal("busy runtime target hook should remain fail-open after its timeout")
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("prompt build hook did not wait for target release: %s", elapsed)
	}
	if got := len(rc.HookEvents()); got != 1 {
		t.Fatalf("prompt build hook was not dispatched after target release, got %d events", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["context"] != "base" || payload["session"] != "s1" || payload["tenant_id"] != tenant.DefaultID {
		t.Fatalf("unexpected payload: %s", string(result))
	}
}

func TestPendingRuntimeHookQueuesPromptBuildHooks(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(200)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "mempalace"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn()
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		hub.dispatchHook("before_prompt_build", json.RawMessage(`{"context":"first"}`), tenant.DefaultID, "s1")
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !hub.isRuntimeTargetBusy("local", target) {
		time.Sleep(time.Millisecond)
	}
	if !hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("pending runtime hook did not mark target busy")
	}
	responsesDone := make(chan struct{})
	go func() {
		defer close(responsesDone)
		for i := 0; i < 2; i++ {
			deadline := time.Now().Add(time.Second)
			var event runtimeapi.HookEventReq
			for time.Now().Before(deadline) {
				events := rc.HookEvents()
				if len(events) > i {
					event = events[i]
					break
				}
				time.Sleep(time.Millisecond)
			}
			if event.CallID == "" {
				return
			}
			hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{Type: string(MsgHookReply), ID: event.CallID, Action: string(khooks.ActionPass)}))
		}
	}()

	started := time.Now()
	result, ok := hub.dispatchHook("before_prompt_build", json.RawMessage(`{"context":"second"}`), tenant.DefaultID, "s1")
	if !ok {
		t.Fatal("queued runtime target hook should complete")
	}
	if elapsed := time.Since(started); elapsed < time.Millisecond {
		t.Fatalf("second prompt build hook did not wait for first target reservation: %s", elapsed)
	}
	if got := len(rc.HookEvents()); got != 2 {
		t.Fatalf("expected both prompt build hooks to be dispatched, got %d", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["context"] != "second" {
		t.Fatalf("unexpected payload: %s", string(result))
	}
	select {
	case <-responsesDone:
	case <-time.After(time.Second):
		t.Fatal("prompt hook replies did not complete")
	}

	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first hook did not complete")
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("timed-out hook did not release target busy marker")
	}
}

func TestConcurrentRuntimePromptBuildHooksSerializeTarget(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(200)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "mempalace"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn()
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	responsesDone := make(chan struct{})
	go func() {
		defer close(responsesDone)
		for i := 0; i < 2; i++ {
			deadline := time.Now().Add(time.Second)
			var event runtimeapi.HookEventReq
			for time.Now().Before(deadline) {
				events := rc.HookEvents()
				if len(events) > i {
					event = events[i]
					break
				}
				time.Sleep(time.Millisecond)
			}
			if event.CallID == "" {
				return
			}
			hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{Type: string(MsgHookReply), ID: event.CallID, Action: string(khooks.ActionPass)}))
		}
	}()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			hub.dispatchHook("before_prompt_build", json.RawMessage(`{"context":"current"}`), tenant.DefaultID, "s1")
		}()
	}
	close(start)
	wg.Wait()

	if got := len(rc.HookEvents()); got != 2 {
		t.Fatalf("expected both runtime hook events after concurrent dispatches, got %d", got)
	}
	select {
	case <-responsesDone:
	case <-time.After(time.Second):
		t.Fatal("prompt hook replies did not complete")
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("timed-out hook did not release target busy marker")
	}
}

func TestParallelRuntimeApprovalHooksDoNotFailBusyWhileFirstIsSuspended(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(1000)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-approvals"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newSuspendingExchangeRuntimeConn(hub)
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	var firstBlocked *khooks.DispatchDecision
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"exec_run","input":{"cmd":"one"}}`), tenant.DefaultID, "s1", nil)
		if ok || blocked == nil || !blocked.Pending {
			t.Errorf("expected first approval hook to suspend, ok=%v blocked=%+v", ok, blocked)
			return
		}
		firstBlocked = blocked
	}()

	select {
	case <-rc.FirstSuspended():
	case <-time.After(time.Second):
		t.Fatal("first approval hook did not suspend")
	}

	_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"exec_run","input":{"cmd":"two"}}`), tenant.DefaultID, "s1", nil)
	if !ok || blocked != nil {
		t.Fatalf("expected second approval hook to wait and run instead of failing busy, ok=%v blocked=%+v", ok, blocked)
	}

	<-firstDone
	if firstBlocked != nil {
		releaseBlockedHookDecision(firstBlocked)
	}
	if got := len(rc.HookEvents()); got != 2 {
		t.Fatalf("expected both approval hook events to run, got %d", got)
	}
}

func TestConcurrentRuntimeSecurityHooksSerializeTarget(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(200)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn()
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), tenant.DefaultID, "s1")
		}()
	}
	close(start)
	wg.Wait()

	if got := len(rc.HookEvents()); got != 2 {
		t.Fatalf("expected both runtime security hooks to run serially, got %d", got)
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("timed-out security hook did not release target busy marker")
	}
}

func TestHandleToolUseSecurityHookTimeoutSendsTerminalResult(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	timeout := int64(10)
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn()
	rc.WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()
	c := addTenantCaptureClient(t, hub, "alpha", "driver", "s1", []string{TopicToolResult}, nil)

	hub.tools.HandleToolUse(c, &Message{
		Type:  string(MsgRequest),
		Topic: TopicToolCall,
		ID:    "call-timeout",
		Name:  "exec_run",
		Input: json.RawMessage(`{"cmd":"bad"}`),
	})

	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.ID != "call-timeout" || msg.Name != "exec_run" {
		t.Fatalf("expected terminal tool result for timed-out hook, got %+v", msg)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msg.Output), &payload); err != nil {
		t.Fatalf("tool result output is not structured JSON: %q", msg.Output)
	}
	if payload["error"] != "not_invoked" || payload["kind"] != "hook_timeout" {
		t.Fatalf("unexpected not-invoked payload: %#v", payload)
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("timed-out security hook did not release target busy marker")
	}
}

type suspendingExchangeRuntimeConn struct {
	*openHookRuntimeConn
	hub            *Hub
	firstSuspended chan struct{}
	once           sync.Once
}

func newSuspendingExchangeRuntimeConn(hub *Hub) *suspendingExchangeRuntimeConn {
	return &suspendingExchangeRuntimeConn{openHookRuntimeConn: newOpenHookRuntimeConn(), hub: hub, firstSuspended: make(chan struct{})}
}

func (c *suspendingExchangeRuntimeConn) FirstSuspended() <-chan struct{} {
	return c.firstSuspended
}

func (c *suspendingExchangeRuntimeConn) SendHookEvent(ctx context.Context, req runtimeapi.HookEventReq) error {
	if err := c.openHookRuntimeConn.SendHookEvent(ctx, req); err != nil {
		return err
	}
	if len(c.HookEvents()) == 1 {
		go func() {
			c.hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{
				Type:    string(MsgHookReply),
				ID:      req.CallID,
				Action:  string(khooks.ActionSuspend),
				Reason:  "approve exec",
				Payload: json.RawMessage(`{"kind":"approval_required","question":"Approve?","details":{"tool":"exec_run"},"options":["allow once","deny once"]}`),
			}))
			c.once.Do(func() { close(c.firstSuspended) })
		}()
		return nil
	}
	go c.hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{Type: string(MsgHookReply), ID: req.CallID, Action: string(khooks.ActionPass)}))
	return nil
}

type openHookRuntimeConn struct {
	*runtimemock.RuntimeConn
	done       chan struct{}
	mu         sync.Mutex
	hookEvents []runtimeapi.HookEventReq
}

func newOpenHookRuntimeConn() *openHookRuntimeConn {
	return &openHookRuntimeConn{RuntimeConn: runtimemock.New(), done: make(chan struct{})}
}

func (c *openHookRuntimeConn) Done() <-chan struct{} { return c.done }

func (c *openHookRuntimeConn) WithCapabilities(capabilities ...wire.Capability) *openHookRuntimeConn {
	c.RuntimeConn.WithCapabilities(capabilities...)
	return c
}

func (c *openHookRuntimeConn) SendHookEvent(ctx context.Context, req runtimeapi.HookEventReq) error {
	c.mu.Lock()
	c.hookEvents = append(c.hookEvents, req)
	c.mu.Unlock()
	return c.RuntimeConn.SendHookEvent(ctx, req)
}

func (c *openHookRuntimeConn) HookEvents() []runtimeapi.HookEventReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]runtimeapi.HookEventReq, len(c.hookEvents))
	copy(out, c.hookEvents)
	return out
}

func TestBusyRuntimeTargetWaitsForSecurityHooks(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.hooks.SetDefaultTimeout(20 * time.Millisecond)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(100)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	release := hub.markRuntimeTargetBusy("local", target)
	go func() {
		time.Sleep(50 * time.Millisecond)
		release()
	}()
	started := time.Now()
	_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), tenant.DefaultID, "s1", nil)
	elapsed := time.Since(started)
	if ok || blocked == nil || blocked.Status != "timeout" {
		t.Fatalf("security hook should run after busy target is released and then time out, ok=%v blocked=%+v", ok, blocked)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("security hook did not wait for busy target release, elapsed=%s", elapsed)
	}
	if got := len(rc.HookEvents()); got != 1 {
		t.Fatalf("security hook should send after busy target release, got %d", got)
	}
}

func TestUnavailableRuntimeSecurityHookFailsClosed(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(20)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target:  target,
		Hooks:   []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:   wire.CapabilityStateReady,
		Source:  wire.CapabilitySourceWorker,
		Tenants: []string{"beta"},
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"exec_run","input":{"cmd":"echo should-not-run"}}`), "alpha", "s1", nil)
	if ok || blocked == nil || blocked.Status != "unavailable" {
		t.Fatalf("unavailable security hook should fail closed, ok=%v blocked=%+v", ok, blocked)
	}
	if got := len(rc.HookEvents()); got != 0 {
		t.Fatalf("tenant-mismatched hook should not receive event, got %d", got)
	}
}

func TestBusyRuntimeSecurityHookFailsClosedEvenWhenAnotherHookIsAvailable(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.hooks.SetDefaultTimeout(20 * time.Millisecond)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	short := int64(20)
	availableTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	busyTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-approvals"}
	available := wire.Capability{
		Target: availableTarget,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &short}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	busy := wire.Capability{
		Target: busyTarget,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 80, TimeoutMS: &short}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(available, busy)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{available, busy}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", available)
	hub.syncRuntimeCapability("local", busy)
	hub.rebuildHookIndex()

	releaseBusy := hub.markRuntimeTargetBusy("local", busyTarget)
	defer releaseBusy()
	started := time.Now()
	_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), tenant.DefaultID, "s1", nil)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("busy security hook ignored its queue budget: %s", elapsed)
	}
	if ok || blocked == nil || blocked.Target != "runtime:local:plugin:hook-approvals" || blocked.Status != "busy" {
		t.Fatalf("busy security hook should fail closed before executing remaining hooks, ok=%v blocked=%+v", ok, blocked)
	}
	if got := len(rc.HookEvents()); got != 0 {
		t.Fatalf("remaining security hooks must not run after a busy eligible hook, got %d", got)
	}
}

func TestSuspendedSecurityHookReacquiresRemainingRuntimeHookOnResume(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(200)
	approvalTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-approvals"}
	policyTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	approval := wire.Capability{
		Target: approvalTarget,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	duplicateApproval := approval
	duplicateApproval.Tenants = []string{tenant.DefaultID}
	policy := wire.Capability{
		Target: policyTarget,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 10, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(approval, duplicateApproval, policy)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{approval, duplicateApproval, policy}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", approval)
	hub.syncRuntimeCapability("local", duplicateApproval)
	hub.syncRuntimeCapability("local", policy)
	hub.rebuildHookIndex()

	type hookDispatchResult struct {
		ok      bool
		blocked *khooks.DispatchDecision
	}
	dispatchDone := make(chan hookDispatchResult, 1)
	go func() {
		_, ok, blocked := hub.hooks.DispatchDetailedExcept("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), tenant.DefaultID, "s1", nil)
		dispatchDone <- hookDispatchResult{ok: ok, blocked: blocked}
	}()
	var firstEvents []runtimeapi.HookEventReq
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		firstEvents = rc.HookEvents()
		if len(firstEvents) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(firstEvents) != 1 || firstEvents[0].Target.ID != approvalTarget.ID {
		t.Fatalf("expected only approval hook before resume, got %+v", firstEvents)
	}
	hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{ID: firstEvents[0].CallID, Action: string(khooks.ActionSuspend)}))
	var blocked *khooks.DispatchDecision
	select {
	case result := <-dispatchDone:
		if result.ok || result.blocked == nil || !result.blocked.Pending {
			t.Fatalf("expected suspended hook decision, ok=%v blocked=%+v", result.ok, result.blocked)
		}
		blocked = result.blocked
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for suspended hook decision")
	}
	if hub.isRuntimeTargetBusy("local", policyTarget) {
		t.Fatal("remaining runtime hook target stayed busy while tool was suspended")
	}

	resumeDone := make(chan hookDispatchResult, 1)
	go func() {
		_, ok, blocked := hub.hooks.ResumeModifying(blocked)
		resumeDone <- hookDispatchResult{ok: ok, blocked: blocked}
	}()
	var events []runtimeapi.HookEventReq
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events = rc.HookEvents()
		if len(events) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !hub.isRuntimeTargetBusy("local", policyTarget) {
		t.Fatal("resumed remaining runtime hook did not reacquire busy marker")
	}
	if len(events) != 2 || events[1].Target.ID != policyTarget.ID {
		t.Fatalf("expected policy hook after resume, got %+v", events)
	}
	hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{ID: events[1].CallID, Action: string(khooks.ActionPass)}))
	select {
	case result := <-resumeDone:
		if !result.ok || result.blocked != nil {
			t.Fatalf("expected resumed hook chain to continue, ok=%v blocked=%+v", result.ok, result.blocked)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resumed hook chain")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) && hub.isRuntimeTargetBusy("local", policyTarget) {
		time.Sleep(time.Millisecond)
	}
	if hub.isRuntimeTargetBusy("local", policyTarget) {
		t.Fatal("resumed remaining runtime hook did not release busy marker after reply")
	}
}

func TestRuntimeModifyingHookReplyReleasesBusyMarker(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(200)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100, TimeoutMS: &timeout}, {Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	done := make(chan struct{}, 1)
	go func() {
		_, ok := hub.hooks.Dispatch("before_prompt_build", json.RawMessage(`{"tools":[]}`), tenant.DefaultID, "s1")
		if !ok {
			t.Errorf("before_prompt_build dispatch failed")
		}
		done <- struct{}{}
	}()
	var event runtimeapi.HookEventReq
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events := rc.HookEvents()
		if len(events) == 1 {
			event = events[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if event.CallID == "" {
		t.Fatal("expected runtime hook event")
	}
	hub.hooks.HandleRuntimeResult("local", hookMessageFromKernel(&Message{ID: event.CallID, Action: string(khooks.ActionPass)}))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hook dispatch")
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("runtime hook target stayed busy after modifying hook reply")
	}
}

func TestRuntimeVoidHookDoesNotMarkTargetBusy(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "cancel-observer"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "cancel", Priority: 0}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newOpenHookRuntimeConn().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	hub.dispatchHook("cancel", json.RawMessage(`{"session":"s1"}`), tenant.DefaultID, "s1")
	if got := len(rc.HookEvents()); got != 1 {
		t.Fatalf("expected one void runtime hook event, got %d", got)
	}
	if hub.isRuntimeTargetBusy("local", target) {
		t.Fatal("void runtime hook left target busy")
	}
}

type failingHookRuntimeConn struct {
	done   chan struct{}
	closed bool
}

func newFailingHookRuntimeConn() *failingHookRuntimeConn {
	return &failingHookRuntimeConn{done: make(chan struct{})}
}

func (c *failingHookRuntimeConn) Invoke(context.Context, runtimeapi.InvokeReq) (runtimeapi.InvokeResp, error) {
	return runtimeapi.InvokeResp{}, errors.New("unexpected invoke")
}

func (c *failingHookRuntimeConn) InvokeStream(context.Context, runtimeapi.InvokeReq, runtimeapi.InvokeStreamSink) (runtimeapi.InvokeResp, error) {
	return runtimeapi.InvokeResp{}, errors.New("unexpected invoke stream")
}

func (c *failingHookRuntimeConn) Cancel(context.Context, string) error {
	return errors.New("unexpected cancel")
}

func (c *failingHookRuntimeConn) Health(context.Context) (runtimeapi.HealthResp, error) {
	return runtimeapi.HealthResp{}, errors.New("unexpected health")
}

func (c *failingHookRuntimeConn) ListCapabilities(context.Context) (runtimeapi.ListCapabilitiesResp, error) {
	return runtimeapi.ListCapabilitiesResp{}, errors.New("unexpected list capabilities")
}

func (c *failingHookRuntimeConn) Reload(context.Context, runtimeapi.ReloadReq) (runtimeapi.ReloadResp, error) {
	return runtimeapi.ReloadResp{}, errors.New("unexpected reload")
}

func (c *failingHookRuntimeConn) SendHookEvent(context.Context, runtimeapi.HookEventReq) error {
	return errors.New("broken pipe")
}

func (c *failingHookRuntimeConn) Close() error {
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	return nil
}

func (c *failingHookRuntimeConn) Done() <-chan struct{} { return c.done }

func TestRuntimeHookSendErrorClosesConnAndBlocksQuickly(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now()}))
	hub.runtimes = NewRuntimeRegistry()
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	timeout := int64(20)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := newFailingHookRuntimeConn()
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	started := time.Now()
	_, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), tenant.DefaultID, "s1")
	if ok {
		t.Fatal("expected broken runtime hook send to fail closed")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("expected disconnect path to avoid hook timeout wait, elapsed=%s", elapsed)
	}
	if !rc.closed {
		t.Fatal("expected hook send failure to close runtime connection")
	}
}

func TestRuntimeToolsAreScopedByTenant(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "alpha-runtime", Backend: "attach"}, {ID: "beta-runtime", Backend: "attach"}},
		map[string]runtimeconfig.Binding{
			"alpha": {AllowedRuntimes: []string{"alpha-runtime"}, DefaultRuntime: "alpha-runtime"},
			"beta":  {AllowedRuntimes: []string{"beta-runtime"}, DefaultRuntime: "beta-runtime"},
		},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))
	alphaRuntime := runtimemock.New()
	betaRuntime := runtimemock.New()
	alphaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "alpha-fs"}
	betaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "beta-fs"}
	alphaCapability := wire.Capability{Target: alphaTarget, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	betaCapability := wire.Capability{Target: betaTarget, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	alphaRuntime.WithCapabilities(alphaCapability)
	betaRuntime.WithCapabilities(betaCapability)
	alphaRuntime.OnInvoke("alpha", alphaTarget, "fs_read").Return([]byte(`"alpha-ok"`))
	betaRuntime.OnInvoke("beta", betaTarget, "fs_read").Return([]byte(`"beta-ok"`))
	if err := hub.runtimes.RegisterHello("alpha-runtime", alphaRuntime, []wire.Capability{alphaCapability}, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello alpha: %v", err)
	}
	if err := hub.runtimes.RegisterHello("beta-runtime", betaRuntime, []wire.Capability{betaCapability}, 0, []string{"beta"}); err != nil {
		t.Fatalf("RegisterHello beta: %v", err)
	}
	hub.syncRuntimeCapability("alpha-runtime", alphaCapability)
	hub.syncRuntimeCapability("beta-runtime", betaCapability)
	if entry, ok := hub.toolExec[toolExecKey("alpha", "fs_read")]; !ok {
		t.Fatalf("missing alpha scoped tool dispatch: %#v", hub.toolExec)
	} else if !toolExecVisible(entry, "alpha") {
		t.Fatalf("alpha scoped tool dispatch not visible to alpha: %#v", entry)
	}

	alphaTools := string(hub.initToolsJSON("alpha"))
	betaTools := string(hub.initToolsJSON("beta"))
	if strings.Count(alphaTools, "fs_read") != 1 || strings.Count(betaTools, "fs_read") != 1 {
		t.Fatalf("expected tenant init tools to expose fs_read once, alpha=%s beta=%s dispatch=%#v", alphaTools, betaTools, hub.toolExec)
	}

	alphaClient := &Client{hub: hub, name: "alpha-client", tenantID: "alpha", session: "s-alpha", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(alphaClient) {
		t.Fatal("add alpha client failed")
	}
	hub.sessions.GetOrCreate("s-alpha", "alpha").AddClient(alphaClient.name)

	hub.tools.handleDynamicTool("alpha", "s-alpha", "tid-alpha", "fs_read", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, alphaClient.recvCh)
	if !isToolResult(msg) || msg.Output != "alpha-ok" {
		t.Fatalf("unexpected alpha result: %+v", msg)
	}
	if len(betaRuntime.RecordedInvokes()) != 0 {
		t.Fatalf("beta runtime should not be invoked for alpha tenant: %+v", betaRuntime.RecordedInvokes())
	}

}

func TestTenantScopedCapabilityUpdateDoesNotEvictSiblingTenantDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	if err := hub.runtimes.RegisterHello("local", runtimemock.New(), nil, 0, []string{"alpha", "beta"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	alpha := wire.Capability{Target: target, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	beta := wire.Capability{Target: target, Tenants: []string{"beta"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}

	hub.syncRuntimeCapability("local", alpha)
	hub.syncRuntimeCapability("local", beta)

	if _, ok := hub.toolExec[toolExecKey("alpha", "fs_read")]; !ok {
		t.Fatalf("alpha dispatch was evicted: %#v", hub.toolExec)
	}
	if _, ok := hub.toolExec[toolExecKey("beta", "fs_read")]; !ok {
		t.Fatalf("beta dispatch missing: %#v", hub.toolExec)
	}
}

func TestTenantScopedCatalogUpdateDoesNotWidenRuntimeTenants(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "bootstrap", CreatedAt: time.Now()},
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
	))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	if err := hub.runtimes.RegisterHello("local", runtimemock.New(), nil, 0, []string{"bootstrap"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	_, ok, err := hub.runtimes.ApplyCatalogUpdate("local", wire.CatalogUpdate{
		Target:   target,
		Tenants:  []string{"alpha"},
		Tools:    []wire.ToolSpec{{Name: "fs_read"}},
		Revision: 1,
		State:    wire.CapabilityStateReady,
		Source:   wire.CapabilitySourceWorker,
	})
	if err != nil {
		t.Fatalf("ApplyCatalogUpdate: %v", err)
	}
	if !ok {
		t.Fatal("expected catalog update to apply")
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	if hub.runtimes.RuntimeAllowedForTenant("alpha", "local") {
		t.Fatalf("runtime unexpectedly learned late tenant: %#v", hub.runtimes.Snapshot())
	}
	alphaTools := string(hub.initToolsJSON("alpha"))
	if strings.Count(alphaTools, "fs_read") != 1 {
		t.Fatalf("expected alpha init tools to expose late runtime tool, tools=%s snapshot=%#v dispatch=%#v", alphaTools, hub.runtimes.Snapshot(), hub.toolExec)
	}
}

func TestInitRefreshesAttachedRuntimeCapabilitiesAfterLateTenantAppears(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "bootstrap", CreatedAt: time.Now()},
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
	))
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	bootstrapTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "bootstrap-fs"}
	alphaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	bootstrapCapability := wire.Capability{Target: bootstrapTarget, Tenants: []string{"bootstrap"}, Tools: []wire.ToolSpec{{Name: "bootstrap_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1}
	conn := runtimemock.New().WithCapabilities(
		bootstrapCapability,
	)
	if err := hub.runtimes.RegisterHello("local", conn, []wire.Capability{bootstrapCapability}, 0, []string{"*"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", bootstrapCapability)

	conn.WithCapabilities(
		wire.Capability{Target: bootstrapTarget, Tenants: []string{"bootstrap"}, Tools: []wire.ToolSpec{{Name: "bootstrap_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1},
		wire.Capability{Target: alphaTarget, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1},
	)

	alphaTools := string(hub.initToolsJSON("alpha"))
	if strings.Count(alphaTools, "fs_read") != 1 {
		t.Fatalf("expected init to refresh late tenant runtime capability, tools=%s snapshot=%#v dispatch=%#v", alphaTools, hub.runtimes.Snapshot(), hub.toolExec)
	}
}

func TestHandleDynamicTool_FallsBackToTenantDefaultRuntimeForGlobalDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"remote"}, DefaultRuntime: "remote"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	remote := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	remote.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"remote"`))
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	hub.toolExec[toolExecKey("alpha", "mcp__echo")] = runtimeDispatch("", "alpha", target, nil, 0)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "remote" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	recorded := remote.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" {
		t.Fatalf("unexpected remote runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicTool_PrefersSessionRuntimeForGlobalDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"local", "remote"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	local := runtimemock.New()
	remote := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	local.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"local"`))
	remote.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"remote"`))
	if err := hub.runtimes.RegisterHello("local", local, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	hub.toolExec[toolExecKey("alpha", "mcp__echo")] = runtimeDispatch("", "alpha", target, nil, 0)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	sess := hub.sessions.GetOrCreate("s1", "alpha")
	sess.AddClient(c.name)
	sess.BindPreferredRuntime("remote")

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "remote" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if len(local.RecordedInvokes()) != 0 {
		t.Fatalf("local default runtime should not be invoked: %+v", local.RecordedInvokes())
	}
	if recorded := remote.RecordedInvokes(); len(recorded) != 1 || recorded[0].TenantID != "alpha" {
		t.Fatalf("unexpected remote runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicTool_FallsBackWhenSessionRuntimeUnavailable(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"local", "remote"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	local := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	local.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"local"`))
	if err := hub.runtimes.RegisterHello("local", local, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	hub.toolExec[toolExecKey("alpha", "mcp__echo")] = runtimeDispatch("", "alpha", target, nil, 0)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	sess := hub.sessions.GetOrCreate("s1", "alpha")
	sess.AddClient(c.name)
	sess.BindPreferredRuntime("remote")

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "local" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if recorded := local.RecordedInvokes(); len(recorded) != 1 || recorded[0].TenantID != "alpha" {
		t.Fatalf("unexpected local runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicTool_ReturnsErrorWithoutImplicitKernelDefaultRuntimeFallback(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	local := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	local.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"local"`))
	if err := hub.runtimes.RegisterHello("local", local, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	hub.toolExec[toolExecKey("alpha", "mcp__echo")] = runtimeDispatch("", "alpha", target, nil, 0)

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	sess := hub.sessions.GetOrCreate("s1", "alpha")
	sess.AddClient(c.name)
	sess.BindPreferredRuntime("remote")

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "default runtime is not configured") {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if len(local.RecordedInvokes()) != 0 {
		t.Fatalf("local runtime should not be used as implicit kernel default: %+v", local.RecordedInvokes())
	}
}

func TestHandleDynamicToolInvokesRuntimeOwningTool(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"local", "remote"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	local := runtimemock.New()
	remote := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	local.OnInvoke("alpha", target, "fs_read").Return([]byte(`"local"`))
	remote.OnInvoke("alpha", target, "fs_read").Return([]byte(`"remote"`))
	if err := hub.runtimes.RegisterHello("local", local, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	hub.syncRuntimeCapability("remote", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "fs_read", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "remote" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if len(local.RecordedInvokes()) != 0 {
		t.Fatalf("local default runtime should not be invoked: %+v", local.RecordedInvokes())
	}
	recorded := remote.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" || !sameRuntimeTarget(recorded[0].Target, target) {
		t.Fatalf("unexpected remote runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicTool_ReturnsTenantForbiddenWhenDefaultRuntimeDisallowed(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"local"}, DefaultRuntime: "remote"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	hub.syncRuntimeCapability("remote", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})
	c := &Client{hub: hub, name: "test", tenantID: "alpha", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("alpha", "s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`), "")
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "cannot use runtime") {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
}

func TestJoinBindsSessionTenantAndRejectsUnknownTenant(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	c := &Client{hub: hub, name: "test", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicSessionInit: true}, sends: map[string]bool{}, state: ClientProtocolReady, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.handleJoin(c, &Message{Type: string(MsgJoin), Session: "s1", TenantID: "missing"})
	msg := waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgError) || msg.Text != "tenant_unknown" {
		t.Fatalf("unexpected unknown tenant response: %+v", msg)
	}
	hub.handleJoin(c, &Message{Type: string(MsgJoin), Session: "s1", TenantID: "alpha"})
	msg = waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgJoined) || msg.TenantID != "alpha" {
		t.Fatalf("unexpected joined response: %+v", msg)
	}
	if got := hub.sessionTenantID("alpha", "s1"); got != "alpha" {
		t.Fatalf("session tenant = %q", got)
	}
}

func TestRuntimeSkillCapabilitiesAreIgnored(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindSkill, ID: "skill:timer"}
	if err := hub.runtimes.RegisterHello("local", rc, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{
		Target:      target,
		Tools:       []wire.ToolSpec{{Name: "timer_start"}},
		State:       wire.CapabilityStateManifestLoaded,
		Source:      wire.CapabilitySourceManifest,
		WorkerMode:  wire.WorkerModeCold,
		HarnessKind: wire.HarnessKindPython,
	})

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		hub.toolExecMu.RLock()
		_, ok := hub.toolExec["timer_start"]
		hub.toolExecMu.RUnlock()
		if ok {
			t.Fatal("skill target capability registered executable tool")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
