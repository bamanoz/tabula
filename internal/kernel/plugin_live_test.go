package kernel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

func TestPluginHelloLiveE2E(t *testing.T) {
	root := repoRoot(t)
	manifest, err := plugin.LoadManifest(filepath.Join(root, "examples", "plugin-hello"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}
	if err := hub.RegisterPlugin(manifest, map[string]any{"greeting": "hola"}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	defer stopPluginRun(t, hub, "hello")

	handle := hub.plugins.Get("hello")
	if handle == nil || !handle.IsRegistered() || handle.PID() == 0 {
		t.Fatalf("hello plugin not live: handle=%#v", handle)
	}
	tools := handle.Tools()
	if len(tools) != 2 || tools[0].Name != "hello_ping" || tools[1].Name != "hello_spawn_child" {
		t.Fatalf("registered tools: %+v", tools)
	}
	if _, ok := hub.toolExec["hello_ping"]; !ok {
		t.Fatal("hello_ping not installed in dispatch table")
	}

	toolRecv := addToolResultCaptureClient(t, hub, "live")
	eventRecv := addCaptureClient(t, hub, "hello-event-recorder", "live", []string{"hello_plugin_event"}, nil)

	hub.tools.handleDynamicTool("live", "tc-live", "hello_ping", json.RawMessage(`{"name":"tabula"}`))
	toolResult := waitForMessage(t, toolRecv.recvCh)
	if toolResult.Type != string(MsgToolResult) || toolResult.ID != "tc-live" || toolResult.Output != "hola, tabula" {
		t.Fatalf("tool_result: %+v", toolResult)
	}
	event := waitForMessage(t, eventRecv.recvCh)
	if event.Type != "hello_plugin_event" || event.Session != "live" || !strings.Contains(string(event.Payload), `"name":"tabula"`) {
		t.Fatalf("plugin send event: %+v payload=%s", event, string(event.Payload))
	}

	rewritten, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"hello_ping","input":{"name":"rewrite"}}`), "live")
	if !ok || !strings.Contains(string(rewritten), `"rewritten"`) {
		t.Fatalf("expected rewrite hook success, ok=%v payload=%s", ok, string(rewritten))
	}
	_, ok = hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"hello_ping","input":{"name":"blocked"}}`), "live")
	if ok {
		t.Fatal("expected hello plugin to deny blocked tool call")
	}

	snapshot := string(hub.SnapshotPlugins())
	if !strings.Contains(snapshot, `"id":"hello"`) || !strings.Contains(snapshot, `"status":"running"`) || !strings.Contains(snapshot, `"hello_ping"`) {
		t.Fatalf("plugin snapshot missing hello diagnostics: %s", snapshot)
	}
}

func TestPluginHelloCleansUpChildProcessOnShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reference child-PG cleanup smoke uses POSIX sleep/process signalling")
	}
	root := repoRoot(t)
	manifest, err := plugin.LoadManifest(filepath.Join(root, "examples", "plugin-hello"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}
	if err := hub.RegisterPlugin(manifest, map[string]any{"greeting": "hola"}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	toolRecv := addToolResultCaptureClient(t, hub, "child-cleanup")
	hub.tools.handleDynamicTool("child-cleanup", "tc-child", "hello_spawn_child", json.RawMessage(`{"seconds":60}`))
	toolResult := waitForMessage(t, toolRecv.recvCh)
	pid, err := strconv.Atoi(strings.TrimSpace(toolResult.Output))
	if err != nil || pid <= 0 {
		t.Fatalf("expected child pid result, output=%q err=%v", toolResult.Output, err)
	}
	if !processExists(pid) {
		t.Fatalf("expected spawned child pid %d to be alive before shutdown", pid)
	}

	gracefulStopPluginRun(t, hub, "hello")
	waitForProcessExit(t, pid)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not locate repo root from %s", wd)
		}
		wd = parent
	}
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("child process pid %d still alive after plugin shutdown", pid)
}

func gracefulStopPluginRun(t *testing.T, hub *Hub, id string) {
	t.Helper()
	handle := hub.plugins.Get(id)
	if handle == nil {
		stopPluginRun(t, hub, id)
		return
	}
	if err := handle.SendShutdown(); err != nil {
		t.Fatalf("SendShutdown(%s): %v", id, err)
	}
	select {
	case <-handle.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for plugin %s to stop gracefully", id)
	}
	stopPluginRun(t, hub, id)
}
