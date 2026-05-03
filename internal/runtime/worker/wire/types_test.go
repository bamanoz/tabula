package wire

import (
	"bufio"
	"bytes"
	"errors"
	"reflect"
	"testing"

	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestWorkerFrameRoundTripEveryType(t *testing.T) {
	frames := []any{
		&WorkerInit{Op: OpInit, KernelID: "main", TenantID: "default", TargetID: "timer", Env: map[string]string{"TABULA_TENANT_ID": "default"}},
		&WorkerInitAck{Op: OpInitAck, Ready: true, Tools: []runtimewire.ToolSpec{{Name: "run"}}, Subscriptions: []runtimewire.HookSpec{{Event: "before_tool_call"}}},
		&WorkerCall{Op: OpCall, CallID: "call-1", Tool: "run", Args: []byte(`{"x":1}`)},
		&WorkerResult{Op: OpResult, CallID: "call-1", OK: true, Data: []byte(`{"ok":true}`)},
		&WorkerEvent{Op: OpEvent, CallID: "hook-1", Event: "before_tool_call", ReplyMode: ReplyModeModifying, Data: []byte(`{"tool":"run"}`)},
		&WorkerEventReply{Op: OpEventReply, CallID: "hook-1", Action: runtimewire.HookActionRewrite, Data: []byte(`{"tool":"safe_run"}`)},
		&WorkerToolsUpdated{Op: OpToolsUpdated, Revision: 2, Tools: []runtimewire.ToolSpec{{Name: "run"}}, Removed: []string{"old_run"}},
		&WorkerSend{Op: OpSend, Channel: "bus", Type: "worker_event", Payload: []byte(`{"ok":true}`)},
		&WorkerLog{Op: OpLog, Level: "info", Message: "worker ready", Fields: []byte(`{"worker":1}`)},
		&WorkerShutdown{Op: OpShutdown, Reason: "test"},
		&WorkerError{Op: OpError, Error: WorkerErrorBody{Code: "panic", Message: "boom"}},
	}

	for _, frame := range frames {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, frame); err != nil {
			t.Fatalf("WriteFrame(%T): %v", frame, err)
		}
		_, decoded, err := ReadFrame(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("ReadFrame(%T): %v", frame, err)
		}
		if !reflect.DeepEqual(frame, decoded) {
			t.Fatalf("round trip mismatch for %T\nwant: %#v\n got: %#v", frame, frame, decoded)
		}
	}
}

func TestWorkerNDJSONSplitterOrder(t *testing.T) {
	var buf bytes.Buffer
	for _, callID := range []string{"one", "two", "three"} {
		if err := WriteFrame(&buf, &WorkerCall{Op: OpCall, CallID: callID, Tool: "run"}); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(&buf)
	for _, want := range []string{"one", "two", "three"} {
		_, frame, err := ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := frame.(*WorkerCall)
		if !ok {
			t.Fatalf("expected WorkerCall, got %T", frame)
		}
		if got.CallID != want {
			t.Fatalf("wanted %s, got %s", want, got.CallID)
		}
	}
}

func TestReadFrameEOFDistinctFromMalformedJSON(t *testing.T) {
	_, _, err := ReadFrame(bufio.NewReader(bytes.NewBuffer(nil)))
	if !errors.Is(err, ErrEOF) {
		t.Fatalf("expected ErrEOF, got %v", err)
	}

	_, _, err = ReadFrame(bufio.NewReader(bytes.NewBufferString("{bad-json}\n")))
	var protocolErr ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProtocolError, got %T %v", err, err)
	}
}

func TestWriteFrameRejectsReadyInitAckWithoutCatalogMetadata(t *testing.T) {
	var buf bytes.Buffer
	err := WriteFrame(&buf, &WorkerInitAck{Op: OpInitAck, Ready: true})
	var protocolErr ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProtocolError, got %T %v", err, err)
	}
}
