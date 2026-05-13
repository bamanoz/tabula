package tabula

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/backend/ssh"
	"github.com/bamanoz/tabula/internal/runtime/codec"
)

type sshRuntimeConnector interface {
	connect(context.Context, kernel.RuntimeDefinition) (*codec.Conn, error)
}

type systemSSHRuntimeConnector struct{ logger *slog.Logger }

func (c systemSSHRuntimeConnector) connect(ctx context.Context, def kernel.RuntimeDefinition) (*codec.Conn, error) {
	backend := ssh.Backend{Host: def.Host, SSHCommand: append([]string{"ssh"}, def.SSHArgs...), RemoteCmd: sshRemoteCommand(def), Logger: c.logger}
	return backend.ConnectCodec(ctx)
}

func startSSHRuntimeSupervisors(ctx context.Context, hub *kernel.Hub, defs []kernel.RuntimeDefinition, auth runtimeauth.Authenticator, logger *slog.Logger) []chan struct{} {
	return startSSHRuntimeSupervisorsWithConnector(ctx, hub, defs, auth, logger, systemSSHRuntimeConnector{logger: logger})
}

func startSSHRuntimeSupervisorsWithConnector(ctx context.Context, hub *kernel.Hub, defs []kernel.RuntimeDefinition, auth runtimeauth.Authenticator, logger *slog.Logger, connector sshRuntimeConnector) []chan struct{} {
	if logger == nil {
		logger = slog.Default()
	}
	var done []chan struct{}
	for _, def := range defs {
		if def.Backend != "ssh" {
			continue
		}
		def := def
		ch := make(chan struct{})
		done = append(done, ch)
		go func() {
			defer close(ch)
			attempt := 0
			for {
				if ctx.Err() != nil {
					return
				}
				conn, err := connector.connect(ctx, def)
				if err != nil {
					logger.Warn("ssh runtime connect failed", "runtime_id", def.ID, "error", err)
					if !sshSleepContext(ctx, sshBackoff(attempt)) {
						return
					}
					attempt++
					continue
				}
				attempt = 0
				err = hub.ServeAuthenticatedRuntime(ctx, conn, kernel.RuntimeAttachOptions{Auth: auth, Logger: logger})
				if ctx.Err() != nil {
					return
				}
				logger.Warn("ssh runtime disconnected; reconnecting", "runtime_id", def.ID, "error", err)
				if !sshSleepContext(ctx, sshBackoff(attempt)) {
					return
				}
				attempt++
			}
		}()
	}
	return done
}

func sshRemoteCommand(def kernel.RuntimeDefinition) []string {
	if len(def.RemoteCmd) > 0 {
		return append([]string(nil), def.RemoteCmd...)
	}
	if strings.TrimSpace(def.Command) != "" {
		return strings.Fields(def.Command)
	}
	return nil
}

func sshBackoff(attempt int) time.Duration {
	d := time.Second
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= time.Minute {
			return time.Minute
		}
	}
	return d
}

func sshSleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
