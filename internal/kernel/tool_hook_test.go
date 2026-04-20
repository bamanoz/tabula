package kernel

import (
	"encoding/json"
	"runtime"
	"testing"
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
