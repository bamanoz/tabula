package kernel

import (
	"context"
	"fmt"
	"log/slog"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// RuntimeAttachOptions configures the authenticated Runtime API listener path.
type RuntimeAttachOptions struct {
	Auth       runtimeauth.Authenticator
	Logger     *slog.Logger
	RuntimePID int
}

// ServeAuthenticatedRuntime validates the first Hello frame, writes HelloAck,
// registers the accepted runtime in the Hub read model, and then watches the
// post-handshake stream until it disconnects. M2-07 will promote the attached
// connection from read-model evidence to plugin dispatch routing.
func (h *Hub) ServeAuthenticatedRuntime(ctx context.Context, c *codec.Conn, opts RuntimeAttachOptions) error {
	if h == nil {
		return fmt.Errorf("kernel: nil Hub")
	}
	if c == nil {
		return fmt.Errorf("runtime codec connection is nil")
	}
	defer c.CloseNow()

	_, frame, err := c.Read(ctx)
	if err != nil {
		return err
	}
	hello, ok := frame.(*wire.Hello)
	if !ok {
		return fmt.Errorf("runtime handshake expected hello, got %T", frame)
	}
	ack := opts.Auth.HelloAck(*hello)
	if err := c.Write(ctx, ack); err != nil {
		return err
	}
	if !ack.Accepted {
		if opts.Logger != nil {
			opts.Logger.Warn("runtime authentication rejected", "runtime_id", hello.RuntimeID)
		}
		return nil
	}

	if h.runtimes == nil {
		h.runtimes = NewRuntimeRegistry()
	}
	attachedConn := runtimeconn.New(c)
	defer attachedConn.Close()
	if err := h.runtimes.RegisterHello(hello.RuntimeID, hello.Capabilities, opts.RuntimePID); err != nil {
		return err
	}
	if opts.Logger != nil {
		opts.Logger.Info("runtime attached", "runtime_id", hello.RuntimeID)
	}
	select {
	case <-attachedConn.Done():
		err = nil
	case <-ctx.Done():
		err = ctx.Err()
	}
	h.runtimes.MarkDetached(hello.RuntimeID, err)
	if opts.Logger != nil {
		opts.Logger.Info("runtime detached", "runtime_id", hello.RuntimeID)
	}
	return err
}

// SnapshotRuntimes returns a JSON snapshot of Runtime API attachments.
func (h *Hub) SnapshotRuntimes() []byte {
	return snapshotRuntimes(h)
}
