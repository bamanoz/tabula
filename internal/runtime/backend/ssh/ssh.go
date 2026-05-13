// Package ssh implements the out-of-process system ssh runtime backend.
package ssh

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
)

type Backend struct {
	Host       string
	SSHCommand []string
	RemoteCmd  []string
	Logger     *slog.Logger
}

type processConn struct {
	*runtimeconn.Conn
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (b Backend) Connect(ctx context.Context) (runtime.RuntimeConn, error) {
	codecConn, pc, err := b.ConnectProcess(ctx)
	if err != nil {
		return nil, err
	}
	pc.Conn = runtimeconn.New(codecConn)
	return pc, nil
}

func (b Backend) ConnectCodec(ctx context.Context) (*codec.Conn, error) {
	c, _, err := b.ConnectProcess(ctx)
	return c, err
}

func (b Backend) ConnectProcess(ctx context.Context) (*codec.Conn, *processConn, error) {
	cmd, err := b.command(ctx)
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	go logStderr(stderr, b.logger())
	codecConn := stdio.NewConn(stdout, stdin)
	return codecConn, &processConn{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (b Backend) command(ctx context.Context) (*exec.Cmd, error) {
	host := strings.TrimSpace(b.Host)
	if host == "" {
		return nil, fmt.Errorf("ssh backend host is required")
	}
	sshCommand := b.SSHCommand
	if len(sshCommand) == 0 {
		sshCommand = []string{"ssh"}
	}
	remoteCmd := b.RemoteCmd
	if len(remoteCmd) == 0 {
		remoteCmd = []string{"tabula-runtime", "stdio"}
	}
	name := sshCommand[0]
	args := append([]string{}, sshCommand[1:]...)
	args = append(args, host)
	args = append(args, remoteCmd...)
	return exec.CommandContext(ctx, name, args...), nil
}

func (c *processConn) Close() error {
	err := c.Conn.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-done:
			timer.Stop()
		case <-timer.C:
			_ = c.cmd.Process.Kill()
			<-done
		}
	}
	return err
}

func (b Backend) logger() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.Default()
}

func logStderr(r io.Reader, logger *slog.Logger) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			logger.Warn("ssh runtime stderr", "output", strings.TrimSpace(string(buf[:n])))
		}
		if err != nil {
			return
		}
	}
}

func CodecFromReadWriteCloser(rw io.ReadWriteCloser) *codec.Conn { return codec.NewReadWriteCloser(rw) }
