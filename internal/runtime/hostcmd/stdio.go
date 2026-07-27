package hostcmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/host/daemon"
	"github.com/bamanoz/tabula/internal/runtime/host/dialer"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func stdioCmd(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to runtime.toml")
	tokenFile := fs.String("token-file", "", "runtime token file")
	runtimeID := fs.String("runtime-id", "", "runtime id sent in Hello")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, err := loadRuntimeServiceConfig(runtimeServiceConfigOptions{
		ConfigPath: *configPath,
		RuntimeID:  *runtimeID,
		TokenFile:  *tokenFile,
		Stderr:     stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	runtimeLogger := setupRuntimeLogger(cfg.Logging)
	defer runtimeLogger.Close()
	logger := runtimeLogger.Logger
	workerPool := pool.New(cfg.Kernel.ID, cfg.ManifestStore, cfg.Policy, cfg.PoolOptions)
	workerPool.SetLogger(logger)
	defer workerPool.Close()
	conn := stdio.NewConn(os.Stdin, os.Stdout)
	token, err := dialer.ReadTokenFileForStdio(cfg.Kernel.TokenFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	handler := daemon.NewHandler(daemon.Options{Store: cfg.ManifestStore, Pool: workerPool})
	capabilities := dialer.InitialCapabilitiesForStdio(context.Background(), handler)
	ack, err := runtimeconn.Handshake(context.Background(), conn, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       cfg.RuntimeID,
		Token:           token,
		ProtocolVersion: dialer.ProtocolVersion,
		Capabilities:    capabilities,
		TenantsServed:   cfg.Kernel.Tenants,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if !ack.Accepted {
		fmt.Fprintf(stderr, "error: kernel rejected runtime hello\n")
		return 1
	}
	if err := runtimeconn.Serve(context.Background(), conn, handler); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
