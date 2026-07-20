// Package dialer connects tabula-runtime to one configured kernel.
package dialer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const (
	// DefaultRuntimeID is the local daemon id until M4/M6 registry work makes it configurable.
	DefaultRuntimeID = "local"
	// ProtocolVersion is the M2 Runtime API protocol version sent in Hello.
	ProtocolVersion = "1"
)

// DialFunc dials one kernel endpoint.
type DialFunc func(context.Context, string) (*codec.Conn, error)

// Options configures a runtime dial loop.
type Options struct {
	Kernel          runtimeconfig.Kernel
	RuntimeID       string
	Handler         runtimeconn.Handler
	Logger          *slog.Logger
	Dial            DialFunc
	InitialBackoff  time.Duration
	MaximumBackoff  time.Duration
	ShutdownTimeout time.Duration
	Reconnect       bool
}

// Run dials the configured kernel, performs Hello auth, serves Runtime API
// requests, and reconnects on disconnect with exponential backoff until ctx is
// canceled. Token material is read from token_file before every dial attempt.
func Run(ctx context.Context, opts Options) error {
	if err := opts.validate(); err != nil {
		return err
	}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		err := runOnce(ctx, opts)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		if !opts.Reconnect {
			return err
		}
		if isFatalAuthOrConfig(err) {
			return err
		}
		opts.logger().Warn("kernel connection lost; retrying", "kernel_id", opts.Kernel.ID, "error", err, "backoff", opts.backoff(attempt))
		if !sleepContext(ctx, opts.backoff(attempt)) {
			return nil
		}
		attempt++
	}
}

func runOnce(ctx context.Context, opts Options) error {
	token, err := readToken(opts.Kernel.TokenFile)
	if err != nil {
		return authOrConfigError{err: err}
	}
	c, err := opts.dial()(ctx, opts.Kernel.URL)
	if err != nil {
		return err
	}
	defer func() { _ = c.CloseNow() }()
	return serveOnce(ctx, opts, c, token)
}

func serveOnce(ctx context.Context, opts Options, c *codec.Conn, token string) error {
	capabilities := initialCapabilities(ctx, opts.Handler)
	ack, err := runtimeconn.Handshake(ctx, c, wire.Hello{Op: wire.OpHello, RuntimeID: opts.runtimeID(), Token: token, ProtocolVersion: ProtocolVersion, Capabilities: capabilities, TenantsServed: opts.Kernel.Tenants})
	if err != nil {
		return err
	}
	if !ack.Accepted {
		return handshakeRejectedError(ack.Error)
	}
	opts.logger().Info("connected to kernel", "kernel_id", ack.KernelID, "configured_kernel_id", opts.Kernel.ID)

	serveDone := make(chan error, 1)
	go func() { serveDone <- runtimeconn.Serve(context.Background(), c, opts.Handler) }()
	select {
	case err := <-serveDone:
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			err = errors.New("kernel connection closed")
		}
		return err
	case <-ctx.Done():
		_ = c.Close(websocket.StatusNormalClosure, "shutdown")
		timer := time.NewTimer(opts.shutdownTimeout())
		select {
		case <-serveDone:
			timer.Stop()
		case <-timer.C:
			_ = c.CloseNow()
		}
		return nil
	}
}

func initialCapabilities(ctx context.Context, handler runtimeconn.Handler) []wire.Capability {
	if handler == nil {
		return nil
	}
	resp, err := handler.ListCapabilities(ctx, wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil {
		return nil
	}
	return resp.Targets
}

// InitialCapabilitiesForStdio exposes the same Hello capability snapshot for the
// stdio subcommand without widening the public dial loop API.
func InitialCapabilitiesForStdio(ctx context.Context, handler runtimeconn.Handler) []wire.Capability {
	return initialCapabilities(ctx, handler)
}

func ReadTokenFileForStdio(path string) (string, error) { return readToken(path) }

func (o Options) validate() error {
	if strings.TrimSpace(o.Kernel.ID) == "" {
		return fmt.Errorf("kernel id is required")
	}
	if strings.TrimSpace(o.Kernel.URL) == "" {
		return fmt.Errorf("kernel url is required")
	}
	if strings.TrimSpace(o.Kernel.TokenFile) == "" {
		return fmt.Errorf("kernel token_file is required")
	}
	if err := wire.ValidateRuntimeID(o.runtimeID()); err != nil {
		return err
	}
	if o.Handler == nil {
		return fmt.Errorf("runtime handler is required")
	}
	return nil
}

func (o Options) runtimeID() string {
	if strings.TrimSpace(o.RuntimeID) == "" {
		return DefaultRuntimeID
	}
	return strings.TrimSpace(o.RuntimeID)
}

func (o Options) dial() DialFunc {
	if o.Dial != nil {
		return o.Dial
	}
	return defaultDialFunc(o.Kernel)
}

func defaultDialFunc(kernel runtimeconfig.Kernel) DialFunc {
	return func(ctx context.Context, rawURL string) (*codec.Conn, error) {
		rawURL = strings.TrimSpace(rawURL)
		scheme, err := kernelURLScheme(rawURL)
		if err != nil {
			return nil, err
		}
		switch scheme {
		case "ws", "wss":
			return wss.Dial(ctx, rawURL, wss.DialOptions{TLSInsecureSkipVerify: kernel.TLSInsecureSkipVerify, CAFile: kernel.CAFile, CertFile: kernel.CertFile, KeyFile: kernel.KeyFile})
		case "unix", "":
			return unixsock.Dial(ctx, rawURL)
		default:
			return nil, fmt.Errorf("unsupported kernel url scheme %q", scheme)
		}
	}
}

func kernelURLScheme(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(rawURL), "unix://") {
		return "unix", nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse kernel url %q: %w", rawURL, err)
	}
	return strings.ToLower(u.Scheme), nil
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o Options) backoff(attempt int) time.Duration {
	initial := o.InitialBackoff
	if initial <= 0 {
		initial = time.Second
	}
	maximum := o.MaximumBackoff
	if maximum <= 0 {
		maximum = time.Minute
	}
	d := initial
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= maximum {
			return maximum
		}
	}
	if d > maximum {
		return maximum
	}
	return d
}

func (o Options) shutdownTimeout() time.Duration {
	if o.ShutdownTimeout <= 0 {
		return 5 * time.Second
	}
	return o.ShutdownTimeout
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read runtime token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("runtime token file %s is empty", path)
	}
	return token, nil
}

type authOrConfigError struct{ err error }

func (e authOrConfigError) Error() string { return e.err.Error() }
func (e authOrConfigError) Unwrap() error { return e.err }

func handshakeRejectedError(wireErr *wire.Error) error {
	if wireErr == nil {
		return authOrConfigError{err: fmt.Errorf("kernel rejected runtime hello")}
	}
	return authOrConfigError{err: fmt.Errorf("kernel rejected runtime hello with %s: %s", wireErr.Code, wireErr.Message)}
}

func isFatalAuthOrConfig(err error) bool {
	var fatal authOrConfigError
	return errors.As(err, &fatal)
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
