package dialer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/daemon"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestRunConnectsAndServesAdminOps(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-dialer-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	observed := make(chan struct{})
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
			defer close(observed)
			_, frame, err := c.Read(ctx)
			if err != nil {
				t.Errorf("read hello: %v", err)
				return
			}
			hello, ok := frame.(*wire.Hello)
			if !ok {
				t.Errorf("expected hello, got %T", frame)
				return
			}
			if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
				t.Errorf("unexpected hello: %#v", hello)
			}
			if err := c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
				return
			}
			rc := runtimeconn.New(c)
			health, err := rc.Health(ctx)
			if err != nil || !health.OK || health.WorkerCount != 0 {
				t.Errorf("Health = %#v, %v", health, err)
			}
			caps, err := rc.ListCapabilities(ctx)
			if err != nil || len(caps.Targets) != 0 {
				t.Errorf("ListCapabilities = %#v, %v", caps, err)
			}
			reload, err := rc.Reload(ctx, runtimeapi.ReloadReq{})
			if err != nil || len(reload.EvictedTargets) != 0 {
				t.Errorf("Reload = %#v, %v", reload, err)
			}
			invoke, err := rc.Invoke(ctx, runtimeapi.InvokeReq{CallID: "call-1", TenantID: "default", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read"})
			if err != nil {
				t.Errorf("Invoke: %v", err)
			} else if invoke.OK || invoke.Error == nil || invoke.Error.Code != wire.ErrorInternal || invoke.Error.Message != "worker pool not configured" {
				t.Errorf("unexpected invoke response: %#v", invoke)
			}
		})
	}()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "local", URL: "unix://" + sock, TokenFile: tokenPath},
			Handler:         daemon.NewHandler(),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	waitFor(t, observed, "runtime admin ops")
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after context cancellation")
	}
	listener.Close()
	<-serverDone
}

func TestRunServesManifestBackedInvokeOverUnix(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	pluginDir := filepath.Join(dir, "plugins")
	writeDialerInvokePlugin(t, pluginDir)
	store, err := manifest.NewStore([]string{pluginDir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workerPool := pool.New("main", store, bare.New())
	t.Cleanup(workerPool.Close)

	sock := filepath.Join("/tmp", "tabula-rt-dialer-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	observed := make(chan struct{})
	var observedOnce sync.Once
	signalObserved := func() { observedOnce.Do(func() { close(observed) }) }
	go func() {
		serverDone <- listener.Serve(func(connCtx context.Context, c *codec.Conn) {
			defer signalObserved()
			_, frame, err := c.Read(connCtx)
			if err != nil {
				t.Errorf("read hello: %v", err)
				return
			}
			hello, ok := frame.(*wire.Hello)
			if !ok {
				t.Errorf("expected hello, got %T", frame)
				return
			}
			if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
				t.Errorf("unexpected hello: %#v", hello)
			}
			if err := c.Write(connCtx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
				return
			}
			rc := runtimeconn.New(c)

			caps, err := rc.ListCapabilities(connCtx)
			if err != nil || len(caps.Targets) != 1 || caps.Targets[0].Target.ID != "fs" || len(caps.Targets[0].Tools) != 1 || caps.Targets[0].Tools[0].Name != "read_file" {
				t.Errorf("ListCapabilities = %#v, %v", caps, err)
				return
			}
			invoke, err := rc.Invoke(connCtx, runtimeapi.InvokeReq{CallID: "e2e-1", TenantID: "tenant-e2e", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file", Args: json.RawMessage(`{"path":"README.md"}`)})
			if err != nil || !invoke.OK {
				t.Errorf("Invoke = %#v, %v", invoke, err)
				return
			}
			var data struct {
				Tool       string `json:"tool"`
				TenantEnv  string `json:"tenant_env"`
				TenantInit string `json:"tenant_init"`
			}
			if err := decodeRenderedInvokeData(invoke.Data, &data); err != nil {
				t.Errorf("decode invoke data: %v", err)
				return
			}
			if data.Tool != "read_file" || data.TenantEnv != "tenant-e2e" || data.TenantInit != "tenant-e2e" {
				t.Errorf("unexpected invoke data: %#v", data)
				return
			}
			health, err := rc.Health(connCtx)
			if err != nil || health.WorkerCount != 2 {
				t.Errorf("Health after invoke = %#v, %v", health, err)
				return
			}
			reloadTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
			reload, err := rc.Reload(connCtx, runtimeapi.ReloadReq{Target: &reloadTarget})
			if err != nil || len(reload.EvictedTargets) != 1 || reload.EvictedTargets[0].ID != "fs" {
				t.Errorf("Reload = %#v, %v", reload, err)
				return
			}
			signalObserved()
			<-ctx.Done()
		})
	}()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "local", URL: "unix://" + sock, TokenFile: tokenPath},
			Handler:         daemon.NewHandler(daemon.Options{Store: store, Pool: workerPool}),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	waitFor(t, observed, "manifest-backed runtime invoke")
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after context cancellation")
	}
	listener.Close()
	<-serverDone
}

func TestRunServesManifestBackedInvokeOverWS(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	pluginDir := filepath.Join(dir, "plugins")
	writeDialerInvokePlugin(t, pluginDir)
	store, err := manifest.NewStore([]string{pluginDir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workerPool := pool.New("main", store, bare.New())
	t.Cleanup(workerPool.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan struct{})
	var observedOnce sync.Once
	signalObserved := func() { observedOnce.Do(func() { close(observed) }) }
	mux := http.NewServeMux()
	wss.Listener{}.Mount(mux, func(connCtx context.Context, c *codec.Conn) {
		defer signalObserved()
		_, frame, err := c.Read(connCtx)
		if err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		hello, ok := frame.(*wire.Hello)
		if !ok {
			t.Errorf("expected hello, got %T", frame)
			return
		}
		if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
			t.Errorf("unexpected hello: %#v", hello)
		}
		if err := c.Write(connCtx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
			t.Errorf("write hello_ack: %v", err)
			return
		}
		rc := runtimeconn.New(c)
		caps, err := rc.ListCapabilities(connCtx)
		if err != nil || len(caps.Targets) != 1 || caps.Targets[0].Target.ID != "fs" || len(caps.Targets[0].Tools) != 1 || caps.Targets[0].Tools[0].Name != "read_file" {
			t.Errorf("ListCapabilities = %#v, %v", caps, err)
			return
		}
		invoke, err := rc.Invoke(connCtx, runtimeapi.InvokeReq{CallID: "e2e-ws-1", TenantID: "tenant-e2e", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file", Args: json.RawMessage(`{"path":"README.md"}`)})
		if err != nil || !invoke.OK {
			t.Errorf("Invoke = %#v, %v", invoke, err)
			return
		}
		var data struct {
			Tool       string `json:"tool"`
			TenantEnv  string `json:"tenant_env"`
			TenantInit string `json:"tenant_init"`
		}
		if err := decodeRenderedInvokeData(invoke.Data, &data); err != nil {
			t.Errorf("decode invoke data: %v", err)
			return
		}
		if data.Tool != "read_file" || data.TenantEnv != "tenant-e2e" || data.TenantInit != "tenant-e2e" {
			t.Errorf("unexpected invoke data: %#v", data)
			return
		}
		signalObserved()
		<-ctx.Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "remote", URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/runtime", TokenFile: tokenPath},
			Handler:         daemon.NewHandler(daemon.Options{Store: store, Pool: workerPool}),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	waitFor(t, observed, "manifest-backed runtime invoke over ws")
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after context cancellation")
	}
}

func TestRunServesManifestBackedInvokeOverWSS(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	pluginDir := filepath.Join(dir, "plugins")
	writeDialerInvokePlugin(t, pluginDir)
	store, err := manifest.NewStore([]string{pluginDir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workerPool := pool.New("main", store, bare.New())
	t.Cleanup(workerPool.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan struct{})
	var observedOnce sync.Once
	signalObserved := func() { observedOnce.Do(func() { close(observed) }) }
	mux := http.NewServeMux()
	wss.Listener{}.Mount(mux, func(connCtx context.Context, c *codec.Conn) {
		defer signalObserved()
		_, frame, err := c.Read(connCtx)
		if err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		hello, ok := frame.(*wire.Hello)
		if !ok {
			t.Errorf("expected hello, got %T", frame)
			return
		}
		if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
			t.Errorf("unexpected hello: %#v", hello)
		}
		if err := c.Write(connCtx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
			t.Errorf("write hello_ack: %v", err)
			return
		}
		rc := runtimeconn.New(c)
		invoke, err := rc.Invoke(connCtx, runtimeapi.InvokeReq{CallID: "e2e-wss-1", TenantID: "tenant-e2e", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "read_file", Args: json.RawMessage(`{"path":"README.md"}`)})
		if err != nil || !invoke.OK {
			t.Errorf("Invoke = %#v, %v", invoke, err)
			return
		}
		signalObserved()
		<-ctx.Done()
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "remote", URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/runtime", TokenFile: tokenPath, TLSInsecureSkipVerify: true},
			Handler:         daemon.NewHandler(daemon.Options{Store: store, Pool: workerPool}),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	waitFor(t, observed, "manifest-backed runtime invoke over wss")
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after context cancellation")
	}
}

func TestRunRetriesWithBackoff(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	err := Run(ctx, Options{
		Kernel:    runtimeconfig.Kernel{ID: "local", URL: "unix:///missing.sock", TokenFile: tokenPath},
		Handler:   daemon.NewHandler(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reconnect: true,
		Dial: func(context.Context, string) (*codec.Conn, error) {
			if attempts.Add(1) >= 3 {
				cancel()
			}
			return nil, errors.New("dial failed")
		},
		InitialBackoff: time.Millisecond,
		MaximumBackoff: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := attempts.Load(); got < 3 {
		t.Fatalf("attempts = %d, want at least 3", got)
	}
}

func TestRunReconnectsAfterKernelDisconnect(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-dialer-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var hellos atomic.Int32
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
			defer func() { _ = c.CloseNow() }()
			_, frame, err := c.Read(ctx)
			if err != nil {
				t.Errorf("read hello: %v", err)
				return
			}
			hello, ok := frame.(*wire.Hello)
			if !ok {
				t.Errorf("expected hello, got %T", frame)
				return
			}
			if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
				t.Errorf("unexpected hello: %#v", hello)
			}
			if err := c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
				return
			}
			if hellos.Add(1) >= 2 {
				cancel()
			}
		})
	}()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "local", URL: "unix://" + sock, TokenFile: tokenPath},
			Handler:         daemon.NewHandler(),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			Reconnect:       true,
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-runDone
		t.Fatalf("Run did not reconnect, hellos=%d", hellos.Load())
	}
	if got := hellos.Load(); got < 2 {
		t.Fatalf("hellos = %d, want at least 2", got)
	}
	listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("fake kernel listener did not stop")
	}
}

func TestRunWithoutReconnectExitsAfterKernelDisconnect(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-dialer-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
			defer func() { _ = c.CloseNow() }()
			_, frame, err := c.Read(ctx)
			if err != nil {
				t.Errorf("read hello: %v", err)
				return
			}
			hello, ok := frame.(*wire.Hello)
			if !ok {
				t.Errorf("expected hello, got %T", frame)
				return
			}
			if hello.Token != "secret-token" || hello.RuntimeID != DefaultRuntimeID {
				t.Errorf("unexpected hello: %#v", hello)
			}
			if err := c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
			}
			listener.Close()
		})
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{
			Kernel:          runtimeconfig.Kernel{ID: "local", URL: "unix://" + sock, TokenFile: tokenPath},
			Handler:         daemon.NewHandler(),
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			InitialBackoff:  time.Millisecond,
			MaximumBackoff:  time.Millisecond,
			ShutdownTimeout: time.Second,
		})
	}()

	select {
	case err := <-runDone:
		if err == nil {
			t.Fatal("expected disconnect error when reconnect is disabled")
		}
	case <-time.After(300 * time.Millisecond):
		cancel()
		<-runDone
		t.Fatal("Run did not exit after kernel disconnect with reconnect disabled")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("fake kernel listener did not stop")
	}
}

func TestRunHandshakeRejectionIsFatal(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("bad-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-dialer-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()
	go func() {
		_ = listener.Serve(func(ctx context.Context, c *codec.Conn) {
			_, _, _ = c.Read(ctx)
			_ = c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: false, Error: &wire.Error{Code: wire.ErrorUnauthorized, Retryable: false, Message: "bad token"}})
		})
	}()

	err = Run(context.Background(), Options{
		Kernel:          runtimeconfig.Kernel{ID: "local", URL: "unix://" + sock, TokenFile: tokenPath},
		Handler:         daemon.NewHandler(),
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		InitialBackoff:  time.Millisecond,
		ShutdownTimeout: time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized fatal error, got %v", err)
	}
}

func TestRunRereadsTokenFileBeforeEachRedial(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("old-token\n"), 0o600); err != nil {
		t.Fatalf("write old token: %v", err)
	}
	var attempts atomic.Int32
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	err := Run(ctx, Options{
		Kernel:    runtimeconfig.Kernel{ID: "local", URL: "unix:///ignored.sock", TokenFile: tokenPath},
		Handler:   daemon.NewHandler(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reconnect: true,
		Dial: func(context.Context, string) (*codec.Conn, error) {
			raw, err := os.ReadFile(tokenPath)
			if err != nil {
				t.Fatalf("read token during dial: %v", err)
			}
			seen = append(seen, strings.TrimSpace(string(raw)))
			if attempts.Add(1) == 1 {
				if err := os.WriteFile(tokenPath, []byte("new-token\n"), 0o600); err != nil {
					t.Fatalf("write new token: %v", err)
				}
				return nil, errors.New("first dial failed")
			}
			cancel()
			return nil, errors.New("stop after token reread")
		},
		InitialBackoff: time.Millisecond,
		MaximumBackoff: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(seen) != 2 || seen[0] != "old-token" || seen[1] != "new-token" {
		t.Fatalf("token reads = %#v, want old then new", seen)
	}
}

func TestBackoffCapsAtMaximum(t *testing.T) {
	opts := Options{InitialBackoff: time.Second, MaximumBackoff: 5 * time.Second}
	if got := opts.backoff(0); got != time.Second {
		t.Fatalf("backoff(0) = %v", got)
	}
	if got := opts.backoff(3); got != 5*time.Second {
		t.Fatalf("backoff(3) = %v", got)
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func writeDialerInvokePlugin(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "fs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(`id = "fs"
name = "Filesystem"
version = "0.1.0"
[worker]
command = ["python3", "worker.py"]
mode = "warm"

[[tools]]
name = "read_file"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker.py"), []byte(`#!/usr/bin/env python3
import json
import os
import sys

init = json.loads(sys.stdin.readline())
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [], "subscriptions": []}) + "\n")
sys.stdout.flush()
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    data = {
        "tool": frame.get("tool"),
        "tenant_env": os.environ.get("TABULA_TENANT_ID"),
        "tenant_init": init.get("tenant_id"),
    }
    sys.stdout.write(json.dumps({"op": "result", "call_id": frame.get("call_id"), "ok": True, "data": data}) + "\n")
    sys.stdout.flush()
`), 0o755); err != nil {
		t.Fatalf("write worker: %v", err)
	}
}

func decodeRenderedInvokeData(raw json.RawMessage, out any) error {
	return json.Unmarshal(raw, out)
}
