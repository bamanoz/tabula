package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/driver"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestHandlerTurnPermitMissingWorkerReturnsRejectedResult(t *testing.T) {
	supervisor := driver.New("kernel", nil, nil, nil)
	t.Cleanup(func() {
		if err := supervisor.Close(); err != nil {
			t.Fatalf("close driver supervisor: %v", err)
		}
	})
	h := NewHandler(Options{Driver: supervisor})
	result, err := h.TurnPermit(context.Background(), wire.TurnPermit{
		Op:        wire.OpTurnPermit,
		RequestID: "permit-1",
		AttemptRef: wire.AttemptRef{
			TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1",
			Fence: wire.DriverFence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1},
		},
		PermitID:       "permit-1",
		SessionVersion: 1,
		Cursor:         1,
	})
	if err != nil {
		t.Fatalf("TurnPermit: %v", err)
	}
	if result.Op != wire.OpDriverResult || result.RequestID != "permit-1" || result.Accepted {
		t.Fatalf("result = %+v", result)
	}
	if result.Error == nil || result.Error.Code != wire.ErrorRuntimeUnavailable || !result.Error.Retryable {
		t.Fatalf("error = %+v", result.Error)
	}
}

func TestHandlerAdminOpsAndInvokePlaceholder(t *testing.T) {
	h := NewHandler()
	health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth})
	if err != nil || !health.OK || health.WorkerCount != 0 {
		t.Fatalf("Health = %#v, %v", health, err)
	}
	caps, err := h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil || len(caps.Targets) != 0 {
		t.Fatalf("ListCapabilities = %#v, %v", caps, err)
	}
	reload, err := h.Reload(context.Background(), wire.Reload{Op: wire.OpReload})
	if err != nil || len(reload.EvictedTargets) != 0 {
		t.Fatalf("Reload = %#v, %v", reload, err)
	}
	cancel, err := h.Cancel(context.Background(), wire.Cancel{Op: wire.OpCancel, CallID: "call-1"})
	if err != nil || cancel.Op != wire.OpCancelAck || cancel.CallID != "call-1" {
		t.Fatalf("Cancel = %#v, %v", cancel, err)
	}
	invoke, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "call-1", TenantID: "default", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if invoke.OK || invoke.Error == nil || invoke.Error.Code != wire.ErrorInternal || invoke.Error.Message != "worker pool not configured" {
		t.Fatalf("unexpected invoke placeholder: %#v", invoke)
	}
}

func TestHandlerInvokesManifestBackedWorkerAndReloads(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})

	caps, err := h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil || len(caps.Targets) != 1 || caps.Targets[0].Target.ID != "fs" || caps.Targets[0].State != wire.CapabilityStateManifestLoaded {
		t.Fatalf("ListCapabilities = %#v, %v", caps, err)
	}
	resp, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "call-1", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file", Args: json.RawMessage(`{"path":"README.md"}`)})
	if err != nil || !resp.OK {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	caps, err = h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil || len(caps.Targets) != 1 || caps.Targets[0].State != wire.CapabilityStateReady || caps.Targets[0].Source != wire.CapabilitySourceWorker {
		t.Fatalf("ListCapabilities after invoke = %#v, %v", caps, err)
	}
	if len(caps.Targets[0].Tools) != 2 || caps.Targets[0].Tools[0].Name != "dynamic_extra" || caps.Targets[0].Tools[1].Name != "read_file" {
		t.Fatalf("worker-backed tools = %#v", caps.Targets[0].Tools)
	}
	if len(caps.Targets[0].Hooks) != 1 || caps.Targets[0].Hooks[0].Event != "before_tool_call" {
		t.Fatalf("worker-backed hooks = %#v", caps.Targets[0].Hooks)
	}
	if health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth}); err != nil || health.WorkerCount != 1 {
		t.Fatalf("Health after invoke = %#v, %v", health, err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	reload, err := h.Reload(context.Background(), wire.Reload{Op: wire.OpReload, Target: &target})
	if err != nil || len(reload.EvictedTargets) != 1 || reload.EvictedTargets[0].ID != "fs" {
		t.Fatalf("Reload = %#v, %v", reload, err)
	}
	caps, err = h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil || len(caps.Targets) != 1 || caps.Targets[0].State != wire.CapabilityStateManifestLoaded || caps.Targets[0].Source != wire.CapabilitySourceManifest {
		t.Fatalf("ListCapabilities after reload = %#v, %v", caps, err)
	}
}

func TestHandlerReloadScopesTenantCatalog(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	writeRuntimePlugin(t, alphaDir)
	writeRuntimePlugin(t, betaDir)
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"alpha": {PluginDirs: []string{alphaDir}},
		"beta":  {PluginDirs: []string{betaDir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	p := pool.New("main", store, bare.New(), pool.Options{AllowedTenants: []string{"alpha", "beta"}})
	t.Cleanup(p.Close)
	h := NewHandler(Options{Store: store, Pool: p})

	for _, tenantID := range []string{"alpha", "beta"} {
		resp, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: tenantID, TenantID: tenantID, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file", Args: json.RawMessage(`{"path":"README.md"}`)})
		if err != nil || !resp.OK {
			t.Fatalf("Invoke %s = %#v, %v", tenantID, resp, err)
		}
	}
	if health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth}); err != nil || health.WorkerCount != 2 {
		t.Fatalf("Health before reload = %#v, %v", health, err)
	}
	reload, err := h.Reload(context.Background(), wire.Reload{Op: wire.OpReload, Tenants: []string{"alpha"}})
	if err != nil || len(reload.EvictedTargets) != 1 || reload.EvictedTargets[0].ID != "fs" {
		t.Fatalf("Reload = %#v, %v", reload, err)
	}
	if health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth}); err != nil || health.WorkerCount != 1 {
		t.Fatalf("Health after tenant reload = %#v, %v", health, err)
	}
}

func TestHandlerTenantReloadDiscoversAndPrimesAppCatalog(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, filepath.Join(dir, "tenants", "alpha", "plugins", "fs"))
	store, err := manifest.NewSearchStore(nil, nil)
	if err != nil {
		t.Fatalf("NewSearchStore: %v", err)
	}
	store.SetTabulaHome(dir)
	p := pool.New("main", store, bare.New(), pool.Options{AllowedTenants: []string{"alpha"}, TabulaHome: dir})
	t.Cleanup(p.Close)
	h := NewHandler(Options{Store: store, Pool: p})

	reload, err := h.Reload(context.Background(), wire.Reload{Op: wire.OpReload, Tenants: []string{"alpha"}})
	if err != nil || len(reload.EvictedTargets) != 0 {
		t.Fatalf("Reload = %#v, %v", reload, err)
	}

	frames := h.AsyncFrames()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-frames:
			if update, ok := frame.(wire.CatalogUpdate); ok && update.Target.ID == "fs" && len(update.Tenants) == 1 && update.Tenants[0] == "alpha" && update.State == wire.CapabilityStateReady {
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("tenant reload did not publish ready catalog update")
}

func TestHandlerHookEventMissingTargetDoesNotDropRuntimeConnection(t *testing.T) {
	store, err := manifest.NewSearchStore(nil, nil)
	if err != nil {
		t.Fatalf("NewSearchStore: %v", err)
	}
	p := pool.New("main", store, bare.New(), pool.Options{AllowedTenants: []string{"alpha"}})
	t.Cleanup(p.Close)
	h := NewHandler(Options{Store: store, Pool: p})

	reply, err := h.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, TenantID: "alpha", CallID: "missing", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying})
	if err != nil {
		t.Fatalf("HookEvent: %v", err)
	}
	if reply.CallID != "missing" || reply.Action != wire.HookActionOK {
		t.Fatalf("reply = %#v", reply)
	}
}

func TestHandlerRoutesHookEventToWorker(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})

	reply, err := h.HookEvent(context.Background(), wire.HookEvent{Op: wire.OpHookEvent, CallID: "hook-1", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"read_file"}`)})
	if err != nil {
		t.Fatalf("HookEvent: %v", err)
	}
	if reply.CallID != "hook-1" || reply.Action != wire.HookActionRewrite {
		t.Fatalf("unexpected hook reply: %#v", reply)
	}
}

func TestHandlerPrepareTenantReturnsAuthoritativeWorkerCatalog(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workerPool := pool.New("kernel", store, bare.New(), pool.Options{AllowedTenants: []string{"tenant-a"}})
	t.Cleanup(workerPool.Close)
	h := NewHandler(Options{Store: store, Pool: workerPool})

	ack, err := h.PrepareTenant(context.Background(), wire.PrepareTenant{Op: wire.OpPrepareTenant, RequestID: "prepare-1", TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("PrepareTenant: %v", err)
	}
	if ack.RequestID != "prepare-1" || len(ack.Capabilities) != 1 {
		t.Fatalf("ack = %#v", ack)
	}
	capability := ack.Capabilities[0]
	if capability.Target.ID != "fs" || capability.State != wire.CapabilityStateReady || capability.Source != wire.CapabilitySourceWorker {
		t.Fatalf("capability = %#v", capability)
	}
	toolNames := map[string]bool{}
	for _, tool := range capability.Tools {
		toolNames[tool.Name] = true
	}
	if len(capability.Tools) != 2 || !toolNames["read_file"] || !toolNames["dynamic_extra"] {
		t.Fatalf("tools = %#v", capability.Tools)
	}
}

func TestHandlerAsyncFramesOnlyPrimeDynamicTargetsOnAttach(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	writeDynamicRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})

	frames := h.AsyncFrames()
	if frames == nil {
		t.Fatal("AsyncFrames returned nil")
	}
	if again := h.AsyncFrames(); again != frames {
		t.Fatal("AsyncFrames returned competing channel on second call")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth}); err != nil {
			t.Fatalf("Health after async attach = %#v, %v", health, err)
		} else if health.WorkerCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if health, err := h.Health(context.Background(), wire.Health{Op: wire.OpHealth}); err != nil || health.WorkerCount != 1 {
		t.Fatalf("Health after async attach = %#v, %v", health, err)
	}
	caps, err := h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil || len(caps.Targets) != 2 {
		t.Fatalf("ListCapabilities after async attach = %#v, %v", caps, err)
	}
	byTarget := map[string]wire.Capability{}
	for _, capability := range caps.Targets {
		byTarget[capability.Target.ID] = capability
	}
	if byTarget["fs"].State != wire.CapabilityStateManifestLoaded || byTarget["fs"].Source != wire.CapabilitySourceManifest {
		t.Fatalf("manifest target should stay lazy after async attach: %#v", byTarget["fs"])
	}
	if byTarget["dynamic"].State != wire.CapabilityStateReady || byTarget["dynamic"].Source != wire.CapabilitySourceWorker {
		t.Fatalf("dynamic target should be ready after async attach: %#v", byTarget["dynamic"])
	}
}

func TestHandlerCancelAbandonsInvoke(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})
	done := make(chan wire.InvokeResult, 1)
	go func() {
		resp, _ := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "slow", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "slow"})
		done <- resp
	}()
	time.Sleep(50 * time.Millisecond)
	ack, err := h.Cancel(context.Background(), wire.Cancel{Op: wire.OpCancel, CallID: "slow"})
	if err != nil || ack.CallID != "slow" {
		t.Fatalf("Cancel = %#v, %v", ack, err)
	}
	select {
	case resp := <-done:
		if resp.Error == nil || resp.Error.Code != wire.ErrorCancelled {
			t.Fatalf("cancelled invoke = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled invoke")
	}
	resp, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "after-cancel", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file"})
	if err != nil || !resp.OK {
		t.Fatalf("invoke after cancel = %#v, %v", resp, err)
	}
}

func TestHandlerTimeoutReturnsStructuredResult(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})
	resp, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "slow-timeout", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "slow", TimeoutMS: 20})
	if err != nil {
		t.Fatalf("Invoke timeout = %#v, %v", resp, err)
	}
	if resp.Error == nil || resp.Error.Code != wire.ErrorTimeout {
		t.Fatalf("expected timeout result, got %#v", resp)
	}
	resp, err = h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "after-timeout", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file"})
	if err != nil || !resp.OK {
		t.Fatalf("invoke after timeout = %#v, %v", resp, err)
	}
}

func writeRuntimePlugin(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "fs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(`id = "fs"
name = "Filesystem"
version = "0.1.0"
[worker]
command = ["python3", "worker.py"]
mode = "warm"

[[tools]]
name = "read_file"

[[tools]]
name = "slow"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker.py"), []byte(`#!/usr/bin/env python3
import json
import sys
import time

sys.stdin.readline()
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [{"name": "read_file"}, {"name": "dynamic_extra"}], "subscriptions": [{"event": "before_tool_call", "priority": 100}]}) + "\n")
sys.stdout.flush()
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    if frame.get("op") == "event":
        sys.stdout.write(json.dumps({"op": "event_reply", "call_id": frame.get("call_id"), "action": "rewrite", "data": {"tool": "safe_read_file"}, "reason": "rewritten"}) + "\n")
        sys.stdout.flush()
        continue
    if frame.get("tool") == "slow":
        time.sleep(5)
    sys.stdout.write(json.dumps({"op": "result", "call_id": frame.get("call_id"), "ok": True, "data": {"tool": frame.get("tool")}}) + "\n")
    sys.stdout.flush()
`), 0o755); err != nil {
		t.Fatalf("write worker: %v", err)
	}
}

func writeDynamicRuntimePlugin(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "dynamic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir dynamic plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(`id = "dynamic"
name = "Dynamic"
version = "0.1.0"
[worker]
command = ["python3", "run.py"]
mode = "warm"
[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`), 0o644); err != nil {
		t.Fatalf("write dynamic manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte(`#!/usr/bin/env python3
import json
import sys

json.loads(sys.stdin.readline())
sys.stdout.write(json.dumps({"op":"init_ack","ready":True,"tools":[{"name":"dynamic_ping"}],"subscriptions":[]}) + "\n")
sys.stdout.flush()
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        break
`), 0o755); err != nil {
		t.Fatalf("write dynamic worker: %v", err)
	}
}
