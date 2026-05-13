// Package codec provides the production Runtime API WebSocket JSON codec.
package codec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const (
	// MaxFrameBytes is the M2 runtime API frame limit.
	MaxFrameBytes int64 = 1 << 20
	// IdleReadTimeout is the default per-frame read bound used by tests and
	// callers that do not provide a tighter context.
	IdleReadTimeout = 60 * time.Second
	// WriteTimeout is the default per-frame write bound used by tests and
	// callers that do not provide a tighter context.
	WriteTimeout = 60 * time.Second
	// Subprotocol is the Runtime API websocket subprotocol token.
	Subprotocol = "tabula-runtime.v1"
)

type noIdleTimeoutKey struct{}

// NoIdleTimeoutContext returns a context that disables the codec's default
// idle read timeout. Long-lived Runtime API read loops use this because an idle
// runtime connection is healthy; request-level deadlines still pass explicit
// contexts to Read/Write callers.
func NoIdleTimeoutContext() context.Context {
	return context.WithValue(context.Background(), noIdleTimeoutKey{}, true)
}

// Conn is a validated Runtime API JSON frame codec over one websocket.
type Conn struct {
	ws     *websocket.Conn
	rw     io.ReadWriteCloser
	reader *bufio.Reader
}

// Dial opens a websocket Runtime API connection.
func Dial(ctx context.Context, url string, opts *websocket.DialOptions) (*Conn, *http.Response, error) {
	opts = cloneDialOptions(opts)
	ws, resp, err := websocket.Dial(ctx, url, opts)
	if err != nil {
		return nil, resp, err
	}
	return New(ws), resp, nil
}

// New wraps an accepted or dialed websocket connection.
func New(ws *websocket.Conn) *Conn {
	ws.SetReadLimit(MaxFrameBytes)
	return &Conn{ws: ws}
}

// NewReadWriteCloser wraps a newline-delimited Runtime API JSON stream.
func NewReadWriteCloser(rw io.ReadWriteCloser) *Conn {
	return &Conn{rw: rw, reader: bufio.NewReaderSize(rw, int(MaxFrameBytes))}
}

// Read reads, decodes, and validates one Runtime API frame.
func (c *Conn) Read(ctx context.Context) (wire.Envelope, any, error) {
	raw, err := c.ReadRaw(ctx)
	if err != nil {
		return wire.Envelope{}, nil, err
	}
	return wire.Decode(raw)
}

// ReadRaw reads one Runtime API JSON frame without decoding it. Server-side
// dispatch uses this to convert malformed invoke frames into structured
// protocol_error responses when a call_id can still be recovered.
func (c *Conn) ReadRaw(ctx context.Context) ([]byte, error) {
	ctx, cancel := withDefaultTimeout(ctx, IdleReadTimeout)
	defer cancel()
	if c.rw != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if int64(len(line)) > MaxFrameBytes {
			return nil, fmt.Errorf("runtime frame exceeds %d bytes", MaxFrameBytes)
		}
		return append([]byte(nil), line...), nil
	}

	var raw jsonMessage
	if err := wsjson.Read(ctx, c.ws, &raw); err != nil {
		return nil, err
	}
	return append([]byte(nil), raw...), nil
}

// Write validates and writes one Runtime API frame.
func (c *Conn) Write(ctx context.Context, frame any) error {
	data, err := wire.Encode(frame)
	if err != nil {
		return err
	}
	ctx, cancel := withDefaultTimeout(ctx, WriteTimeout)
	defer cancel()
	if c.rw != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := c.rw.Write(append(data, '\n')); err != nil {
			return err
		}
		return nil
	}
	return wsjson.Write(ctx, c.ws, jsonMessage(data))
}

// Close performs a graceful websocket close.
func (c *Conn) Close(status websocket.StatusCode, reason string) error {
	if c.rw != nil {
		return c.rw.Close()
	}
	return c.ws.Close(status, reason)
}

// CloseNow closes the websocket without a close handshake.
func (c *Conn) CloseNow() error {
	if c.rw != nil {
		return c.rw.Close()
	}
	return c.ws.CloseNow()
}

// WebSocket exposes the underlying websocket for transport-level adapters.
func (c *Conn) WebSocket() *websocket.Conn { return c.ws }

func cloneDialOptions(opts *websocket.DialOptions) *websocket.DialOptions {
	var out websocket.DialOptions
	if opts != nil {
		out = *opts
	}
	out.Subprotocols = append([]string{Subprotocol}, out.Subprotocols...)
	return &out
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx.Value(noIdleTimeoutKey{}) == true {
		return context.WithCancel(ctx)
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

type jsonMessage []byte

func (m jsonMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return nil, fmt.Errorf("empty runtime frame")
	}
	return m, nil
}

func (m *jsonMessage) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}
