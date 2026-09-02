package wire

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeDecodeRoundTripEveryOp(t *testing.T) {
	raw := json.RawMessage(`{"path":"README.md"}`)
	frames := []any{
		&Hello{Op: OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1", Capabilities: []Capability{{Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []ToolSpec{{Name: "read"}}, Hooks: []HookSpec{{Event: "before_tool_call", Priority: 10}}, Revision: 1, State: CapabilityStateReady, Source: CapabilitySourceWorker}}},
		&HelloAck{Op: OpHelloAck, Accepted: true, KernelID: "main"},
		&Invoke{Op: OpInvoke, CallID: "call-1", TenantID: "default", Meta: json.RawMessage(`{"actor":"agent/build"}`), Target: Target{Kind: TargetKindSkill, ID: "timer"}, Tool: "run", Args: raw, TimeoutMS: 5000},
		&InvokeResult{Op: OpInvokeResult, CallID: "call-1", OK: true, Data: json.RawMessage(`{"ok":true}`)},
		&InvokeResultStart{Op: OpInvokeResultStart, CallID: "call-2"},
		&InvokeResultDelta{Op: OpInvokeResultDelta, CallID: "call-2", Seq: 1, Data: `{"ok":`},
		&InvokeResultEnd{Op: OpInvokeResultEnd, CallID: "call-2", Bytes: 6},
		&Cancel{Op: OpCancel, CallID: "call-1"},
		&CancelAck{Op: OpCancelAck, CallID: "call-1"},
		&Health{Op: OpHealth},
		&HealthResp{Op: OpHealthResp, OK: true, UptimeMS: 12, WorkerCount: 3},
		&ListCapabilities{Op: OpListCapabilities},
		&ListCapabilitiesResp{Op: OpListCapabilitiesResp, Targets: []Capability{{Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []ToolSpec{{Name: "read"}}, Revision: 2, State: CapabilityStateReady, Source: CapabilitySourceWorker}}},
		&Reload{Op: OpReload, Target: &Target{Kind: TargetKindPlugin, ID: "fs"}},
		&ReloadAck{Op: OpReloadAck, EvictedTargets: []Target{{Kind: TargetKindPlugin, ID: "fs"}}},
		&PrepareTenant{Op: OpPrepareTenant, RequestID: "prepare-1", TenantID: "default"},
		&PrepareTenantAck{Op: OpPrepareTenantAck, RequestID: "prepare-1", Capabilities: []Capability{{Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []ToolSpec{{Name: "read"}}, Revision: 2, State: CapabilityStateReady, Source: CapabilitySourceWorker}}},
		&HookEvent{Op: OpHookEvent, CallID: "hook-1", Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Event: "before_tool_call", ReplyMode: HookReplyModeModifying, Data: json.RawMessage(`{"tool":"read"}`)},
		&CatalogUpdate{Op: OpCatalogUpdate, Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []ToolSpec{{Name: "read", Description: "Read file"}}, Hooks: []HookSpec{{Event: "before_tool_call", Priority: 1}}, Revision: 3, State: CapabilityStateReady, Source: CapabilitySourceWorker, Diagnostic: "ready"},
		&HookEventReply{Op: OpHookEventReply, CallID: "hook-1", Action: HookActionRewrite, Data: json.RawMessage(`{"tool":"read_file"}`), Reason: "renamed"},
		&HookEventReply{Op: OpHookEventReply, CallID: "hook-2", Action: HookActionSuspend, Data: json.RawMessage(`{"kind":"approval_required"}`), Reason: "approval required"},
		&PluginSend{Op: OpPluginSend, Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Channel: "bus", Type: "notify", Payload: json.RawMessage(`{"ok":true}`)},
		&PluginLog{Op: OpPluginLog, Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Level: "info", Message: "ready", Fields: json.RawMessage(`{"worker":1}`)},
		&LifecycleNotice{Op: OpLifecycleNotice, Target: Target{Kind: TargetKindPlugin, ID: "fs"}, State: LifecycleStateReady, PID: 42, Message: "attached"},
	}

	for _, frame := range frames {
		data, err := Encode(frame)
		if err != nil {
			t.Fatalf("Encode(%T): %v", frame, err)
		}
		_, decoded, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(%T): %v", frame, err)
		}
		if !reflect.DeepEqual(frame, decoded) {
			t.Fatalf("round trip mismatch for %T\nwant: %#v\n got: %#v", frame, frame, decoded)
		}
	}
}

func TestDecodeRejectsInvokeWithoutTenantID(t *testing.T) {
	_, _, err := Decode([]byte(`{"op":"invoke","call_id":"call-1","target":{"kind":"skill","id":"timer"},"tool":"run"}`))
	if err == nil {
		t.Fatal("expected missing tenant_id to fail")
	}
}

func TestDecodeRejectsUnknownOp(t *testing.T) {
	_, _, err := Decode([]byte(`{"op":"future"}`))
	if err == nil {
		t.Fatal("expected unknown op to fail")
	}
}

func TestDecodeRejectsCapabilityWithoutStateAndSource(t *testing.T) {
	_, _, err := Decode([]byte(`{"op":"hello","runtime_id":"local","token":"redacted","protocol_version":"1","capabilities":[{"target":{"kind":"plugin","id":"fs"},"tools":[{"name":"read"}]}]}`))
	if err == nil {
		t.Fatal("expected capability without state/source to fail")
	}
}

func TestDecodeRejectsPluginSendNonBusChannel(t *testing.T) {
	_, _, err := Decode([]byte(`{"op":"plugin_send","target":{"kind":"plugin","id":"fs"},"channel":"stderr","type":"notify"}`))
	if err == nil {
		t.Fatal("expected non-bus plugin_send channel to fail")
	}
}

func TestDecodeRejectsHookEventWithoutReplyMode(t *testing.T) {
	_, _, err := Decode([]byte(`{"op":"hook_event","call_id":"hook-1","target":{"kind":"plugin","id":"fs"},"event":"before_tool_call"}`))
	if err == nil {
		t.Fatal("expected missing reply_mode to fail")
	}
}

func TestCanonicalErrorRosterExcludesStaleAndPluginInternalCodes(t *testing.T) {
	for _, code := range []ErrorCode{ErrorCancelled, ErrorRuntimeBusy, ErrorTenantForbidden, ErrorTargetForbidden} {
		if !IsErrorCode(code) {
			t.Fatalf("expected %s to be canonical", code)
		}
	}
	for _, code := range []ErrorCode{"tenant_denied", "target_not_authorized", "fs_outside_root", "exec_denied"} {
		if IsErrorCode(code) {
			t.Fatalf("%s must not be a Runtime API wire error", code)
		}
	}
}

func TestIdentifierValidation(t *testing.T) {
	if err := ValidateTenantID("default"); err != nil {
		t.Fatalf("default tenant should be valid: %v", err)
	}
	for _, id := range []string{"", "Upper", "bad_underscore", "-bad"} {
		if err := ValidateTenantID(id); err == nil {
			t.Fatalf("expected tenant id %q to fail", id)
		}
	}
	for _, id := range []string{"kernel", "system", "admin"} {
		if err := ValidateRuntimeID(id); err == nil {
			t.Fatalf("expected runtime id %q to be reserved", id)
		}
	}
}
