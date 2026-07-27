package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/host/dialer"
	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestStdioRequiresRuntimeConfig(t *testing.T) {
	var stderr bytes.Buffer
	t.Setenv("TABULA_HOME", t.TempDir())
	code := run([]string{"stdio"}, &stderr)
	if code == 0 {
		t.Fatal("stdio unexpectedly succeeded without runtime config")
	}
	if !strings.Contains(stderr.String(), "runtime.toml") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testRuntimeHandler struct{}

func (testRuntimeHandler) Hello(context.Context, wire.Hello) (wire.HelloAck, error) {
	return wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}, nil
}

func (testRuntimeHandler) Invoke(context.Context, wire.Invoke) (wire.InvokeResult, error) {
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: "stdio-1", OK: true, Data: json.RawMessage(`{"stdio":true}`)}, nil
}

func (testRuntimeHandler) Cancel(context.Context, wire.Cancel) (wire.CancelAck, error) {
	return wire.CancelAck{Op: wire.OpCancelAck, CallID: "unused"}, nil
}

func (testRuntimeHandler) Health(context.Context, wire.Health) (wire.HealthResp, error) {
	return wire.HealthResp{Op: wire.OpHealthResp, OK: true}, nil
}

func (testRuntimeHandler) ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	return wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp}, nil
}

func (testRuntimeHandler) Reload(context.Context, wire.Reload) (wire.ReloadAck, error) {
	return wire.ReloadAck{Op: wire.OpReloadAck}, nil
}

func (testRuntimeHandler) HookEvent(context.Context, wire.HookEvent) (wire.HookEventReply, error) {
	return wire.HookEventReply{Op: wire.OpHookEventReply, Action: wire.HookActionOK}, nil
}

func TestStdioRuntimeRoundTripsInvoke(t *testing.T) {
	aRead, bWrite := io.Pipe()
	bRead, aWrite := io.Pipe()
	client := stdio.NewConn(bRead, bWrite)
	runtimeSide := stdio.NewConn(aRead, aWrite)
	defer func() { _ = client.CloseNow() }()
	defer func() { _ = runtimeSide.CloseNow() }()
	handler := testRuntimeHandler{}
	serverDone := make(chan error, 1)
	go func() {
		_, frame, err := runtimeSide.Read(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		hello, ok := frame.(*wire.Hello)
		if !ok || hello.RuntimeID != "remote" {
			serverDone <- fmt.Errorf("unexpected hello: %#v", frame)
			return
		}
		if err := runtimeSide.Write(context.Background(), wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
			serverDone <- err
			return
		}
		serverDone <- runtimeconn.Serve(context.Background(), runtimeSide, handler)
	}()
	ack, err := runtimeconn.Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "remote", Token: "rtk", ProtocolVersion: dialer.ProtocolVersion})
	if err != nil || !ack.Accepted {
		t.Fatalf("Handshake = %#v, %v", ack, err)
	}
	rc := runtimeconn.New(client)
	resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "stdio-1", TenantID: "default", Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tool: "echo", Args: json.RawMessage(`{"ok":true}`)})
	if err != nil || !resp.OK || string(resp.Data) != `{"stdio":true}` {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	_ = client.CloseNow()
	select {
	case err := <-serverDone:
		if err != nil && !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("serverDone = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stdio server")
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"bogus"}, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestVersionFlagPrintsRuntimeVersion(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"--version"}, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "tabula-runtime") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStartSubprocessSIGTERMClosesConnection(t *testing.T) {
	if os.Getenv("TABULA_RUNTIME_HELPER") == "1" {
		return
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "runtime-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-main-"+filepath.Base(dir), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	connected := make(chan struct{})
	closeObserved := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
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
			if hello.Token != "secret-token" || hello.RuntimeID != "local" {
				t.Errorf("unexpected hello: %#v", hello)
			}
			if err := c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
				return
			}
			close(connected)
			_, _, _ = c.Read(ctx)
			close(closeObserved)
		})
	}()

	configPath := filepath.Join(dir, "runtime.toml")
	writeFile(t, configPath, `[[kernel]]
id = "local"
url = "unix://`+sock+`"
token_file = "`+tokenPath+`"
`)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestRuntimeStartHelperProcess", "--", "start", "--config", configPath, "--runtime-id", "local")
	cmd.Env = append(os.Environ(), "TABULA_RUNTIME_HELPER=1")
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	waitFor(t, connected, "runtime hello handshake")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("signal helper: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("helper exit: %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not exit within 5s; stderr=%s", stderr.String())
	}
	waitFor(t, closeObserved, "runtime graceful close")
	listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("fake kernel listener did not stop")
	}
}

func TestStartSubprocessBootstrapsRuntimeMetadataWhenRuntimeIDNotProvided(t *testing.T) {
	if os.Getenv("TABULA_RUNTIME_HELPER") == "1" {
		return
	}
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Cleanup(func() { os.Unsetenv("TABULA_HOME") })
	tokenPath := filepath.Join(home, "run", "runtime-token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	sock := filepath.Join("/tmp", "tabula-rt-main-meta-"+filepath.Base(home), "runtime.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(sock)) })
	listener, err := unixsock.Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	connected := make(chan string, 1)
	closeObserved := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
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
			if hello.Token != "secret-token" {
				t.Errorf("unexpected hello token: %#v", hello)
			}
			connected <- hello.RuntimeID
			if err := c.Write(ctx, wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}); err != nil {
				t.Errorf("write hello_ack: %v", err)
				return
			}
			_, _, _ = c.Read(ctx)
			close(closeObserved)
		})
	}()

	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	configPath := filepath.Join(configDir, "runtime.toml")
	writeFile(t, configPath, `[[kernel]]
id = "local"
url = "unix://`+sock+`"
token_file = "`+tokenPath+`"
`)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestRuntimeStartHelperProcess", "--", "start", "--config", configPath)
	cmd.Env = append(os.Environ(), "TABULA_RUNTIME_HELPER=1", "TABULA_HOME="+home)
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	var observedRuntimeID string
	select {
	case observedRuntimeID = <-connected:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for runtime hello handshake")
	}
	meta, err := runtimeinstance.Load(filepath.Join(home, "run", "runtime-instance.json"))
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("Load metadata: %v", err)
	}
	if observedRuntimeID != meta.RuntimeID {
		_ = cmd.Process.Kill()
		t.Fatalf("hello runtime id = %q, metadata runtime id = %q", observedRuntimeID, meta.RuntimeID)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("signal helper: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("helper exit: %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not exit within 5s; stderr=%s", stderr.String())
	}
	waitFor(t, closeObserved, "runtime graceful close")
	listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("fake kernel listener did not stop")
	}
}

func TestRuntimeStartHelperProcess(t *testing.T) {
	if os.Getenv("TABULA_RUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			os.Exit(run(args[i+1:], os.Stderr))
		}
	}
	os.Exit(2)
}

func waitFor(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
