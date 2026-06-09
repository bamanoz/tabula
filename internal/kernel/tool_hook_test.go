package kernel

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBeforeToolCallHookCanModifyToolInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-modify",
			Input: json.RawMessage(`{"text":"original"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "modify",
		Payload: json.RawMessage(`{
			"tool":"echo_tool",
			"id":"t-modify",
			"input":{"text":"rewritten"}
		}`),
	})

	result := readMsg(t, drv)
	if !isToolResult(&result) {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "rewritten") || strings.Contains(result.Output, "original") {
		t.Fatalf("expected rewritten plugin input, got %q", result.Output)
	}
}

func TestBeforeToolCallHookReceivesToolMeta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)
	hook := env.connectHook("perm", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-meta",
			Input: json.RawMessage(`{"text":"ok"}`),
			Meta:  json.RawMessage(`{"actor":"agent/immune-plan"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	var payload struct {
		Meta map[string]string `json:"meta"`
	}
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	if payload.Meta["actor"] != "agent/immune-plan" {
		t.Fatalf("expected actor meta in hook payload, got %+v", payload.Meta)
	}
	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	result := readMsg(t, drv)
	if !isToolResult(&result) {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
}

// Interactive approval-style hook: timeout_ms=0 means wait until the
// subscriber replies. The hook reply gates the tool execution.
func TestBeforeToolCallHook_InfiniteTimeout_RepliesAfterDelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)

	infinite := 0
	hook := env.connectHook("approval", []HookSubscription{
		{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-wait",
			Input: json.RawMessage(`{"text":"ok"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %s", hookMsg.Type)
	}

	// Simulate user pondering well past the legacy 5s default.
	time.Sleep(6 * time.Second)

	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	result := readMsgTimeout(t, drv, 10*time.Second)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result after late approval, got %v", result)
	}
	if !strings.Contains(result.Output, "ok") {
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

	env := newTestEnvWithSkillTool(t)

	infinite := 0
	hook := env.connectHook("approval", []HookSubscription{
		{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-disco",
			Input: json.RawMessage(`{"text":"should-not-run"}`),
		})
	}()

	if hookMsg := readMsg(t, hook); hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %s", hookMsg.Type)
	}

	// Drop the subscriber without replying.
	hook.Close()

	result := readMsgTimeout(t, drv, 5*time.Second)
	if result == nil {
		t.Fatal("driver should receive tool_result with blocked error")
	}
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked") {
		t.Fatalf("expected blocked output, got %q", result.Output)
	}
}

func TestBeforeToolCallHookSkipsSenderHookSubscription(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)
	infinite := 0

	// The same client both subscribes to before_tool_call and sends a tool.call.
	// Dispatching a synchronous hook back to it would deadlock because its
	// readPump is currently handling this tool.call.
	client := env.dial()
	writeJSON(t, client, Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":           "gateway-self-hook",
			"send_topics":    []string{TopicToolCall, "hook_reply"},
			"receive_topics": []string{TopicToolResult, "hook"},
			"hooks":          []HookSubscription{{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite}},
			"auth_token":     env.Token,
		}),
	})
	if msg := readMsg(t, client); msg.Type != string(MsgHelloAck) {
		t.Fatalf("expected hello_ack, got %s", msg.Type)
	}
	writeJSON(t, client, Message{Type: "join", Session: "main"})
	if msg := readMsg(t, client); msg.Type != "joined" {
		t.Fatalf("expected joined, got %s", msg.Type)
	}

	writeJSON(t, client, Message{
		Type:  string(MsgRequest),
		Topic: TopicToolCall,
		Name:  "echo_tool",
		ID:    "self-hook",
		Input: json.RawMessage(`{"text":"ok"}`),
	})

	result := readMsgTimeout(t, client, 2*time.Second)
	if result == nil {
		t.Fatal("expected tool_result without self-hook deadlock")
	}
	if result.Type == "hook" {
		t.Fatalf("sender should not receive its own synchronous hook")
	}
	if !isToolResult(result) || !strings.Contains(result.Output, "ok") {
		t.Fatalf("expected ok tool_result, got %+v", result)
	}
}

func TestBeforeToolResultHookTimeoutFailsOpenForSmallResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)
	timeout := 10
	hook := env.connectHook("tool-results", []HookSubscription{{Event: "before_tool_result", Priority: 100, TimeoutMs: &timeout}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-small-result",
			Input: json.RawMessage(`{"text":"ok"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_result" {
		t.Fatalf("expected hook/before_tool_result, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	result := readMsgTimeout(t, drv, 2*time.Second)
	if result == nil || !isToolResult(result) || !strings.Contains(result.Output, "ok") {
		t.Fatalf("expected small result to fail open, got %+v", result)
	}
}

func TestBeforeToolResultHookCanRewriteLargeResultFromSpool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithSkillTool(t)
	hook := env.connectHook("tool-results", []HookSubscription{{Event: "before_tool_result", Priority: 100}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})
	large := strings.Repeat("x", 13000)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t-large-result",
			Input: mustMarshalRaw(map[string]any{"text": large}),
		})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_result" {
		t.Fatalf("expected hook/before_tool_result, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload struct {
		Tool   string `json:"tool"`
		ID     string `json:"id"`
		Source struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		} `json:"source"`
	}
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	if payload.Tool != "echo_tool" || payload.ID != "t-large-result" || payload.Source.Kind != "spool_file" || payload.Source.Path == "" {
		t.Fatalf("unexpected hook payload: %+v", payload)
	}
	content, err := os.ReadFile(payload.Source.Path)
	if err != nil {
		t.Fatalf("read spool: %v", err)
	}
	previewLen := len(content)
	if previewLen > 128 {
		previewLen = 128
	}
	if !strings.Contains(string(content), large[:128]) {
		t.Fatalf("expected spool to contain large result prefix, got %q", string(content[:previewLen]))
	}
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "modify",
		Payload: mustMarshalRaw(map[string]any{
			"tool":      payload.Tool,
			"id":        payload.ID,
			"output":    "preview",
			"artifact":  map[string]any{"ref": "artifact://echo-large", "chars": len(large)},
			"truncated": true,
		}),
	})
	result := readMsgTimeout(t, drv, 2*time.Second)
	if result == nil || !isToolResult(result) || result.Output != "preview" || !result.Truncated {
		t.Fatalf("expected rewritten large tool result, got %+v", result)
	}
	var artifact struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(result.Artifact, &artifact); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if artifact.Ref != "artifact://echo-large" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
}
