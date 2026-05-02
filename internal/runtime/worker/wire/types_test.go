package wire

import (
	"bufio"
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestWorkerFrameRoundTripEveryType(t *testing.T) {
	frames := []any{
		&WorkerInit{KernelID: "main", TenantID: "default", TargetID: "timer", Env: map[string]string{"TABULA_TENANT_ID": "default"}},
		&WorkerInitAck{Ready: true},
		&WorkerCall{CallID: "call-1", Tool: "run", Args: []byte(`{"x":1}`)},
		&WorkerResult{CallID: "call-1", OK: true, Data: []byte(`{"ok":true}`)},
		&WorkerShutdown{Reason: "test"},
		&WorkerError{Error: WorkerErrorBody{Code: "panic", Message: "boom"}},
	}

	for _, frame := range frames {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, frame); err != nil {
			t.Fatalf("WriteFrame(%T): %v", frame, err)
		}
		decoded := reflect.New(reflect.TypeOf(frame).Elem()).Interface()
		if err := ReadFrame(bufio.NewReader(&buf), decoded); err != nil {
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
		if err := WriteFrame(&buf, &WorkerCall{CallID: callID, Tool: "run"}); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(&buf)
	for _, want := range []string{"one", "two", "three"} {
		var got WorkerCall
		if err := ReadFrame(reader, &got); err != nil {
			t.Fatal(err)
		}
		if got.CallID != want {
			t.Fatalf("wanted %s, got %s", want, got.CallID)
		}
	}
}

func TestReadFrameEOFDistinctFromMalformedJSON(t *testing.T) {
	err := ReadFrame(bufio.NewReader(bytes.NewBuffer(nil)), &WorkerCall{})
	if !errors.Is(err, ErrEOF) {
		t.Fatalf("expected ErrEOF, got %v", err)
	}

	err = ReadFrame(bufio.NewReader(bytes.NewBufferString("{bad-json}\n")), &WorkerCall{})
	var protocolErr ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProtocolError, got %T %v", err, err)
	}
}
