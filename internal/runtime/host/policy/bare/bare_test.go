package bare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestBarePolicySpawnInitCallAndShutdown(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !worker.IsAlive() {
		t.Fatal("worker should be alive after spawn")
	}
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "call-1", Tool: "read_file", Args: json.RawMessage(`{"path":"x"}`), TurnCorrelationID: "tc-1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.OK || result.CallID != "call-1" || !strings.Contains(string(result.Data), `"tenant_id": "tenant-a"`) || !strings.Contains(string(result.Data), `"turn_correlation_id": "tc-1"`) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	info, err := worker.Wait()
	if err != nil || info.Code != 0 {
		t.Fatalf("Wait = %#v, %v", info, err)
	}
}

func TestWarmWorkerAcceptsMultipleSequentialCalls(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, callID := range []string{"call-1", "call-2"} {
		result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: callID, Tool: "echo"})
		if err != nil {
			t.Fatalf("Call(%s): %v", callID, err)
		}
		if !result.OK || result.CallID != callID {
			t.Fatalf("Call(%s) = %#v", callID, result)
		}
	}
}

func TestWarmWorkerAcceptsConcurrentCallsWithOutOfOrderResults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", filepath.Join(dir, "home"))
	script := filepath.Join(dir, "worker.py")
	writeFile(t, script, concurrentWorkerScript)
	worker, err := New().Spawn(context.Background(), policy.SpawnReq{
		KernelID:   "main",
		TenantID:   "tenant-a",
		TargetID:   "fs",
		Command:    []string{"./worker.py"},
		Manifest:   json.RawMessage(`{"id":"fs"}`),
		WorkingDir: dir,
		Mode:       policy.SpawnModeWarm,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: "main", TenantID: "tenant-a", TargetID: "fs", Manifest: json.RawMessage(`{"id":"fs"}`)}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	asyncLogs := make(chan string, 8)
	stopLogs := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopLogs:
				return
			case event, ok := <-worker.Events():
				if !ok {
					return
				}
				if log, ok := event.Frame.(*workerwire.WorkerLog); ok {
					asyncLogs <- log.Message
				}
			}
		}
	}()
	defer close(stopLogs)

	results := make(chan workerwire.WorkerResult, 2)
	errs := make(chan string, 2)
	var wg sync.WaitGroup
	for _, call := range []workerwire.WorkerCall{{CallID: "slow", Tool: "slow"}, {CallID: "fast", Tool: "fast"}} {
		wg.Add(1)
		go func(call workerwire.WorkerCall) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := worker.Call(ctx, call)
			if err != nil {
				errs <- call.CallID + ": " + err.Error()
				return
			}
			results <- result
		}(call)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		seenLogs := []string{}
		for {
			select {
			case msg := <-asyncLogs:
				seenLogs = append(seenLogs, msg)
			default:
				t.Fatalf("Call error: %s (async logs: %v)", err, seenLogs)
			}
		}
	}
	seen := map[string]string{}
	for result := range results {
		if !result.OK {
			t.Fatalf("unexpected result: %#v", result)
		}
		var data map[string]string
		if err := json.Unmarshal(result.Data, &data); err != nil {
			t.Fatalf("decode result %s: %v", result.Data, err)
		}
		seen[result.CallID] = data["tool"]
	}
	if len(seen) != 2 || seen["fast"] != "fast" || seen["slow"] != "slow" {
		t.Fatalf("unexpected concurrent results: %#v", seen)
	}
}

func TestColdWorkerRejectsSecondCall(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeCold)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "call-1", Tool: "echo"}); err != nil {
		t.Fatalf("first Call: %v", err)
	}
	if _, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "call-2", Tool: "echo"}); err == nil || !strings.Contains(err.Error(), "cold worker") {
		t.Fatalf("expected cold-worker reuse error, got %v", err)
	}
}

func TestSpawnInjectsTenantKernelAndTargetEnv(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "env", Tool: "env"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var data map[string]string
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode data %s: %v", result.Data, err)
	}
	if data["tenant_id"] != req.TenantID || data["kernel_id"] != req.KernelID || data["target_id"] != req.TargetID {
		t.Fatalf("env data mismatch: %#v", data)
	}
	if data["tenant_dir"] != filepath.Join(os.Getenv("TABULA_HOME"), "tenants", req.TenantID) {
		t.Fatalf("tenant_dir mismatch: %#v", data)
	}
}

func TestBarePolicySpawnSupportsCanonicalWorkerCommand(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("test uses POSIX executable path")
	}
	req := testSpawnReq(t, policy.SpawnModeWarm)
	req.Command = []string{"./worker.py"}
	req.Runtime = ""
	req.Entry = ""
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "call-1", Tool: "echo"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.OK || result.CallID != "call-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestWorkerEnvPreservesGenericPathAndTabulaEnv(t *testing.T) {
	t.Setenv("TABULA_HOME", "/tmp/tabula-home")
	t.Setenv("TABULA_URL", "ws://127.0.0.1:9999/ws")
	t.Setenv("PATH", "/custom/bin")
	t.Setenv("PYTHONPATH", "/custom/packages")
	env := workerEnv(policy.SpawnReq{KernelID: "main", TenantID: "default", TargetID: "plugin"})
	if got := envValue(env, "PATH"); got != "/custom/bin" {
		t.Fatalf("PATH = %q, want passthrough PATH", got)
	}
	if got := envValue(env, "PYTHONPATH"); got != "/custom/packages" {
		t.Fatalf("PYTHONPATH = %q, want passthrough PYTHONPATH", got)
	}
	if got := envValue(env, "TABULA_URL"); got != "ws://127.0.0.1:9999/ws" {
		t.Fatalf("TABULA_URL = %q", got)
	}
	if got := envValue(env, "TABULA_BOOT_PATH"); got != "" {
		t.Fatalf("TABULA_BOOT_PATH leaked into worker env: %q", got)
	}
}

func TestRuntimeCommandUsesConfiguredRuntimeCommand(t *testing.T) {
	policy := &Policy{RuntimeCommands: map[string]string{"custom": "/bin/custom"}}
	if got := policy.runtimeCommand("custom"); got != "/bin/custom" {
		t.Fatalf("runtimeCommand(custom) = %q", got)
	}
	if got := policy.runtimeCommand("missing"); got != "" {
		t.Fatalf("runtimeCommand(missing) = %q, want empty", got)
	}
}

func TestCanonicalWorkerCommandUsesPathAsDeclared(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	req.Command = []string{"worker-binary", "--flag"}
	req.Runtime = ""
	req.Entry = ""
	cmd, err := New().buildCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if cmd.Path != "worker-binary" {
		t.Fatalf("cmd.Path = %q, want worker-binary", cmd.Path)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "worker-binary" || cmd.Args[1] != "--flag" {
		t.Fatalf("cmd.Args = %#v", cmd.Args)
	}
}

func TestWorkerRoutesAsyncFramesWithoutBreakingCallResult(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: "dynamic", Tool: "dynamic"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.OK || result.CallID != "dynamic" {
		t.Fatalf("unexpected result: %#v", result)
	}
	var sawLog, sawTools bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && (!sawLog || !sawTools) {
		select {
		case event := <-worker.Events():
			if event.Err != nil {
				t.Fatalf("unexpected async error: %v", event.Err)
			}
			switch frame := event.Frame.(type) {
			case *workerwire.WorkerLog:
				sawLog = frame.Message == "dynamic tool updated"
			case *workerwire.WorkerToolsUpdated:
				sawTools = frame.Revision == 2 && len(frame.Tools) == 2
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !sawLog || !sawTools {
		t.Fatalf("expected async log/tools update, sawLog=%v sawTools=%v", sawLog, sawTools)
	}
}

func TestWorkerRoutesHookEventAndReply(t *testing.T) {
	req := testSpawnReq(t, policy.SpawnModeWarm)
	worker, err := New().Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = worker.Shutdown(context.Background()) }()
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: req.KernelID, TenantID: req.TenantID, TargetID: req.TargetID, Manifest: req.Manifest}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reply, err := worker.HookEvent(context.Background(), workerwire.WorkerEvent{CallID: "hook-1", Event: "before_tool_call", ReplyMode: workerwire.ReplyModeModifying, Data: json.RawMessage(`{"tool":"echo"}`)})
	if err != nil {
		t.Fatalf("HookEvent: %v", err)
	}
	if reply == nil || reply.CallID != "hook-1" || reply.Action != "rewrite" {
		t.Fatalf("unexpected reply: %#v", reply)
	}
}

func TestInitTimeoutKillsUnresponsiveWorker(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.sh")
	writeFile(t, script, "#!/bin/sh\nsleep 30\n")
	worker, err := (&Policy{InitTimeout: 50 * time.Millisecond}).Spawn(context.Background(), policy.SpawnReq{
		KernelID:   "main",
		TenantID:   "default",
		TargetID:   "fs",
		Runtime:    "bash",
		Entry:      "worker.sh",
		WorkingDir: dir,
		Mode:       policy.SpawnModeWarm,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: "main", TenantID: "default", TargetID: "fs"}); err == nil {
		t.Fatal("expected init timeout")
	}
	_, _ = worker.Wait()
}

func TestInitErrorIncludesRedactedWorkerStderrHint(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.py")
	writeFile(t, script, `#!/usr/bin/env python3
import sys

_ = sys.stdin.readline()
print("RuntimeError: expected register_request as first plugin message", file=sys.stderr)
print("secret_token=super-secret", file=sys.stderr)
sys.stderr.flush()
sys.exit(1)
`)
	worker, err := New().Spawn(context.Background(), policy.SpawnReq{
		KernelID:   "main",
		TenantID:   "default",
		TargetID:   "legacy-plugin",
		Command:    []string{"./worker.py"},
		WorkingDir: dir,
		Mode:       policy.SpawnModeWarm,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: "main", TenantID: "default", TargetID: "legacy-plugin"}); err == nil {
		t.Fatal("expected init error")
	} else {
		got := err.Error()
		if !strings.Contains(got, "worker stderr captured: 2 lines") {
			t.Fatalf("expected bounded stderr metadata in init error, got %q", got)
		}
		if !strings.Contains(got, "likely legacy register_request/stdio plugin SDK") {
			t.Fatalf("expected legacy-sdk hint in init error, got %q", got)
		}
		if strings.Contains(got, "super-secret") || strings.Contains(got, "RuntimeError: expected register_request as first plugin message") {
			t.Fatalf("expected raw worker stderr to be redacted, got %q", got)
		}
	}
	_, _ = worker.Wait()
}

func TestSpawnValidatesRequiredFields(t *testing.T) {
	_, err := New().Spawn(context.Background(), policy.SpawnReq{})
	if err == nil || !strings.Contains(err.Error(), "kernel id is required") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func testSpawnReq(t *testing.T, mode policy.SpawnMode) policy.SpawnReq {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", filepath.Join(dir, "home"))
	script := filepath.Join(dir, "worker.py")
	writeFile(t, script, testWorkerScript)
	return policy.SpawnReq{
		KernelID:   "main",
		TenantID:   "tenant-a",
		TargetID:   "fs",
		Command:    []string{"./worker.py"},
		Manifest:   json.RawMessage(`{"id":"fs"}`),
		WorkingDir: dir,
		Mode:       mode,
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const testWorkerScript = `#!/usr/bin/env python3
import json
import os
import sys

line = sys.stdin.readline()
if not line:
    sys.exit(2)
json.loads(line)
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [], "subscriptions": []}) + "\n")
sys.stdout.flush()

for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    if frame.get("op") == "event":
        sys.stdout.write(json.dumps({"op": "event_reply", "call_id": frame.get("call_id"), "action": "rewrite", "data": {"tool": "safe_echo"}, "reason": "rewritten"}) + "\n")
        sys.stdout.flush()
        continue
    call_id = frame.get("call_id", "")
    if frame.get("tool") == "dynamic":
        sys.stdout.write(json.dumps({"op": "log", "level": "info", "msg": "dynamic tool updated"}) + "\n")
        sys.stdout.write(json.dumps({"op": "tools_updated", "revision": 2, "tools": [{"name": "dynamic"}, {"name": "echo"}], "removed": ["slow"]}) + "\n")
        sys.stdout.flush()
    data = {
        "tool": frame.get("tool", ""),
        "tenant_id": os.environ.get("TABULA_TENANT_ID", ""),
        "tenant_dir": os.environ.get("TABULA_TENANT_DIR", ""),
        "kernel_id": os.environ.get("TABULA_KERNEL_ID", ""),
        "target_id": os.environ.get("TABULA_TARGET_ID", ""),
        "turn_correlation_id": frame.get("turn_correlation_id", ""),
    }
    sys.stdout.write(json.dumps({"op": "result", "call_id": call_id, "ok": True, "data": data}) + "\n")
    sys.stdout.flush()
`

const concurrentWorkerScript = `#!/usr/bin/env python3
import json
import sys
import threading
import time

line = sys.stdin.readline()
if not line:
    sys.exit(2)
json.loads(line)
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [], "subscriptions": []}) + "\n")
sys.stdout.flush()
out_lock = threading.Lock()



def handle(frame):
    with out_lock:
        sys.stdout.write(json.dumps({"op": "log", "level": "info", "msg": "received " + frame.get("tool")}) + "\n")
        sys.stdout.flush()
    if frame.get("tool") == "slow":
        time.sleep(0.2)
    else:
        time.sleep(0.02)
    with out_lock:
        sys.stdout.write(json.dumps({"op": "log", "level": "info", "msg": "sent " + frame.get("tool")}) + "\n")
        sys.stdout.flush()
        sys.stdout.write(json.dumps({"op": "result", "call_id": frame.get("call_id"), "ok": True, "data": {"tool": frame.get("tool")}}) + "\n")
        sys.stdout.flush()

threads = []
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        break
    t = threading.Thread(target=handle, args=(frame,))
    t.start()
    threads.append(t)

for t in threads:
    t.join()
`
