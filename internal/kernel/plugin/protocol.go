package plugin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MaxLineSize caps a single NDJSON message at 10 MiB per
// creative-plugin-protocol.md §2.1. Lines exceeding this are rejected as
// malformed.
const MaxLineSize = 10 * 1024 * 1024

// Method names per creative-plugin-protocol.md §2.5.
const (
	MethodRegisterRequest = "register_request" // kernel → plugin
	MethodRegister        = "register"         // plugin → kernel (reply)
	MethodToolCall        = "tool_call"        // kernel → plugin
	MethodToolResult      = "tool_result"      // plugin → kernel (reply)
	MethodEvent           = "event"            // kernel → plugin
	MethodEventReply      = "event_reply"      // plugin → kernel (reply)
	MethodSend            = "send"             // plugin → kernel
	MethodLog             = "log"              // plugin → kernel
	MethodShutdown        = "shutdown"         // kernel → plugin
	MethodUpdateTools     = "update_tools"     // plugin → kernel (no reply)
)

// EventReply action values per creative-plugin-protocol.md §2.5.
const (
	ActionOK      = "ok"
	ActionRewrite = "rewrite"
	ActionDeny    = "deny"
	ActionClaim   = "claim"
)

// Message is the envelope for every kernel↔plugin JSON-RPC message.
// There is no JSON-RPC `id` field; request/reply correlation uses `callId`
// inside the params payload (creative §2.5).
type Message struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// NewMessage constructs a Message by marshalling params.
func NewMessage(method string, params any) (*Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("plugin protocol: marshal %s params: %w", method, err)
	}
	return &Message{Method: method, Params: raw}, nil
}

// DecodeParams unmarshals m.Params into out.
func (m *Message) DecodeParams(out any) error {
	if len(m.Params) == 0 {
		return nil
	}
	return json.Unmarshal(m.Params, out)
}

// ----------------------------------------------------------------------------
// Param types — kernel → plugin
// ----------------------------------------------------------------------------

// RegisterRequestParams is sent by the kernel immediately after spawning a
// plugin process (creative §2.3).
type RegisterRequestParams struct {
	ProtocolVersion int            `json:"protocol_version"`
	PluginID        string         `json:"plugin_id"`
	Config          map[string]any `json:"config,omitempty"`
}

// ToolCallParams carries a single LLM-driven tool invocation.
type ToolCallParams struct {
	CallID     string          `json:"callId"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args,omitempty"`
	Session    string          `json:"session,omitempty"`
	DeadlineMs int             `json:"deadline_ms,omitempty"`
}

// EventParams delivers a bus event to a subscribed plugin. CallID is set
// only for events whose strategy is modifying or claiming; void events
// omit it.
type EventParams struct {
	CallID  string          `json:"callId,omitempty"`
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data,omitempty"`
	Session string          `json:"session,omitempty"`
}

// ShutdownParams currently has no fields; reserved for future graceful
// shutdown reasons / deadlines (creative §2.3).
type ShutdownParams struct{}

// ----------------------------------------------------------------------------
// Param types — plugin → kernel
// ----------------------------------------------------------------------------

// RegisterParams is the plugin's reply to register_request, listing the
// tools it provides and the bus events it subscribes to.
type RegisterParams struct {
	ProtocolVersion int                `json:"protocol_version"`
	PluginID        string             `json:"plugin_id"`
	Tools           []ToolSpec         `json:"tools"`
	Subscriptions   []SubscriptionSpec `json:"subscriptions"`
}

// ToolSpec is the plugin-declared metadata for a single tool exposed to
// the LLM. DeadlineMs is optional; default 30000ms (creative §2.7).
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	DeadlineMs  int             `json:"deadline_ms,omitempty"`
}

// SubscriptionSpec mirrors kernel.HookSubscription on the wire. TimeoutMs
// follows the same nil/0/positive semantics (kernel.hooks.go).
type SubscriptionSpec struct {
	Event     string `json:"event"`
	Priority  int    `json:"priority"`
	TimeoutMs *int   `json:"timeout_ms,omitempty"`
}

// ToolResultParams replies to a tool_call. Result and Error are mutually
// exclusive; the plugin sends one, the kernel relays it to the caller.
type ToolResultParams struct {
	CallID string          `json:"callId"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// EventReplyParams replies to a modifying/claiming event delivery.
// Action is one of ActionOK/ActionRewrite/ActionDeny/ActionClaim.
type EventReplyParams struct {
	CallID string          `json:"callId"`
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
	Reason string          `json:"reason,omitempty"`
}

// SendParams is the plugin emitting a bus event (analog of Client send).
type SendParams struct {
	Channel string          `json:"channel"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Session string          `json:"session,omitempty"`
}

// LogParams is a structured log line. Per creative §2.5 / D2.12 the
// metric convention reuses Fields with `metric_name`/`metric_value`/
// `metric_kind` keys instead of a separate method.
type LogParams struct {
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

// UpdateToolsParams replaces the plugin's currently-active tool catalog
// (creative §2.5 / D2.10). Tools is the full new authoritative list;
// Removed is an optional convenience hint for kernel diff/log clarity.
type UpdateToolsParams struct {
	Tools   []ToolSpec `json:"tools"`
	Removed []string   `json:"removed,omitempty"`
}

// ----------------------------------------------------------------------------
// NDJSON framing
// ----------------------------------------------------------------------------

// Errors returned by Reader/Writer.
var (
	// ErrLineTooLong is returned when a single NDJSON line exceeds MaxLineSize.
	ErrLineTooLong = errors.New("plugin protocol: line exceeds MaxLineSize")
)

// MalformedError signals that a non-empty NDJSON line could not be
// decoded as a Message. Snippet contains up to the first 256 bytes of the
// offending line per creative §2.6 logging rule.
type MalformedError struct {
	Snippet string
	Err     error
}

func (e *MalformedError) Error() string {
	return fmt.Sprintf("plugin protocol: malformed message: %v (snippet: %q)", e.Err, e.Snippet)
}

func (e *MalformedError) Unwrap() error { return e.Err }

// Reader reads NDJSON-framed Messages from an io.Reader (typically a
// plugin's stdout pipe). Blank lines are skipped per creative §2.1.
type Reader struct {
	sc *bufio.Scanner
}

// NewReader wraps r in a Reader with the protocol's max-line guard.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxLineSize)
	return &Reader{sc: sc}
}

// ReadMessage returns the next Message, skipping blank lines. Returns
// io.EOF at clean stream end. Malformed lines return *MalformedError;
// over-long lines return ErrLineTooLong.
func (r *Reader) ReadMessage() (*Message, error) {
	for r.sc.Scan() {
		line := r.sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, &MalformedError{Snippet: snippet(line, 256), Err: err}
		}
		return &msg, nil
	}
	if err := r.sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, ErrLineTooLong
		}
		return nil, err
	}
	return nil, io.EOF
}

// Writer writes NDJSON-framed Messages. Safe for concurrent use; each
// WriteMessage call holds an internal mutex so two messages can never
// interleave on the wire.
type Writer struct {
	mu sync.Mutex
	bw *bufio.Writer
}

// NewWriter wraps w in a Writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{bw: bufio.NewWriter(w)}
}

// WriteMessage marshals m as NDJSON (one line + '\n') and flushes.
// Returns ErrLineTooLong if the marshalled payload exceeds MaxLineSize.
func (w *Writer) WriteMessage(m *Message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("plugin protocol: marshal message: %w", err)
	}
	if len(raw)+1 > MaxLineSize {
		return ErrLineTooLong
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.bw.Write(raw); err != nil {
		return err
	}
	if err := w.bw.WriteByte('\n'); err != nil {
		return err
	}
	return w.bw.Flush()
}

func snippet(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max])
}
