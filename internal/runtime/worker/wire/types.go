// Package wire defines the runtime-to-worker protocol used after a runtime
// daemon has spawned a skill or plugin worker process.
package wire

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrEOF reports a cleanly closed worker protocol stream.
var ErrEOF = errors.New("worker wire: eof")

// ProtocolError reports malformed worker protocol input.
type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string { return "worker protocol error: " + e.Message }

// ProtocolErrorf creates a ProtocolError with formatted text.
func ProtocolErrorf(format string, args ...any) error {
	return ProtocolError{Message: fmt.Sprintf(format, args...)}
}

// WorkerInit is sent by the runtime as the first frame after worker spawn.
type WorkerInit struct {
	// KernelID identifies the kernel this worker serves.
	KernelID string `json:"kernel_id"`
	// TenantID identifies the tenant context for all worker state/config.
	TenantID string `json:"tenant_id"`
	// TargetID identifies the skill/plugin target this worker serves.
	TargetID string `json:"target_id"`
	// Manifest carries the manifest subset needed by the worker harness.
	Manifest json.RawMessage `json:"manifest,omitempty"`
	// Env carries explicit worker environment values.
	Env map[string]string `json:"env,omitempty"`
}

// WorkerInitAck reports whether the worker is ready to accept calls.
type WorkerInitAck struct {
	// Ready is true when initialization succeeded.
	Ready bool `json:"ready"`
	// Error is set when Ready is false.
	Error *WorkerErrorBody `json:"error,omitempty"`
}

// WorkerCall is a single tool call from runtime to worker.
type WorkerCall struct {
	// CallID correlates this call with its WorkerResult.
	CallID string `json:"call_id"`
	// Tool is the target-local tool name.
	Tool string `json:"tool"`
	// Args is raw JSON so the worker protocol stays tool-schema neutral.
	Args json.RawMessage `json:"args,omitempty"`
}

// WorkerResult is the terminal worker response for one call.
type WorkerResult struct {
	// CallID correlates this result with its WorkerCall.
	CallID string `json:"call_id"`
	// OK reports whether Data contains a successful result.
	OK bool `json:"ok"`
	// Data is raw JSON returned by the worker when OK is true.
	Data json.RawMessage `json:"data,omitempty"`
	// Error is set when OK is false.
	Error *WorkerErrorBody `json:"error,omitempty"`
}

// WorkerShutdown asks the worker to exit cooperatively.
type WorkerShutdown struct {
	// Reason is optional diagnostic text for shutdown.
	Reason string `json:"reason,omitempty"`
}

// WorkerError is an unsolicited async error frame from worker to runtime.
type WorkerError struct {
	// Error contains the protocol/panic diagnostic.
	Error WorkerErrorBody `json:"error"`
}

// WorkerErrorBody describes worker-side errors without using Runtime API wire
// error-code namespace.
type WorkerErrorBody struct {
	// Code is a worker-local error code.
	Code string `json:"code"`
	// Message is human-readable diagnostic text.
	Message string `json:"message,omitempty"`
}

// WriteFrame marshals msg to one NDJSON frame and writes it to w.
func WriteFrame(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ReadFrame reads one NDJSON frame into into. EOF is reported as ErrEOF, while
// malformed JSON is reported as ProtocolError.
func ReadFrame(r *bufio.Reader, into any) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return ErrEOF
		}
		return err
	}
	if err := json.Unmarshal(line, into); err != nil {
		return ProtocolErrorf("decode frame: %v", err)
	}
	return nil
}
