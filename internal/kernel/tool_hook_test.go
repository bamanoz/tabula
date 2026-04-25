package kernel

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBeforeToolCallHookCanModifyToolInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnv(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t-modify",
			Input: json.RawMessage(`{"command":"echo original"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "modify",
		Payload: json.RawMessage(`{
			"tool":"shell_exec",
			"id":"t-modify",
			"input":{"command":"echo rewritten"}
		}`),
	})

	result := readMsg(t, drv)
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if result.Output != "rewritten" {
		t.Fatalf("expected rewritten output, got %q", result.Output)
	}
}

// Interactive approval-style hook: timeout_ms=0 means wait until the
// subscriber replies. The hook reply gates the tool execution.
func TestBeforeToolCallHook_InfiniteTimeout_RepliesAfterDelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnv(t)

	infinite := 0
	hook := env.connectHook("approval", []HookSubscription{
		{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t-wait",
			Input: json.RawMessage(`{"command":"echo ok"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %s", hookMsg.Type)
	}

	// Simulate user pondering well past the legacy 5s default.
	time.Sleep(6 * time.Second)

	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	result := readMsgTimeout(t, drv, 10*time.Second)
	if result == nil || result.Type != "tool_result" {
		t.Fatalf("expected tool_result after late approval, got %v", result)
	}
	if result.Output != "ok" {
		t.Fatalf("expected ok, got %q", result.Output)
	}
}

// Security hook with timeout_ms=0 must fail-closed (block) when the
// subscribing client disconnects without replying. Mirrors the case where
// the gateway-tui exits while an approval modal is open.
func TestBeforeToolCallHook_InfiniteTimeout_DisconnectBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnv(t)

	infinite := 0
	hook := env.connectHook("approval", []HookSubscription{
		{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t-disco",
			Input: json.RawMessage(`{"command":"echo should-not-run"}`),
		})
	}()

	if hookMsg := readMsg(t, hook); hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %s", hookMsg.Type)
	}

	// Drop the subscriber without replying.
	hook.Close()

	result := readMsgTimeout(t, drv, 5*time.Second)
	if result == nil {
		t.Fatal("driver should receive tool_result with blocked-by-hook error")
	}
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked by hook") {
		t.Fatalf("expected blocked-by-hook output, got %q", result.Output)
	}
}

func TestBeforeToolCallHookSkipsSenderHookSubscription(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnv(t)
	infinite := 0

	// The same client both subscribes to before_tool_call and sends a tool_use.
	// Dispatching a synchronous hook back to it would deadlock because its
	// readPump is currently handling this tool_use.
	client := env.dial()
	writeJSON(t, client, Message{
		Type:     "connect",
		Name:     "gateway-self-hook",
		Sends:    []string{"tool_use", "hook_result"},
		Receives: []string{"tool_result", "hook"},
		Hooks:    []HookSubscription{{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite}},
		Version:  ProtocolVersion,
	})
	if msg := readMsg(t, client); msg.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg.Type)
	}
	writeJSON(t, client, Message{Type: "join", Session: "main"})
	if msg := readMsg(t, client); msg.Type != "joined" {
		t.Fatalf("expected joined, got %s", msg.Type)
	}

	writeJSON(t, client, Message{
		Type:  "tool_use",
		Name:  "shell_exec",
		ID:    "self-hook",
		Input: json.RawMessage(`{"command":"echo ok"}`),
	})

	result := readMsgTimeout(t, client, 2*time.Second)
	if result == nil {
		t.Fatal("expected tool_result without self-hook deadlock")
	}
	if result.Type == "hook" {
		t.Fatalf("sender should not receive its own synchronous hook")
	}
	if result.Type != "tool_result" || result.Output != "ok" {
		t.Fatalf("expected ok tool_result, got %+v", result)
	}
}

func TestBeforeToolCallHookSkipsSenderForProcessSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnv(t)
	infinite := 0

	client := env.dial()
	writeJSON(t, client, Message{
		Type:     "connect",
		Name:     "gateway-self-spawn-hook",
		Sends:    []string{"tool_use", "hook_result"},
		Receives: []string{"tool_result", "hook", "member_joined"},
		Hooks:    []HookSubscription{{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite}},
		Version:  ProtocolVersion,
	})
	if msg := readMsg(t, client); msg.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg.Type)
	}
	writeJSON(t, client, Message{Type: "join", Session: "main"})
	if msg := readMsg(t, client); msg.Type != "joined" {
		t.Fatalf("expected joined, got %s", msg.Type)
	}

	writeJSON(t, client, Message{
		Type:  "tool_use",
		Name:  "process_spawn",
		ID:    "self-spawn-hook",
		Input: json.RawMessage(`{"command":"sleep 0.2"}`),
	})

	result := readMsgTimeout(t, client, 2*time.Second)
	if result == nil {
		t.Fatal("expected process_spawn tool_result without self-hook deadlock")
	}
	if result.Type == "hook" {
		t.Fatalf("sender should not receive its own process_spawn hook")
	}
	if result.Type != "tool_result" || !strings.Contains(result.Output, "PID ") {
		t.Fatalf("expected PID tool_result, got %+v", result)
	}
}
