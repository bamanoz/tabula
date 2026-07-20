package hostcmd

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/daemon"
	"github.com/bamanoz/tabula/internal/runtime/host/dialer"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
	"github.com/bamanoz/tabula/internal/runtime/paths"
	"github.com/bamanoz/tabula/internal/runtime/transport/stdio"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type BuildInfo struct {
	BinaryName string
	Version    string
	Commit     string
	Date       string
}

func normalizeBuildInfo(build BuildInfo) BuildInfo {
	if strings.TrimSpace(build.BinaryName) == "" {
		build.BinaryName = "tabula-runtime"
	}
	if build.Version == "" {
		build.Version = "dev"
	}
	if build.Commit == "" {
		build.Commit = "unknown"
	}
	if build.Date == "" {
		build.Date = "unknown"
	}
	return build
}

func Run(args []string, stderr io.Writer, build BuildInfo) int {
	build = normalizeBuildInfo(build)
	if len(args) == 1 {
		switch args[0] {
		case "--version", "version":
			fmt.Fprintf(stderr, "%s %s (%s) built %s\n", build.BinaryName, build.Version, build.Commit, build.Date)
			return 0
		}
	}
	subcommand := "start"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	switch subcommand {
	case "start":
		return startCmd(args, stderr)
	case "stdio":
		return stdioCmd(args, stderr)
	case "version":
		fmt.Fprintf(stderr, "%s %s (%s) built %s\n", build.BinaryName, build.Version, build.Commit, build.Date)
		return 0
	case "help", "--help", "-h":
		printUsage(stderr, build.BinaryName)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", subcommand)
		printUsage(stderr, build.BinaryName)
		return 1
	}
}

func stdioCmd(args []string, stderr io.Writer) int {
	prepareRuntimeEnvironment(paths.Home())
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to runtime.toml")
	tokenFile := fs.String("token-file", "", "runtime token file")
	runtimeID := fs.String("runtime-id", "", "runtime id sent in Hello")
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
	if strings.TrimSpace(*tokenFile) != "" {
		kernelCfg.TokenFile = strings.TrimSpace(*tokenFile)
	}
	resolvedRuntimeID, err := configuredRuntimeID(*runtimeID, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestStore, err := newManifestStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestStore.SetTabulaHome(paths.Home())
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	workerPool := pool.New(kernelCfg.ID, manifestStore, runtimePolicy(cfg), pool.Options{
		ColdWorkersPerTenantMax: cfg.Pool.ColdWorkersPerTenantMax,
		ColdWorkersByTenant:     coldWorkerTenantOverrides(cfg.Pool.Tenants),
		AllowedTenants:          kernelCfg.Tenants,
		TabulaHome:              paths.Home(),
		KernelURL:               workerKernelURL(kernelCfg.URL),
		PluginKindDependsOn:     pluginKindDependencies(cfg.PluginKinds),
	})
	workerPool.SetLogger(logger)
	defer workerPool.Close()
	conn := stdio.NewConn(os.Stdin, os.Stdout)
	token, err := dialer.ReadTokenFileForStdio(kernelCfg.TokenFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	handler := daemon.NewHandler(daemon.Options{Store: manifestStore, Pool: workerPool})
	capabilities := dialer.InitialCapabilitiesForStdio(context.Background(), handler)
	ack, err := runtimeconn.Handshake(context.Background(), conn, wire.Hello{
		Op:              wire.OpHello,
		RuntimeID:       resolvedRuntimeID,
		Token:           token,
		ProtocolVersion: dialer.ProtocolVersion,
		Capabilities:    capabilities,
		TenantsServed:   kernelCfg.Tenants,
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

func startCmd(args []string, stderr io.Writer) int {
	prepareRuntimeEnvironment(paths.Home())
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to runtime.toml")
	runtimeID := fs.String("runtime-id", "", "runtime id sent in Hello")
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
	resolvedRuntimeID, err := configuredRuntimeID(*runtimeID, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestStore, err := newManifestStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	manifestStore.SetTabulaHome(paths.Home())
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("runtime manifest store loaded", "plugin_dirs", len(runtimePluginDirs(cfg)), "skill_dirs", len(runtimeSkillDirs(cfg)), "capabilities", len(manifestStore.Capabilities()))
	workerPool := pool.New(kernelCfg.ID, manifestStore, runtimePolicy(cfg), pool.Options{
		ColdWorkersPerTenantMax: cfg.Pool.ColdWorkersPerTenantMax,
		ColdWorkersByTenant:     coldWorkerTenantOverrides(cfg.Pool.Tenants),
		AllowedTenants:          kernelCfg.Tenants,
		TabulaHome:              paths.Home(),
		KernelURL:               workerKernelURL(kernelCfg.URL),
		PluginKindDependsOn:     pluginKindDependencies(cfg.PluginKinds),
	})
	workerPool.SetLogger(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer workerPool.Close()
	if err := dialer.Run(ctx, dialer.Options{
		Kernel:    kernelCfg,
		RuntimeID: resolvedRuntimeID,
		Handler:   daemon.NewHandler(daemon.Options{Store: manifestStore, Pool: workerPool}),
		Logger:    logger,
		Reconnect: true,
	}); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func configuredRuntimeID(explicit string, now time.Time) (string, error) {
	if runtimeID := strings.TrimSpace(explicit); runtimeID != "" {
		return runtimeID, nil
	}
	meta, err := runtimeinstance.Ensure(paths.RuntimeInstanceFile(), now)
	if err != nil {
		return "", fmt.Errorf("resolve runtime instance metadata: %w", err)
	}
	return meta.RuntimeID, nil
}

func workerKernelURL(configured string) string {
	if envURL := strings.TrimSpace(os.Getenv("TABULA_URL")); envURL != "" {
		return envURL
	}
	return strings.TrimSpace(configured)
}

func prepareRuntimeEnvironment(tabulaHome string) {
	loadRuntimeEnvFile(filepath.Join(tabulaHome, ".env"))
	if savedPath := strings.TrimSpace(os.Getenv("TABULA_PATH")); savedPath != "" {
		_ = os.Setenv("PATH", savedPath)
	}
}

func loadRuntimeEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	for scanner := bufio.NewScanner(f); scanner.Scan(); {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, strings.TrimSpace(value))
	}
}

func printUsage(w io.Writer, binaryName string) {
	fmt.Fprintf(w, "Usage: %s [start] [--config PATH] [--runtime-id ID]\n", binaryName)
	fmt.Fprintf(w, "       %s stdio\n", binaryName)
	fmt.Fprintf(w, "       %s --version\n", binaryName)
}

func coldWorkerTenantOverrides(in map[string]runtimeconfig.TenantPool) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for tenantID, limits := range in {
		if limits.ColdWorkersMax > 0 {
			out[tenantID] = limits.ColdWorkersMax
		}
	}
	return out
}

func pluginKindDependencies(in map[string]runtimeconfig.PluginKind) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for kind, policy := range in {
		if len(policy.DependsOn) > 0 {
			out[kind] = append([]string(nil), policy.DependsOn...)
		}
	}
	return out
}

func runtimePolicy(cfg runtimeconfig.Config) *bare.Policy {
	commands := make(map[string][]string, len(cfg.Runtimes))
	for name, runtime := range cfg.Runtimes {
		commands[name] = append([]string(nil), runtime.Command...)
	}
	return &bare.Policy{RuntimeCommands: commands}
}

func newManifestStore(cfg runtimeconfig.Config) (*manifest.Store, error) {
	if len(cfg.Tenants) == 0 {
		return manifest.NewSearchStore(cfg.PluginDirs, cfg.SkillDirs)
	}
	catalogs := make(map[string]manifest.SearchDirs, len(cfg.Tenants))
	for _, tenant := range cfg.Tenants {
		catalogs[tenant.ID] = manifest.SearchDirs{PluginDirs: tenant.PluginDirs, SkillDirs: tenant.SkillDirs}
	}
	return manifest.NewTenantStore(catalogs)
}

func runtimePluginDirs(cfg runtimeconfig.Config) []string {
	if len(cfg.Tenants) == 0 {
		return cfg.PluginDirs
	}
	var out []string
	for _, tenant := range cfg.Tenants {
		out = append(out, tenant.PluginDirs...)
	}
	return out
}

func runtimeSkillDirs(cfg runtimeconfig.Config) []string {
	if len(cfg.Tenants) == 0 {
		return cfg.SkillDirs
	}
	var out []string
	for _, tenant := range cfg.Tenants {
		out = append(out, tenant.SkillDirs...)
	}
	return out
}

// Compatibility wrappers keep the legacy cmd/tabula-runtime tests working
// while the runtime host implementation lives in this shared package.
func ConfiguredRuntimeID(explicit string, now time.Time) (string, error) {
	return configuredRuntimeID(explicit, now)
}

func WorkerKernelURL(configured string) string {
	return workerKernelURL(configured)
}

func PrepareRuntimeEnvironment(tabulaHome string) {
	prepareRuntimeEnvironment(tabulaHome)
}

func NewManifestStore(cfg runtimeconfig.Config) (*manifest.Store, error) {
	return newManifestStore(cfg)
}

func RuntimePluginDirs(cfg runtimeconfig.Config) []string {
	return runtimePluginDirs(cfg)
}
