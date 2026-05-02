// Package unixsock provides Runtime API websocket transport over AF_UNIX.
package unixsock

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/coder/websocket"

	"github.com/bamanoz/tabula/internal/runtime/codec"
)

// Listener owns a unix socket listener for runtime daemon connections.
type Listener struct {
	path string
	ln   net.Listener
	srv  *http.Server
}

// Listen creates the parent directory with 0700 mode and binds a unix socket.
func Listen(path string) (*Listener, error) {
	if path == "" {
		return nil, fmt.Errorf("unix socket path is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create runtime socket dir: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return nil, fmt.Errorf("chmod runtime socket dir: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale runtime socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix runtime socket: %w", err)
	}
	if err := chmodSocket(path); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &Listener{path: path, ln: ln}, nil
}

// Handler accepts one websocket Runtime API connection.
type Handler func(context.Context, *codec.Conn)

// Serve accepts websocket Runtime API connections and passes each to handler.
func (l *Listener) Serve(handler Handler) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/runtime", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{codec.Subprotocol},
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		conn := codec.New(ws)
		go handler(context.Background(), conn)
	})
	l.srv = &http.Server{Handler: mux}
	err := l.srv.Serve(l.ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Close shuts down the listener and removes the socket path.
func (l *Listener) Close() error {
	var err error
	if l.srv != nil {
		err = l.srv.Close()
	} else if l.ln != nil {
		err = l.ln.Close()
	}
	if rmErr := os.Remove(l.path); rmErr != nil && !os.IsNotExist(rmErr) && err == nil {
		err = rmErr
	}
	return err
}

// Path returns the bound unix socket path.
func (l *Listener) Path() string { return l.path }

// Dial opens a websocket Runtime API connection to a unix socket.
func Dial(ctx context.Context, url string) (*codec.Conn, error) {
	path, err := socketPath(url)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	httpClient := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", path)
		},
	}}
	conn, _, err := codec.Dial(ctx, "ws://runtime.local/runtime", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func socketPath(url string) (string, error) {
	if strings.HasPrefix(url, "unix://") {
		path := strings.TrimPrefix(url, "unix://")
		if path == "" {
			return "", fmt.Errorf("unix socket path is required")
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return path, nil
	}
	if strings.HasPrefix(url, "/") {
		return url, nil
	}
	return "", fmt.Errorf("unsupported unix socket url %q", url)
}

func chmodSocket(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod runtime socket: %w", err)
	}
	return nil
}
