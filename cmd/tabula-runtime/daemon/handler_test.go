package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bamanoz/tabula/cmd/tabula-runtime/manifest"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/policy/bare"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/pool"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

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

func TestHandlerAsyncFramesPrimeManifestTargetsOnAttach(t *testing.T) {
	dir := t.TempDir()
	writeRuntimePlugin(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pool := pool.New("main", store, bare.New())
	t.Cleanup(pool.Close)
	h := NewHandler(Options{Store: store, Pool: pool})

	frames := h.AsyncFrames()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case frame := <-frames:
			if update, ok := frame.(wire.CatalogUpdate); ok && update.Target.ID == "fs" && update.State == wire.CapabilityStateReady && len(update.Hooks) == 1 {
				caps, err := h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
				if err != nil {
					t.Fatalf("ListCapabilities: %v", err)
				}
				if len(caps.Targets) != 1 || caps.Targets[0].State != wire.CapabilityStateReady || caps.Targets[0].Source != wire.CapabilitySourceWorker {
					t.Fatalf("capabilities after async prime = %#v", caps)
				}
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("async attach prime did not publish ready catalog update")
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
runtime = "python"
entry = "worker.py"

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
