package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewMessageRoundTrip(t *testing.T) {
	params := ToolCallParams{
		CallID:     "tc-1",
		Name:       "git_status",
		Args:       json.RawMessage(`{"path":"."}`),
		Session:    "sess-1",
		DeadlineMs: 30000,
	}
	msg, err := NewMessage(MethodToolCall, params)
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if msg.Method != MethodToolCall {
		t.Fatalf("method: got %q want %q", msg.Method, MethodToolCall)
	}
	var got ToolCallParams
	if err := msg.DecodeParams(&got); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	if got.CallID != "tc-1" || got.Name != "git_status" || got.DeadlineMs != 30000 {
		t.Fatalf("decoded params mismatch: %+v", got)
	}
}

func TestReaderSkipsBlankLinesAndDecodes(t *testing.T) {
	src := strings.NewReader(
		"\n   \n" +
			`{"method":"register","params":{"protocol_version":1,"plugin_id":"x","tools":[],"subscriptions":[]}}` + "\n" +
			`{"method":"log","params":{"level":"info","msg":"hi"}}` + "\n",
	)
	r := NewReader(src)

	m1, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("first ReadMessage: %v", err)
	}
	if m1.Method != MethodRegister {
		t.Fatalf("first method: %q", m1.Method)
	}

	m2, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("second ReadMessage: %v", err)
	}
	if m2.Method != MethodLog {
		t.Fatalf("second method: %q", m2.Method)
	}

	if _, err := r.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF expected, got %v", err)
	}
}

func TestReaderMalformedReturnsTypedError(t *testing.T) {
	src := strings.NewReader("{not valid json\n")
	r := NewReader(src)

	_, err := r.ReadMessage()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var malformed *MalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("want *MalformedError, got %T: %v", err, err)
	}
	if malformed.Snippet == "" {
		t.Fatalf("snippet should be populated")
	}
}

func TestWriterRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	in, err := NewMessage(MethodLog, LogParams{Level: "info", Msg: "x"})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if err := w.WriteMessage(in); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	// Ensure exactly one '\n' framing.
	if !bytes.HasSuffix(buf.Bytes(), []byte{'\n'}) {
		t.Fatalf("expected trailing newline, got %q", buf.String())
	}
	if bytes.Count(buf.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("expected exactly one newline, got %q", buf.String())
	}

	r := NewReader(&buf)
	out, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if out.Method != MethodLog {
		t.Fatalf("method: %q", out.Method)
	}
	var p LogParams
	if err := out.DecodeParams(&p); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	if p.Level != "info" || p.Msg != "x" {
		t.Fatalf("params: %+v", p)
	}
}

func TestReaderRejectsLineTooLong(t *testing.T) {
	// Build a JSON line that fits MaxLineSize+1 of content body so that
	// the underlying scanner trips bufio.ErrTooLong; we only need it
	// larger than the buffer cap, which is MaxLineSize.
	big := bytes.Repeat([]byte("x"), MaxLineSize+10)
	payload := append([]byte(`{"method":"log","params":{"level":"info","msg":"`), big...)
	payload = append(payload, []byte(`"}}`+"\n")...)

	r := NewReader(bytes.NewReader(payload))
	_, err := r.ReadMessage()
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("want ErrLineTooLong, got %v", err)
	}
}
