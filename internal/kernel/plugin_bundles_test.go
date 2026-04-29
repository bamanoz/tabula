package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

func TestTabulaBundlesHookPluginsLiveE2E(t *testing.T) {
	root := filepath.Clean(filepath.Join(repoRoot(t), "..", "tabula-bundles"))
	if _, err := os.Stat(root); err != nil {
		t.Skipf("tabula-bundles checkout not available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("TABULA_PROJECT_ROOT", repoRoot(t))
	t.Setenv("PYTHONPATH", filepath.Join(root, "_lib", "python", "src")+string(os.PathListSeparator)+repoRoot(t))

	writeJSONFile(t, filepath.Join(home, "config", "plugins", "hook-permissions", "permissions.json"), map[string]any{
		"rules": []map[string]string{{"tool": "danger_tool", "effect": "deny"}},
	})
	writeJSONFile(t, filepath.Join(home, "config", "skills", "hook-approvals", "rules.json"), map[string]any{
		"rules": []map[string]string{{"tool": "rewrite_tool", "effect": "allow_always"}},
	})

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}

	plugins := []struct {
		id   string
		path string
	}{
		{"hook-permissions", filepath.Join(root, "base", "hook-permissions")},
		{"hook-workspace-boundary", filepath.Join(root, "coder-workspace", "hook-workspace-boundary")},
		{"hook-approvals", filepath.Join(root, "coder-workspace", "hook-approvals")},
		{"hook-caveman", filepath.Join(root, "caveman", "hook-caveman")},
		{"hook-logger", filepath.Join(root, "base", "hook-logger")},
	}
	for _, item := range plugins {
		manifest, err := plugin.LoadManifest(item.path)
		if err != nil {
			t.Fatalf("LoadManifest(%s): %v", item.id, err)
		}
		if err := hub.RegisterPlugin(manifest, nil); err != nil {
			t.Fatalf("RegisterPlugin(%s): %v", item.id, err)
		}
		defer stopPluginRun(t, hub, item.id)
	}

	if _, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"danger_tool","input":{}}`), "live"); ok {
		t.Fatal("hook-permissions should deny danger_tool")
	}

	outside := filepath.Join(filepath.Dir(repoRoot(t)), "outside.txt")
	if _, ok := hub.dispatchHook("before_tool_call", mustJSON(t, map[string]any{"tool": "read", "input": map[string]any{"path": outside}}), "live"); ok {
		t.Fatal("hook-workspace-boundary should deny path outside project root")
	}

	rewritten, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"rewrite_tool","id":"t1","input":{}}`), "live")
	if !ok || !strings.Contains(string(rewritten), `"approved":true`) {
		t.Fatalf("hook-approvals should rewrite approved payload, ok=%v payload=%s", ok, string(rewritten))
	}

	sessionPayload, ok := hub.dispatchHook("session_start", json.RawMessage(`{"session":"live"}`), "live")
	if !ok || !strings.Contains(string(sessionPayload), "CAVEMAN MODE ACTIVE") {
		t.Fatalf("hook-caveman should inject context, ok=%v payload=%s", ok, string(sessionPayload))
	}

	logPath := filepath.Join(home, "logs", "hook-logger", "hooks.jsonl")
	if _, ok := hub.dispatchHook("after_message", json.RawMessage(`{"session":"live","text":"hello"}`), "live"); !ok {
		t.Fatal("after_message hook dispatch should pass")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "after_message") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hook-logger did not write after_message log, last err=%v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTabulaBundlesMCPPluginLiveE2E(t *testing.T) {
	root := filepath.Clean(filepath.Join(repoRoot(t), "..", "tabula-bundles"))
	if _, err := os.Stat(root); err != nil {
		t.Skipf("tabula-bundles checkout not available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("PYTHONPATH", filepath.Join(root, "_lib", "python", "src")+string(os.PathListSeparator)+repoRoot(t))

	fake := filepath.Join(home, "fake_mcp_server.py")
	if err := os.WriteFile(fake, []byte(fakeMCPServerPython), 0o755); err != nil {
		t.Fatalf("write fake mcp server: %v", err)
	}
	writeJSONFile(t, filepath.Join(home, "config", "plugins", "mcp", "servers.json"), map[string]any{
		"servers": map[string]any{
			"fake": map[string]any{"transport": "stdio", "command": []string{"python3", fake}},
		},
	})

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}

	manifest, err := plugin.LoadManifest(filepath.Join(root, "base", "mcp"))
	if err != nil {
		t.Fatalf("LoadManifest(mcp): %v", err)
	}
	if err := hub.RegisterPlugin(manifest, nil); err != nil {
		t.Fatalf("RegisterPlugin(mcp): %v", err)
	}
	defer stopPluginRun(t, hub, "mcp")

	waitForToolDispatch(t, hub, "mcp__fake__echo")
	for _, want := range []string{"mcp_list_servers", "mcp_discover", "mcp_list_tools", "mcp_call", "mcp__fake__echo"} {
		if _, ok := hub.toolExec[want]; !ok {
			t.Fatalf("expected MCP tool %s in dispatch", want)
		}
	}

	recv := addToolResultCaptureClient(t, hub, "mcp-live")
	hub.tools.handleDynamicTool("mcp-live", "tc-list", "mcp_list_servers", json.RawMessage(`{}`))
	msg := waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"fake"`) {
		t.Fatalf("mcp_list_servers result: %+v", msg)
	}

	hub.tools.handleDynamicTool("mcp-live", "tc-echo", "mcp__fake__echo", json.RawMessage(`{"message":"hello"}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, "hello") {
		t.Fatalf("mcp__fake__echo result: %+v", msg)
	}

	hub.tools.handleDynamicTool("mcp-live", "tc-call", "mcp_call", json.RawMessage(`{"server":"fake","tool":"echo","args":{"message":"via generic"}}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, "via generic") {
		t.Fatalf("mcp_call result: %+v", msg)
	}

	hub.tools.handleDynamicTool("mcp-live", "tc-discover", "mcp_discover", json.RawMessage(`{}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"fake"`) {
		t.Fatalf("mcp_discover result: %+v", msg)
	}
}

func TestTabulaBundlesSubagentsPluginLimitsLiveE2E(t *testing.T) {
	root := filepath.Clean(filepath.Join(repoRoot(t), "..", "tabula-bundles"))
	if _, err := os.Stat(root); err != nil {
		t.Skipf("tabula-bundles checkout not available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("PYTHONPATH", filepath.Join(root, "_lib", "python", "src")+string(os.PathListSeparator)+repoRoot(t))
	runner := filepath.Join(home, "clients", "subagent", "run.py")
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatalf("mkdir runner: %v", err)
	}
	clientManifest := filepath.Join(filepath.Dir(runner), "client.toml")
	if err := os.WriteFile(clientManifest, []byte(`id = "subagent"
name = "Subagent Runner"
runtime = "python"
entry = "run.py"
`), 0o644); err != nil {
		t.Fatalf("write fake subagent client manifest: %v", err)
	}
	if err := os.WriteFile(runner, []byte(fakeSubagentRunnerPython), 0o755); err != nil {
		t.Fatalf("write fake subagent runner: %v", err)
	}

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}

	manifest, err := plugin.LoadManifest(filepath.Join(root, "subagents", "subagents"))
	if err != nil {
		t.Fatalf("LoadManifest(subagents): %v", err)
	}
	if err := hub.RegisterPlugin(manifest, map[string]any{"max_children": 1, "max_spawn_depth": 3}); err != nil {
		t.Fatalf("RegisterPlugin(subagents): %v", err)
	}
	defer stopPluginRun(t, hub, "subagents")

	for _, want := range []string{"subagent_spawn", "subagent_list", "subagent_wait", "subagent_kill"} {
		if _, ok := hub.toolExec[want]; !ok {
			t.Fatalf("expected subagents tool %s in dispatch", want)
		}
	}

	recv := addToolResultCaptureClient(t, hub, "parent")
	hub.tools.handleDynamicTool("parent", "spawn-1", "subagent_spawn", json.RawMessage(`{"type":"general","task":"stay alive","id":"sa-one","timeout":30}`))
	msg := waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"status":"running"`) || !strings.Contains(msg.Output, `"depth":1`) {
		t.Fatalf("first subagent spawn result: %+v", msg)
	}

	hub.tools.handleDynamicTool("parent", "spawn-2", "subagent_spawn", json.RawMessage(`{"type":"general","task":"blocked","id":"sa-two","timeout":30}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, "too many active subagents (1)") {
		t.Fatalf("expected MaxChildren denial, got: %+v", msg)
	}

	hub.tools.handleDynamicTool("parent", "depth", "subagent_spawn", json.RawMessage(`{"type":"general","task":"too deep","id":"sa-deep","parent_depth":2}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, "max spawn depth reached (3)") {
		t.Fatalf("expected max depth denial, got: %+v", msg)
	}

	hub.tools.handleDynamicTool("parent", "list", "subagent_list", json.RawMessage(`{"parent_session":"parent"}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"id":"sa-one"`) {
		t.Fatalf("expected subagent_list to include sa-one, got: %+v", msg)
	}

	hub.tools.handleDynamicTool("parent", "kill", "subagent_kill", json.RawMessage(`{"id":"sa-one"}`))
	msg = waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"status":"killed"`) {
		t.Fatalf("expected subagent_kill killed result, got: %+v", msg)
	}
}

const fakeMCPServerPython = `#!/usr/bin/env python3
import json
import sys

for line in sys.stdin:
    req = json.loads(line)
    method = req.get("method")
    rid = req.get("id")
    if method == "initialize":
        result = {"serverInfo":{"name":"fake"},"capabilities":{"tools":{}}}
    elif method == "tools/list":
        result = {"tools":[{"name":"echo","description":"Echo a message","inputSchema":{"type":"object","properties":{"message":{"type":"string","description":"Message to echo"}},"required":["message"]}}]}
    elif method == "tools/call":
        params = req.get("params") or {}
        msg = (params.get("arguments") or {}).get("message", "")
        result = {"content":[{"type":"text","text":msg}]}
    elif method == "notifications/initialized":
        continue
    else:
        print(json.dumps({"jsonrpc":"2.0","id":rid,"error":{"code":-32601,"message":"unknown"}}), flush=True)
        continue
    print(json.dumps({"jsonrpc":"2.0","id":rid,"result":result}), flush=True)
`

const fakeSubagentRunnerPython = `#!/usr/bin/env python3
import argparse
import signal
import sys
import time

parser = argparse.ArgumentParser()
parser.add_argument("--provider")
parser.add_argument("--id", required=True)
parser.add_argument("--parent-session", required=True)
parser.add_argument("--task", required=True)
parser.add_argument("--model")
parser.add_argument("--timeout", type=int, default=30)
parser.add_argument("--max-turns", type=int, default=20)
args = parser.parse_args()

running = True
def stop(_sig, _frame):
    global running
    running = False

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
deadline = time.time() + max(1, args.timeout)
while running and time.time() < deadline:
    time.sleep(0.1)
sys.exit(0)
`

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func waitForToolDispatch(t *testing.T, hub *Hub, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := hub.toolExec[name]; ok {
			return
		}
		if time.Now().After(deadline) {
			handle := hub.plugins.Get("mcp")
			var names []string
			if handle != nil {
				for _, tool := range handle.Tools() {
					names = append(names, tool.Name)
				}
			}
			t.Fatalf("timed out waiting for tool %s, current plugin tools=%v", name, names)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}
