package sshattach

import (
	"context"
	"log/slog"
	"strings"
	"time"

	backendssh "github.com/bamanoz/tabula/internal/runtime/backend/ssh"
	"github.com/bamanoz/tabula/internal/runtime/codec"
)

type Definition struct {
	ID        string
	Backend   string
	Host      string
	Command   string
	SSHArgs   []string
	RemoteCmd []string
}

type ServeFunc func(context.Context, *codec.Conn) error

type Connector interface {
	Connect(context.Context, Definition) (*codec.Conn, error)
}

type SystemConnector struct{ Logger *slog.Logger }

func (c SystemConnector) Connect(ctx context.Context, def Definition) (*codec.Conn, error) {
	backend := backendssh.Backend{
		Host:       def.Host,
		SSHCommand: append([]string{"ssh"}, def.SSHArgs...),
		RemoteCmd:  remoteCommand(def),
		Logger:     c.Logger,
	}
	return backend.ConnectCodec(ctx)
}

func StartSupervisors(ctx context.Context, defs []Definition, serve ServeFunc, logger *slog.Logger) []chan struct{} {
	return StartSupervisorsWithConnector(ctx, defs, serve, logger, SystemConnector{Logger: logger})
}

func StartSupervisorsWithConnector(ctx context.Context, defs []Definition, serve ServeFunc, logger *slog.Logger, connector Connector) []chan struct{} {
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
				conn, err := connector.Connect(ctx, def)
				if err != nil {
					logger.Warn("ssh runtime connect failed", "runtime_id", def.ID, "error", err)
					if !sleepContext(ctx, backoff(attempt)) {
						return
					}
					attempt++
					continue
				}
				attempt = 0
				err = serve(ctx, conn)
				if ctx.Err() != nil {
					return
				}
				logger.Warn("ssh runtime disconnected; reconnecting", "runtime_id", def.ID, "error", err)
				if !sleepContext(ctx, backoff(attempt)) {
					return
				}
				attempt++
			}
		}()
	}
	return done
}

func remoteCommand(def Definition) []string {
	if len(def.RemoteCmd) > 0 {
		return append([]string(nil), def.RemoteCmd...)
	}
	if strings.TrimSpace(def.Command) != "" {
		return strings.Fields(def.Command)
	}
	return nil
}

func backoff(attempt int) time.Duration {
	d := time.Second
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= time.Minute {
			return time.Minute
		}
	}
	return d
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
