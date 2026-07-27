package hostcmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/layout"
	"github.com/bamanoz/tabula/internal/logging"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
)

type runtimeServiceConfig struct {
	Home          string
	ConfigPath    string
	RuntimeID     string
	Runtime       runtimeconfig.Config
	Kernel        runtimeconfig.Kernel
	ManifestStore *manifest.Store
	Policy        *bare.Policy
	PoolOptions   pool.Options
	Logging       logging.Config
}

type runtimeServiceConfigOptions struct {
	ConfigPath string
	RuntimeID  string
	TokenFile  string
	Stderr     io.Writer
	Now        time.Time
}

func loadRuntimeServiceConfig(opts runtimeServiceConfigOptions) (*runtimeServiceConfig, error) {
	home := layout.Home()
	prepareRuntimeEnvironment(home)
	path, err := resolveRuntimeConfigPath(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	cfg, err := runtimeconfig.Load(path)
	if err != nil {
		return nil, err
	}
	kernelCfg, err := cfg.SingleKernel()
	if err != nil {
		return nil, err
	}
	if tokenFile := strings.TrimSpace(opts.TokenFile); tokenFile != "" {
		kernelCfg.TokenFile = tokenFile
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	resolvedRuntimeID, err := configuredRuntimeID(opts.RuntimeID, now)
	if err != nil {
		return nil, err
	}
	manifestStore, err := newManifestStore(cfg)
	if err != nil {
		return nil, err
	}
	manifestStore.SetTabulaHome(home)
	return &runtimeServiceConfig{
		Home:          home,
		ConfigPath:    path,
		RuntimeID:     resolvedRuntimeID,
		Runtime:       cfg,
		Kernel:        kernelCfg,
		ManifestStore: manifestStore,
		Policy:        runtimePolicy(cfg),
		PoolOptions: pool.Options{
			ColdWorkersPerTenantMax: cfg.Pool.ColdWorkersPerTenantMax,
			ColdWorkersByTenant:     coldWorkerTenantOverrides(cfg.Pool.Tenants),
			AllowedTenants:          kernelCfg.Tenants,
			TabulaHome:              home,
			KernelURL:               workerKernelURL(kernelCfg.URL),
			PythonPath:              runtimePythonPath(cfg, home),
			PluginKindDependsOn:     pluginKindDependencies(cfg.PluginKinds),
		},
		Logging: runtimeLoggingConfig(home, opts.Stderr),
	}, nil
}

func resolveRuntimeConfigPath(configPath string) (string, error) {
	path := strings.TrimSpace(configPath)
	if path != "" {
		return path, nil
	}
	defaultPath, err := runtimeconfig.DefaultPath()
	if err != nil {
		return "", fmt.Errorf("resolve runtime config path: %w", err)
	}
	return defaultPath, nil
}
