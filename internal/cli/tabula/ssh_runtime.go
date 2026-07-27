package tabula

import (
	"context"
	"log/slog"

	"github.com/bamanoz/tabula/internal/kernel"
	"github.com/bamanoz/tabula/internal/runtime/attach/ssh"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
)

func startSSHRuntimeSupervisors(ctx context.Context, hub *kernel.Hub, defs []runtimeconfig.Definition, auth runtimeauth.Authenticator, logger *slog.Logger) []chan struct{} {
	serve := func(ctx context.Context, conn *codec.Conn) error {
		return hub.ServeAuthenticatedRuntime(ctx, conn, kernel.RuntimeAttachOptions{Auth: auth, Logger: logger})
	}
	return sshattach.StartSupervisors(ctx, sshRuntimeDefinitions(defs), serve, logger)
}

func sshRuntimeDefinitions(defs []runtimeconfig.Definition) []sshattach.Definition {
	out := make([]sshattach.Definition, 0, len(defs))
	for _, def := range defs {
		out = append(out, sshattach.Definition{
			ID:        def.ID,
			Backend:   def.Backend,
			Host:      def.Host,
			Command:   def.Command,
			SSHArgs:   append([]string(nil), def.SSHArgs...),
			RemoteCmd: append([]string(nil), def.RemoteCmd...),
		})
	}
	return out
}
