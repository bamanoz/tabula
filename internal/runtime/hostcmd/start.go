package hostcmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/bamanoz/tabula/internal/runtime/host/daemon"
	"github.com/bamanoz/tabula/internal/runtime/host/dialer"
	"github.com/bamanoz/tabula/internal/runtime/host/driver"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
)

func startCmd(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to runtime.toml")
	runtimeID := fs.String("runtime-id", "", "runtime id sent in Hello")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, err := loadRuntimeServiceConfig(runtimeServiceConfigOptions{
		ConfigPath: *configPath,
		RuntimeID:  *runtimeID,
		Stderr:     stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	runtimeLogger := setupRuntimeLogger(cfg.Logging)
	defer runtimeLogger.Close()
	logger := runtimeLogger.Logger
	logger.Info("runtime manifest store loaded", "plugin_dirs", len(runtimePluginDirs(cfg.Runtime)), "skill_dirs", len(runtimeSkillDirs(cfg.Runtime)), "capabilities", len(cfg.ManifestStore.Capabilities()))
	workerPool := pool.New(cfg.Kernel.ID, cfg.ManifestStore, cfg.Policy, cfg.PoolOptions)
	workerPool.SetLogger(logger)
	driverSupervisor := driver.New(cfg.Kernel.ID, cfg.ManifestStore, cfg.Policy, driver.SpawnEnv(cfg.PoolOptions.TabulaHome, cfg.PoolOptions.KernelURL, cfg.PoolOptions.PythonPath))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer workerPool.Close()
	defer driverSupervisor.Close()
	if err := dialer.Run(ctx, dialer.Options{
		Kernel:    cfg.Kernel,
		RuntimeID: cfg.RuntimeID,
		Handler:   daemon.NewHandler(daemon.Options{Store: cfg.ManifestStore, Pool: workerPool, Driver: driverSupervisor}),
		Logger:    logger,
		Reconnect: true,
	}); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
