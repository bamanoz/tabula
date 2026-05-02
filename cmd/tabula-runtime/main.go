package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	runtimeconfig "github.com/bamanoz/tabula/cmd/tabula-runtime/config"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/daemon"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/dialer"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/manifest"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/policy/bare"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/pool"
)

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

func run(args []string, stderr io.Writer) int {
	subcommand := "start"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	switch subcommand {
	case "start":
		return startCmd(args, stderr)
	case "stdio":
		fmt.Fprintln(stderr, "tabula-runtime stdio is not implemented in M2")
		return 1
	case "help", "--help", "-h":
		printUsage(stderr)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", subcommand)
		printUsage(stderr)
		return 1
	}
}

func startCmd(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to runtime.toml")
	runtimeID := fs.String("runtime-id", dialer.DefaultRuntimeID, "runtime id sent in Hello")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	path := strings.TrimSpace(*configPath)
	if path == "" {
		defaultPath, err := runtimeconfig.DefaultPath()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		path = defaultPath
	}
	cfg, err := runtimeconfig.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	kernelCfg, err := cfg.SingleKernel()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestStore, err := manifest.NewStore(cfg.PluginDirs)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	workerPool := pool.New(kernelCfg.ID, manifestStore, bare.New())

	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer workerPool.Close()
	if err := dialer.Run(ctx, dialer.Options{Kernel: kernelCfg, RuntimeID: *runtimeID, Handler: daemon.NewHandler(daemon.Options{Store: manifestStore, Pool: workerPool}), Logger: logger}); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: tabula-runtime [start] [--config PATH] [--runtime-id ID]")
	fmt.Fprintln(w, "       tabula-runtime stdio")
}
