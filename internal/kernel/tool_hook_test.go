package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBeforeToolCallHookCanModifyToolInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)

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

	env := newTestEnvWithPluginTool(t)
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

func TestBeforeToolCallHookCanSuspendForApprovalAndResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)
	hook := env.connectHook("approval", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	resolvedHook := env.connectHook("approval-recorder", []HookSubscription{{Event: "approval_resolved", Priority: 0}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolCall, TopicToolResult, "tool.suspended", "tool.resumed"})
	ui := env.connectAndJoin("ui", "main", []string{TopicExchangeApprove}, []string{TopicExchangeApprove})

	go func() {
		writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-suspend", Input: json.RawMessage(`{"text":"needs approval"}`)})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %+v", hookMsg)
	}
	writeJSON(t, hook, Message{
		Type:    string(MsgHookReply),
		ID:      hookMsg.ID,
		Action:  string(ActionSuspend),
		Reason:  "approve exec",
		Payload: json.RawMessage(`{"kind":"approval_required","question":"Approve?","details":{"tool":"echo_tool"},"options":["allow once","deny once"]}`),
	})

	approvalReq := readMsg(t, ui)
	if approvalReq.Type != string(MsgRequest) || approvalReq.Topic != TopicExchangeApprove || approvalReq.ID == "" {
		t.Fatalf("expected approval request, got %+v", approvalReq)
	}
	if msg := readMsgTimeout(t, drv, 100*time.Millisecond); msg != nil && msg.Topic == TopicToolResult {
		t.Fatalf("tool result should not be emitted while approval is pending: %+v", msg)
	}

	writeJSON(t, ui, Message{Type: string(MsgReply), Topic: TopicExchangeApprove, ID: approvalReq.ID, Data: json.RawMessage(`{"choice":"allow once"}`)})
	resolved := readMsg(t, resolvedHook)
	if resolved.Type != "hook" || resolved.Name != "approval_resolved" || !strings.Contains(string(resolved.Payload), "allow once") {
		t.Fatalf("expected approval_resolved hook, got %+v", resolved)
	}

	for i := 0; i < 3; i++ {
		msg := readMsg(t, drv)
		if msg.Topic != TopicToolResult {
			continue
		}
		if !strings.Contains(msg.Output, "needs approval") {
			t.Fatalf("expected resumed tool result, got %q", msg.Output)
		}
		return
	}
	t.Fatal("expected resumed tool result")
}

func TestSuspendedBeforeToolCallResumesLaterRewriteHookAfterApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)
	approval := env.connectHook("approval", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	rewriter := env.connectHook("rewriter", []HookSubscription{{Event: "before_tool_call", Priority: 10}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolCall, TopicToolResult, "tool.suspended", "tool.resumed"})
	ui := env.connectAndJoin("ui", "main", []string{TopicExchangeApprove}, []string{TopicExchangeApprove})

	go func() {
		writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-suspend-rewrite", Input: json.RawMessage(`{"text":"original"}`)})
	}()

	approvalMsg := readMsg(t, approval)
	if approvalMsg.Type != "hook" || approvalMsg.Name != "before_tool_call" {
		t.Fatalf("expected approval before_tool_call, got %+v", approvalMsg)
	}
	writeJSON(t, approval, Message{Type: string(MsgHookReply), ID: approvalMsg.ID, Action: string(ActionSuspend), Reason: "approve rewritten"})

	approvalReq := readMsg(t, ui)
	if approvalReq.Type != string(MsgRequest) || approvalReq.Topic != TopicExchangeApprove {
		t.Fatalf("expected approval request, got %+v", approvalReq)
	}
	if !strings.Contains(string(approvalReq.Data), `"text":"original"`) || strings.Contains(string(approvalReq.Data), `"text":"rewritten"`) {
		t.Fatalf("expected approval request to include pre-suspend input only, got %s", string(approvalReq.Data))
	}

	writeJSON(t, ui, Message{Type: string(MsgReply), Topic: TopicExchangeApprove, ID: approvalReq.ID, Data: json.RawMessage(`{"choice":"allow once"}`)})

	rewriteMsg := readMsg(t, rewriter)
	if rewriteMsg.Type != "hook" || rewriteMsg.Name != "before_tool_call" {
		t.Fatalf("expected rewrite before_tool_call after approval, got %+v", rewriteMsg)
	}
	env.Hub.hooks.HandleRuntimeResult("", &Message{ID: rewriteMsg.ID, Action: string(ActionModify), Data: json.RawMessage(`{"input":{"text":"rewritten"}}`)})

	var called Message
	for i := 0; i < 4; i++ {
		called = readMsg(t, drv)
		if called.Topic == TopicToolCall {
			break
		}
	}
	if called.Topic != TopicToolCall {
		t.Fatalf("expected finalized tool.call broadcast, got %+v", called)
	}
	if !strings.Contains(string(called.Input), "rewritten") {
		t.Fatalf("expected finalized tool.call to include rewritten input, got %s", string(called.Input))
	}
	for i := 0; i < 3; i++ {
		msg := readMsg(t, drv)
		if msg.Topic != TopicToolResult {
			continue
		}
		if !strings.Contains(msg.Output, "rewritten") {
			t.Fatalf("expected resumed tool result to use rewritten input, got %q", msg.Output)
		}
		return
	}
	t.Fatal("expected resumed tool result")
}

func TestSuspendedApprovalResendsWhenUIRejoins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)
	hook := env.connectHook("approval", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})
	ui := env.connectAndJoin("ui", "main", []string{TopicExchangeApprove}, []string{TopicExchangeApprove})

	go func() {
		writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-resend", Input: json.RawMessage(`{"text":"needs approval"}`)})
	}()
	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: string(MsgHookReply), ID: hookMsg.ID, Action: string(ActionSuspend), Reason: "approve exec", Payload: json.RawMessage(`{"kind":"approval_required","question":"Approve again?","details":{"tool":"echo_tool"},"options":["allow once","deny once"]}`)})
	firstReq := readMsg(t, ui)
	if firstReq.Topic != TopicExchangeApprove || firstReq.ID == "" {
		t.Fatalf("expected initial approval request, got %+v", firstReq)
	}
	ui.Close()

	ui2 := env.connectAndJoin("ui2", "main", []string{TopicExchangeApprove}, []string{TopicExchangeApprove})
	resent := readMsg(t, ui2)
	if resent.Topic != TopicExchangeApprove || resent.ID != firstReq.ID {
		t.Fatalf("expected resent approval request %q, got %+v", firstReq.ID, resent)
	}
	if !strings.Contains(string(resent.Data), "Approve again?") {
		t.Fatalf("expected original approval payload to be resent, got %s", string(resent.Data))
	}
}

// Interactive approval-style hook: timeout_ms=0 means wait until the
// subscriber replies. The hook reply gates the tool execution.
func TestBeforeToolCallHook_InfiniteTimeout_RepliesAfterDelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)

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

	env := newTestEnvWithPluginTool(t)

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
	var blocked struct {
		Error string `json:"error"`
		Hook  struct {
			Status string `json:"status"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(result.Output), &blocked); err != nil {
		t.Fatalf("blocked output should be structured JSON: %v (%s)", err, result.Output)
	}
	if blocked.Error != "not_invoked" || blocked.Hook.Status == "" {
		t.Fatalf("unexpected blocked output: %+v", blocked)
	}
}

func TestBeforeToolCallHookTimeoutThenNextSubscriberDisconnect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)
	permTimeout := 20
	infinite := 0
	perm := env.connectHook("hook-permissions", []HookSubscription{
		{Event: "before_tool_call", Priority: 100, TimeoutMs: &permTimeout},
	})
	approval := env.connectHook("hook-approvals", []HookSubscription{
		{Event: "before_tool_call", Priority: 10, TimeoutMs: &infinite},
	})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})

	writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-timeout", Input: json.RawMessage(`{"text":"blocked-by-timeout"}`)})
	if hookMsg := readMsg(t, perm); hookMsg.Type != "hook" || hookMsg.ID == "" {
		t.Fatalf("expected permissions hook request, got %+v", hookMsg)
	}
	first := readMsgTimeout(t, drv, 5*time.Second)
	if first == nil || !isToolResult(first) {
		t.Fatalf("expected first tool_result blocked by timeout, got %+v", first)
	}
	var firstBlocked struct {
		Error string `json:"error"`
		Hook  struct {
			Status string `json:"status"`
			Target string `json:"target"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(first.Output), &firstBlocked); err != nil {
		t.Fatalf("unmarshal first blocked result: %v (%s)", err, first.Output)
	}
	if firstBlocked.Error != "not_invoked" || firstBlocked.Hook.Status != "timeout" || !strings.Contains(firstBlocked.Hook.Target, "hook-permissions") {
		t.Fatalf("unexpected first blocked result: %+v", firstBlocked)
	}
	writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-disconnect", Input: json.RawMessage(`{"text":"blocked-by-disconnect"}`)})
	permMsg := readMsg(t, perm)
	if permMsg.Type != "hook" || permMsg.ID == "" {
		t.Fatalf("expected second permissions hook request, got %+v", permMsg)
	}
	writeJSON(t, perm, Message{Type: "hook_reply", ID: permMsg.ID, Action: "pass"})
	approvalMsg := readMsg(t, approval)
	if approvalMsg.Type != "hook" || approvalMsg.ID == "" {
		t.Fatalf("expected approval hook request, got %+v", approvalMsg)
	}
	approval.Close()

	second := readMsgTimeout(t, drv, 5*time.Second)
	if second == nil || !isToolResult(second) {
		t.Fatalf("expected second tool_result blocked by disconnect, got %+v", second)
	}
	var secondBlocked struct {
		Error string `json:"error"`
		Hook  struct {
			Status string `json:"status"`
			Target string `json:"target"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(second.Output), &secondBlocked); err != nil {
		t.Fatalf("unmarshal second blocked result: %v (%s)", err, second.Output)
	}
	if secondBlocked.Error != "not_invoked" || secondBlocked.Hook.Status != "disconnected" || !strings.Contains(secondBlocked.Hook.Target, "hook-approvals") {
		t.Fatalf("unexpected second blocked result: %+v", secondBlocked)
	}
}

func TestBeforeToolCallHookBlockCarriesGenericRetryableDetails(t *testing.T) {
	env := newTestEnvWithPluginTool(t)
	hook := env.connectHook("policy", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})

	writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-retryable", Input: json.RawMessage(`{"text":"blocked"}`)})
	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:    string(MsgHookReply),
		ID:      hookMsg.ID,
		Action:  string(ActionBlock),
		Reason:  "transient gate unavailable",
		Payload: mustMarshalRaw(map[string]any{"kind": "approval_required", "retryable": true}),
	})

	result := readMsgTimeout(t, drv, 5*time.Second)
	if result == nil || !isToolResult(result) {
		t.Fatalf("expected retryable not_invoked tool_result, got %+v", result)
	}
	var blocked struct {
		Error     string `json:"error"`
		Kind      string `json:"kind"`
		Retryable bool   `json:"retryable"`
		Hook      struct {
			Reason  string         `json:"reason"`
			Details map[string]any `json:"details"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(result.Output), &blocked); err != nil {
		t.Fatalf("unmarshal blocked output: %v (%s)", err, result.Output)
	}
	if blocked.Error != "not_invoked" || blocked.Kind != "approval_required" || !blocked.Retryable {
		t.Fatalf("unexpected generic retryable fields: %+v", blocked)
	}
	if blocked.Hook.Reason != "transient gate unavailable" || blocked.Hook.Details["kind"] != "approval_required" {
		t.Fatalf("unexpected hook details: %+v", blocked.Hook)
	}
}

func TestBeforeToolCallHookSkipsSenderHookSubscription(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	env := newTestEnvWithPluginTool(t)
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

	env := newTestEnvWithPluginTool(t)
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

	env := newTestEnvWithPluginTool(t)
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

func TestBeforeToolCallHookWritesDispatchAuditEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	home := t.TempDir()
	env := newTestEnvWithPluginToolHome(t, home)
	hook := env.connectHook("perm", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	drv := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicToolResult})

	go func() {
		writeJSON(t, drv, Message{Type: string(MsgRequest), Topic: TopicToolCall, Name: "echo_tool", ID: "t-audit", Input: json.RawMessage(`{"text":"ok"}`), Meta: mustMarshalRaw(map[string]any{turnCorrelationMetaKey: "tc-audit-1"})})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	result := readMsgTimeout(t, drv, 2*time.Second)
	if result == nil || !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}

	ledger := filepath.Join(home, "data", "sessions", "main", "ledger.jsonl")
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "hook.dispatch.audit") || !strings.Contains(text, "echo_tool") || !strings.Contains(text, "continued") || !strings.Contains(text, "tc-audit-1") {
		t.Fatalf("expected hook dispatch audit event, got %q", text)
	}
}

func TestDispatchHookWritesMissingAuditEvent(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"echo_tool","id":"t-missing","input":{"text":"ok"}}`), "default", "main")
	data, err := os.ReadFile(filepath.Join(home, "data", "sessions", "main", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"status":"missing"`) || !strings.Contains(text, `"dispatch_effect":"continued"`) {
		t.Fatalf("expected missing audit event, got %q", text)
	}
}

func TestDispatchHookWritesTimeoutAuditEvent(t *testing.T) {
	home := t.TempDir()
	env := newTestEnvWithPluginToolHome(t, home)
	hook := env.connectHook("perm", []HookSubscription{{Event: "before_tool_call", Priority: 100}})
	defer hook.Close()
	env.Hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"echo_tool","id":"t-timeout","input":{"text":"ok"}}`), "default", "main")
	data, err := os.ReadFile(filepath.Join(home, "data", "sessions", "main", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"status":"timeout"`) || !strings.Contains(text, `"dispatch_effect":"failed_closed"`) {
		t.Fatalf("expected timeout audit event, got %q", text)
	}
}

func TestHookDispatchAuditRedactsExecCommandInput(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"exec_run","id":"t-exec","input":{"command":"echo super-secret-token","timeout_seconds":30}}`), "default", "main")
	data, err := os.ReadFile(filepath.Join(home, "data", "sessions", "main", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "super-secret-token") {
		t.Fatalf("expected exec command to be redacted from audit, got %q", text)
	}
	if !strings.Contains(text, `"command_sha256"`) || !strings.Contains(text, `"timeout_seconds":30`) {
		t.Fatalf("expected summarized exec command fields, got %q", text)
	}
}
