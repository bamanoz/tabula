package wire

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeDecodeRoundTripEveryOp(t *testing.T) {
	raw := json.RawMessage(`{"path":"README.md"}`)
	frames := []any{
		&Hello{Op: OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1", Capabilities: []Capability{{Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []string{"read"}}}},
		&HelloAck{Op: OpHelloAck, Accepted: true, KernelID: "main"},
		&Invoke{Op: OpInvoke, CallID: "call-1", TenantID: "default", Target: Target{Kind: TargetKindSkill, ID: "timer"}, Tool: "run", Args: raw, TimeoutMS: 5000},
		&InvokeResult{Op: OpInvokeResult, CallID: "call-1", OK: true, Data: json.RawMessage(`{"ok":true}`)},
		&Cancel{Op: OpCancel, CallID: "call-1"},
		&CancelAck{Op: OpCancelAck, CallID: "call-1"},
		&Health{Op: OpHealth},
		&HealthResp{Op: OpHealthResp, OK: true, UptimeMS: 12, WorkerCount: 3},
		&ListCapabilities{Op: OpListCapabilities},
		&ListCapabilitiesResp{Op: OpListCapabilitiesResp, Targets: []Capability{{Target: Target{Kind: TargetKindPlugin, ID: "fs"}, Tools: []string{"read"}}}},
		&Reload{Op: OpReload, Target: &Target{Kind: TargetKindPlugin, ID: "fs"}},
		&ReloadAck{Op: OpReloadAck, EvictedTargets: []Target{{Kind: TargetKindPlugin, ID: "fs"}}},
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
