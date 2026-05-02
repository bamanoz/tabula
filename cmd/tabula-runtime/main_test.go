package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestStdioPlaceholderExitsNonZero(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"stdio"}, &stderr)
	if code == 0 {
		t.Fatal("stdio placeholder unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "not implemented in M2") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"bogus"}, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
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
	var stderr bytes.Buffer
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
