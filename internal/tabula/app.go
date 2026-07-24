package tabula

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	"github.com/bamanoz/tabula/internal/logging"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimecodec "github.com/bamanoz/tabula/internal/runtime/codec"
	runtimehostconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/paths"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"

	"github.com/BurntSushi/toml"
	"github.com/gorilla/websocket"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func normalizeBuildInfo(build BuildInfo) BuildInfo {
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

var upgrader = websocket.Upgrader{
	CheckOrigin: checkWebSocketOrigin,
}

const (
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 60 * time.Second
	serverIdleTimeout       = 120 * time.Second
)

type localRuntimeMode string

const (
	localRuntimeModeExternal localRuntimeMode = "external"
	localRuntimeModeManaged  localRuntimeMode = "managed"
	localRuntimeModeDisabled localRuntimeMode = "disabled"
)

type serveOptions struct {
	runtimeMode localRuntimeMode
}

func newKernelHTTPServer(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

func registerKernelHTTPHandlers(mux *http.ServeMux, hub *kernel.Hub, listenerHost string, build BuildInfo) {
	build = normalizeBuildInfo(build)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                      "ok",
			"version":                     build.Version,
			"kernel_version":              build.Version,
			"commit":                      build.Commit,
			"protocol_version":            kernel.ProtocolVersion,
			"min_plugin_protocol_version": kernel.MinPluginProtocolVersion,
			"max_plugin_protocol_version": kernel.MaxPluginProtocolVersion,
		})
	})

	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(hub.SnapshotSessions())
	})

	mux.HandleFunc("/internal/snapshot/runtimes", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(hub.SnapshotRuntimes())
	}))

	mux.HandleFunc("/internal/reload/runtime", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := reloadRuntimeFromConfig(ctx, os.Getenv("TABULA_HOME"), hub); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
}

func internalDiagnosticsGuard(listenerHost string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLocalInternalDiagnosticsRequest(r, listenerHost) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func isLocalInternalDiagnosticsRequest(r *http.Request, listenerHost string) bool {
	if r == nil {
		return false
	}
	remoteHost, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || !isLoopbackHost(remoteHost) {
		return false
	}

	requestHost := normalizeHostOnly(r.Host)
	if isLoopbackHost(requestHost) || strings.EqualFold(requestHost, "localhost") {
		return true
	}

	// If the listener itself was explicitly bound to a loopback host, allow the
	// normalized listener host as an equivalent Host header. Wildcard binds do not
	// authorize public-looking Host values.
	listenerHost = normalizeHostOnly(listenerHost)
	return listenerHost != "" && !isWildcardHost(listenerHost) && isLoopbackHost(listenerHost) && strings.EqualFold(requestHost, listenerHost)
}

func normalizeHostOnly(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(strings.Trim(host, "[]"))
	}
	return strings.ToLower(strings.Trim(raw, "[]"))
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isWildcardHost(host string) bool {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	return host == "" || host == "0.0.0.0" || host == "::"
}

func Run(args []string, build BuildInfo) int {
	build = normalizeBuildInfo(build)
	// Parse subcommand early.
	subcommand := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
	}

	switch subcommand {
	case "run":
		return runCmd(args[1:], build)
	case "status":
		return statusCmd(args[1:])
	case "tenant":
		return tenantCmd(args[1:])
	case "runtime":
		return runtimeCmd(args[1:], build)
	case "config":
		return configCmd(args[1:])
	case "health":
		return healthCmd(args[1:])
	case "serve":
		serveOpts, code := parseServeFlags(args[1:])
		if code != 0 {
			return code
		}
		return serveCmd(build, serveOpts)
	default:
		// --version is the only flag-only invocation we support.
		if len(args) == 1 && args[0] == "--version" {
			fmt.Printf("tabula %s (%s) built %s\n", build.Version, build.Commit, build.Date)
			return 0
		}
		// --protocol prints the kernel's plugin protocol range as JSON. Used
		// by install scripts to write $TABULA_HOME/PROTOCOL so the distro
		// installer can enforce `requires.protocol_version` offline.
		if len(args) == 1 && args[0] == "--protocol" {
			fmt.Printf("{\"plugin_protocol_min\": %d, \"plugin_protocol_max\": %d}\n",
				kernel.MinPluginProtocolVersion, kernel.MaxPluginProtocolVersion)
			return 0
		}
		// No subcommand — print usage.
		fmt.Fprintf(os.Stderr, "Usage: tabula <command>\n\nCommands:\n  serve    Start the kernel WebSocket server (default)\n  run      One-shot prompt → response\n  status   Show kernel/runtime/tenant status\n  config   Inspect runtime configuration\n  health   Check installed plugin health\n  tenant   Manage tenants\n  runtime  Manage runtimes\n\nServe flags:\n  --runtime-mode external|managed|disabled\n\nFlags:\n  --version    Show version\n  --protocol   Show kernel plugin protocol range (JSON)\n")
		return 1
	}

}

func defaultServeOptions() serveOptions {
	return serveOptions{runtimeMode: localRuntimeModeExternal}
}

func parseServeFlags(args []string) (serveOptions, int) {
	opts := defaultServeOptions()
	if mode := strings.TrimSpace(os.Getenv("TABULA_LOCAL_RUNTIME_MODE")); mode != "" {
		parsed, err := parseLocalRuntimeMode(mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return opts, 1
		}
		opts.runtimeMode = parsed
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_ = fs.Bool("foreground", true, "run in foreground")
	runtimeModeFlag := fs.String("runtime-mode", "", "local runtime mode: external, managed, or disabled")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return opts, 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected serve argument %q\n", fs.Arg(0))
		return opts, 1
	}
	if strings.TrimSpace(*runtimeModeFlag) != "" {
		parsed, err := parseLocalRuntimeMode(*runtimeModeFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return opts, 1
		}
		opts.runtimeMode = parsed
	}
	return opts, 0
}

func parseLocalRuntimeMode(value string) (localRuntimeMode, error) {
	switch localRuntimeMode(strings.ToLower(strings.TrimSpace(value))) {
	case localRuntimeModeExternal:
		return localRuntimeModeExternal, nil
	case localRuntimeModeManaged:
		return localRuntimeModeManaged, nil
	case localRuntimeModeDisabled:
		return localRuntimeModeDisabled, nil
	default:
		return "", fmt.Errorf("invalid runtime mode %q (want external, managed, or disabled)", value)
	}
}

// serveCmd handles "tabula serve" — persistent WebSocket server.
func serveCmd(build BuildInfo, opts serveOptions) int {
	// Resolve TABULA_HOME
	tabulaHome := paths.Home()

	// Load .env before reading any other configuration.
	loadEnvFile(filepath.Join(tabulaHome, ".env"))

	// Setup structured logging
	logFile := os.Getenv("TABULA_LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(paths.LogsDir(), "kernel.log")
	}
	logger := logging.Setup(logging.Config{
		ConsoleLevel: os.Getenv("TABULA_LOG_LEVEL"),
		FileLevel:    os.Getenv("TABULA_FILE_LOG_LEVEL"),
		FilePath:     logFile,
		Compress:     true,
	})
	defer logger.Close()

	// chdir to TABULA_HOME
	if err := os.Chdir(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot chdir to %s: %v\n", tabulaHome, err)
		return 1
	}
	slog.Info("working directory", "path", tabulaHome)
	if err := tenant.PrepareBootLayout(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: tenant layout setup failed: %v\n", err)
		return 1
	}

	// Restore full PATH from install-time snapshot
	savedPath := os.Getenv("TABULA_PATH")
	if savedPath != "" {
		os.Setenv("PATH", savedPath)
		slog.Info("PATH restored from TABULA_PATH", "path", savedPath)
	}

	// Plugin layout is owned by runtime.toml (installer-written). Fail
	// fast if it is missing so the operator gets a clear message rather
	// than an obscure "no plugins" downstream error.
	if err := ensureRuntimeConfigExists(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime config not ready: %v\n", err)
		return 1
	}

	kernelConfig, err := loadKernelConfigFile(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel config failed: %v\n", err)
		return 1
	}
	slog.Info("kernel config loaded", "url", kernelConfig.URL)

	// Runtime-owned skills are advertised from runtime capabilities, not boot metadata.
	toolsJSON := json.RawMessage(`[]`)

	// Parse URL to get listen address
	u, err := url.Parse(kernelConfig.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url %q: %v\n", kernelConfig.URL, err)
		return 1
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}
	wsEndpoint := kernelConfig.URL

	// Set environment for all child processes
	os.Setenv("TABULA_URL", kernelConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)
	clientAuthToken, err := kernel.GenerateKernelClientToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel client token generation failed: %v\n", err)
		return 1
	}
	os.Setenv("TABULA_KERNEL_TOKEN", clientAuthToken)

	// Init kernel hub
	slog.Info("initializing kernel")
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(toolsJSON, maxSpawnDepth, maxChildren, logger.Logger)
	hub.SetClientAuthToken(clientAuthToken)
	hub.SetTenantStore(tenant.NewFSStore(tabulaHome))
	hub.SetSessionStore(kernel.NewDiskSessionStore(tabulaHome))
	if err := hub.ConfigureRuntimeRegistry(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime registry config failed: %v\n", err)
		return 1
	}
	if err := hub.ConfigureTenantInitMeta(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: tenant init meta config failed: %v\n", err)
		return 1
	}
	runtimeDefinitions, err := kernel.LoadRuntimeDefinitions(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime registry config failed: %v\n", err)
		return 1
	}
	hub.StartReaper()

	localRuntimeStore := runtimeauth.NewMemoryStore()
	fileRuntimeStore, err := runtimeauth.NewFileStore(runtimeauth.RuntimeTokenStorePath(tabulaHome))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime token store setup failed: %v\n", err)
		return 1
	}
	runtimeStore := runtimeauth.NewChainStore(localRuntimeStore, fileRuntimeStore)
	if opts.runtimeMode != localRuntimeModeDisabled {
		if _, err := runtimeauth.IssueLocalTokenFile(localRuntimeStore, runtimeauth.RuntimeTokenPath(tabulaHome), runtimeauth.LocalRuntimeID, time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "error: runtime token setup failed: %v\n", err)
			return 1
		}
		slog.Info("runtime token issued", "runtime_id", runtimeauth.LocalRuntimeID, "runtime_mode", opts.runtimeMode)
	} else {
		slog.Info("local runtime setup disabled")
	}
	// Start HTTP/WebSocket server
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, listenAddr, build)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err)
			return
		}
		kernel.NewClient(hub, conn)
	})
	runtimeHandler := func(ctx context.Context, c *runtimecodec.Conn) {
		if peerCN := wss.PeerCertCN(ctx); peerCN != "" {
			ctx = runtimeauth.ContextWithPeerCertCN(ctx, peerCN)
		}
		authenticator := runtimeauth.Authenticator{Store: runtimeStore, KernelID: runtimeauth.DefaultKernelID}
		if serveErr := hub.ServeAuthenticatedRuntime(ctx, c, kernel.RuntimeAttachOptions{
			Auth:   authenticator,
			Logger: logger.Logger,
		}); serveErr != nil {
			slog.Warn("runtime connection closed", "error", serveErr)
		}
	}
	var runtimeWSSListener net.Listener
	var runtimeWSSServer *http.Server
	var runtimeWSSTLSConfig *tls.Config
	serveMainTLS := false
	if endpoint, ok := kernelConfig.runtimeWSSEndpoint(); ok {
		runtimeWSSTLSConfig, err = wss.LoadServerTLSConfig(endpoint.CertFile, endpoint.KeyFile, endpoint.ClientCA, wss.ClientCertAuthMode(endpoint.ClientAuth))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid runtime websocket tls config: %v\n", err)
			return 1
		}
		validator := runtimePeerCertValidator(hub)
		if endpoint.Listen == "" || endpoint.Listen == listenAddr {
			wss.Listener{Path: endpoint.Path, Origins: endpoint.Origins, Logger: logger.Logger, ClientCertValidator: validator}.Mount(mux, runtimeHandler)
			serveMainTLS = runtimeWSSTLSConfig != nil
		} else {
			runtimeMux := http.NewServeMux()
			registerKernelHTTPHandlers(runtimeMux, hub, endpoint.Listen, build)
			wss.Listener{Path: endpoint.Path, Origins: endpoint.Origins, Logger: logger.Logger, ClientCertValidator: validator}.Mount(runtimeMux, runtimeHandler)
			runtimeWSSListener, err = net.Listen("tcp", endpoint.Listen)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: cannot listen for runtime websocket connections on %s: %v\n", endpoint.Listen, err)
				return 1
			}
			runtimeWSSServer = newKernelHTTPServer(runtimeMux, runtimeWSSTLSConfig)
			go func() {
				serveListener := runtimeWSSListener
				if runtimeWSSTLSConfig != nil {
					serveListener = tls.NewListener(runtimeWSSListener, runtimeWSSTLSConfig)
				}
				if err := runtimeWSSServer.Serve(serveListener); err != nil && err != http.ErrServerClosed {
					slog.Error("runtime websocket server error", "error", err)
				}
			}()
			slog.Info("runtime websocket listener ready", "addr", endpoint.Listen, "path", endpoint.Path)
		}
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen on %s: %v\n", listenAddr, err)
		if runtimeWSSListener != nil {
			_ = runtimeWSSListener.Close()
		}
		return 1
	}
	slog.Info("listening", "addr", listenAddr)

	runtimeSock, err := managedLocalRuntimeSocketPath(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime listener config failed: %v\n", err)
		listener.Close()
		return 1
	}
	runtimeListener, err := unixsock.Listen(runtimeSock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen for runtime connections: %v\n", err)
		listener.Close()
		return 1
	}
	slog.Info("runtime listener ready", "path", runtimeListener.Path())
	runtimeStop := make(chan struct{})
	var managedRuntimePID atomic.Int64
	go func() {
		serveErr := runtimeListener.Serve(runtimeHandler)
		select {
		case <-runtimeStop:
			// Expected shutdown path.
		default:
			if serveErr != nil {
				slog.Error("runtime listener error", "error", serveErr)
			}
		}
	}()

	server := newKernelHTTPServer(mux, runtimeWSSTLSConfig)
	go func() {
		serveListener := listener
		if serveMainTLS && runtimeWSSTLSConfig != nil {
			serveListener = tls.NewListener(listener, runtimeWSSTLSConfig)
		}
		if err := server.Serve(serveListener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	var runtimeProc *managedLocalRuntime
	switch opts.runtimeMode {
	case localRuntimeModeExternal:
		slog.Info("local runtime external mode enabled", "config", filepath.Join(tabulaHome, "config", "runtime.toml"))
	case localRuntimeModeManaged:
		runtimeProc, err = startAttachedLocalRuntime(hub, tabulaHome, os.Stderr, nil, func(pid int) {
			managedRuntimePID.Store(int64(pid))
		})
		if err != nil {
			close(runtimeStop)
			runtimeListener.Close()
			listener.Close()
			if runtimeWSSServer != nil {
				_ = runtimeWSSServer.Close()
			}
			if runtimeWSSListener != nil {
				_ = runtimeWSSListener.Close()
			}
			server.Close()
			fmt.Fprintf(os.Stderr, "error: local runtime startup failed: %v\n", err)
			return 1
		}
		slog.Info("local runtime managed mode enabled", "pid", runtimeProc.PID())
	default:
		slog.Info("local runtime disabled")
	}
	if err := kernel.WriteKernelClientTokenFile(kernel.KernelClientTokenPath(tabulaHome), clientAuthToken); err != nil {
		close(runtimeStop)
		runtimeListener.Close()
		listener.Close()
		if runtimeWSSServer != nil {
			_ = runtimeWSSServer.Close()
		}
		if runtimeWSSListener != nil {
			_ = runtimeWSSListener.Close()
		}
		server.Close()
		if runtimeProc != nil {
			if err := runtimeProc.Shutdown(5 * time.Second); err != nil {
				slog.Warn("local runtime shutdown failed", "error", err)
			}
		}
		fmt.Fprintf(os.Stderr, "error: kernel client token setup failed: %v\n", err)
		return 1
	}
	if err := writeKernelStatusFiles(tabulaHome, wsEndpoint, runtimeSock, time.Now().UTC()); err != nil {
		close(runtimeStop)
		runtimeListener.Close()
		listener.Close()
		if runtimeWSSServer != nil {
			_ = runtimeWSSServer.Close()
		}
		if runtimeWSSListener != nil {
			_ = runtimeWSSListener.Close()
		}
		server.Close()
		if runtimeProc != nil {
			if err := runtimeProc.Shutdown(5 * time.Second); err != nil {
				slog.Warn("local runtime shutdown failed", "error", err)
			}
		}
		fmt.Fprintf(os.Stderr, "error: kernel status setup failed: %v\n", err)
		return 1
	}
	defer removeKernelStatusFiles(tabulaHome)

	// Watch for distro reinstall reload triggers.
	stopReload := make(chan struct{})
	go watchReloadTrigger(tabulaHome, hub, stopReload)
	stopRuntimeTokenRevokeWatch := make(chan struct{})
	go watchRuntimeTokenRevocations(fileRuntimeStore, hub, stopRuntimeTokenRevokeWatch)
	sshCtx, stopSSH := context.WithCancel(context.Background())
	sshDone := startSSHRuntimeSupervisors(sshCtx, hub, runtimeDefinitions, runtimeauth.Authenticator{Store: runtimeStore, KernelID: runtimeauth.DefaultKernelID}, logger.Logger)

	slog.Info("ready")

	exitCode := 0
	if runtimeProc != nil {
		select {
		case <-shutdownSignalChan():
		case <-runtimeProc.Done():
			if err := runtimeProc.Wait(); err != nil {
				slog.Error("managed local runtime exited", "error", err)
			} else {
				slog.Error("managed local runtime exited")
			}
			exitCode = 1
		}
	} else {
		waitForShutdownSignal()
	}

	slog.Info("shutting down")
	close(stopReload)
	close(stopRuntimeTokenRevokeWatch)
	stopSSH()
	hub.Shutdown()
	close(runtimeStop)
	runtimeListener.Close()
	if runtimeWSSServer != nil {
		_ = runtimeWSSServer.Close()
	}
	if runtimeWSSListener != nil {
		_ = runtimeWSSListener.Close()
	}
	for _, ch := range sshDone {
		<-ch
	}
	server.Close()
	if runtimeProc != nil {
		if err := runtimeProc.Shutdown(5 * time.Second); err != nil {
			slog.Warn("local runtime shutdown failed", "error", err)
		}
	}
	return exitCode
}

// watchReloadTrigger polls TABULA_HOME/run/reload.touch and triggers a runtime
// reload when its mtime changes. Distro install code touches that file after
// switching the active generation so the live kernel picks up new plugin/skill
// code without a manual restart. Polling is intentional: avoids a new
// fsnotify dependency, and 2s granularity is fine for a manual operator action.
func watchReloadTrigger(tabulaHome string, hub *kernel.Hub, stop <-chan struct{}) {
	triggerPath := filepath.Join(tabulaHome, "run", "reload.touch")
	lastMtime := triggerMTime(triggerPath)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		mt := triggerMTime(triggerPath)
		if mt.IsZero() || mt.Equal(lastMtime) {
			continue
		}
		lastMtime = mt
		reloadTenant := reloadTriggerTenant(triggerPath)
		slog.Info("reload trigger fired", "path", triggerPath, "tenant_hint", reloadTenant)
		reloadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := reloadRuntimeFromConfig(reloadCtx, tabulaHome, hub)
		cancel()
		if err != nil {
			slog.Error("runtime reload failed", "error", err)
			continue
		}
		slog.Info("runtime reload complete")
	}
}

func reloadRuntimeFromConfig(ctx context.Context, tabulaHome string, hub *kernel.Hub) error {
	if err := hub.ConfigureRuntimeRegistry(tabulaHome); err != nil {
		return fmt.Errorf("reload runtime registry config: %w", err)
	}
	if err := hub.ConfigureTenantInitMeta(tabulaHome); err != nil {
		return fmt.Errorf("reload tenant init meta: %w", err)
	}
	hub.SetTenantStore(tenant.NewFSStore(tabulaHome))
	return reloadLocalRuntime(ctx, hub, reloadTenants(tabulaHome)...)
}

func watchRuntimeTokenRevocations(store *runtimeauth.FileStore, hub *kernel.Hub, stop <-chan struct{}) {
	if store == nil || hub == nil {
		return
	}
	seen := map[string]time.Time{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		for _, item := range store.List() {
			if item.RevokedAt.IsZero() {
				continue
			}
			if seen[item.RuntimeID].Equal(item.RevokedAt) {
				continue
			}
			seen[item.RuntimeID] = item.RevokedAt
			hub.DetachRuntimeForRevoke(item.RuntimeID)
		}
	}
}

type runtimeReloader interface {
	ReloadAttachedRuntime(context.Context, string, *wire.Target, ...string) (bool, error)
}

func reloadLocalRuntime(ctx context.Context, reloader runtimeReloader, tenants ...string) error {
	if reloader == nil {
		return fmt.Errorf("runtime reloader is required")
	}
	attempted, err := reloader.ReloadAttachedRuntime(ctx, runtimeauth.LocalRuntimeID, nil, tenants...)
	if err != nil {
		return err
	}
	if !attempted {
		return fmt.Errorf("local runtime is not attached")
	}
	return nil
}

func reloadTenants(tabulaHome string) []string {
	cfg, err := runtimehostconfig.Load(filepath.Join(tabulaHome, "config", "runtime.toml"))
	if err != nil || len(cfg.Kernels) != 1 {
		return nil
	}
	for _, tenantID := range cfg.Kernels[0].Tenants {
		if tenantID == "*" {
			return nil
		}
	}
	return append([]string(nil), cfg.Kernels[0].Tenants...)
}

func reloadTriggerTenants(triggerPath, tabulaHome string) []string {
	_ = triggerPath
	return reloadTenants(tabulaHome)
}

func reloadTriggerTenant(triggerPath string) string {
	data, err := os.ReadFile(triggerPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "tenant" {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func triggerMTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// runCmd handles "tabula run" — one-shot prompt → response.
func runCmd(args []string, build BuildInfo) int {
	prompt := ""
	timeout := 120 * time.Second
	asJSON := false

	// Parse flags.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--prompt", "-p":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "--timeout", "-t":
			if i+1 < len(args) {
				d, err := time.ParseDuration(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: invalid timeout %q: %v\n", args[i+1], err)
					return 1
				}
				timeout = d
				i++
			}
		case "--json":
			asJSON = true
		case "--help", "-h":
			fmt.Println("Usage: tabula run [flags]\n\nFlags:\n  -p, --prompt TEXT   Prompt text (reads stdin if omitted)\n  -t, --timeout DUR   Timeout (default: 120s)\n  --json              Output as JSON")
			return 0
		}
	}

	// Read prompt from stdin if not provided.
	if prompt == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reading stdin: %v\n", err)
			return 1
		}
		prompt = strings.TrimSpace(string(data))
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: no prompt provided (use --prompt or pipe stdin)")
		return 1
	}

	// Resolve TABULA_HOME (same as server mode).
	tabulaHome := paths.Home()

	loadEnvFile(filepath.Join(tabulaHome, ".env"))

	// Setup logging.
	logFile := os.Getenv("TABULA_LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(paths.LogsDir(), "kernel.log")
	}
	logger := logging.Setup(logging.Config{
		ConsoleLevel: os.Getenv("TABULA_LOG_LEVEL"),
		FileLevel:    os.Getenv("TABULA_FILE_LOG_LEVEL"),
		FilePath:     logFile,
		Compress:     true,
	})
	defer logger.Close()

	// chdir to TABULA_HOME.
	if err := os.Chdir(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot chdir to %s: %v\n", tabulaHome, err)
		return 1
	}
	if err := tenant.PrepareBootLayout(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: tenant layout setup failed: %v\n", err)
		return 1
	}

	// Restore PATH.
	savedPath := os.Getenv("TABULA_PATH")
	if savedPath != "" {
		os.Setenv("PATH", savedPath)
	}

	if err := ensureRuntimeConfigExists(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime config not ready: %v\n", err)
		return 1
	}

	kernelConfig, err := loadKernelConfigFile(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel config failed: %v\n", err)
		return 1
	}

	// Runtime-owned skills are advertised from runtime capabilities, not boot metadata.
	toolsJSON := json.RawMessage(`[]`)

	// Parse URL.
	u, err := url.Parse(kernelConfig.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url %q: %v\n", kernelConfig.URL, err)
		return 1
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}

	os.Setenv("TABULA_URL", kernelConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)
	os.Setenv("TABULA_SKIP_MCP", "1")
	clientAuthToken, err := kernel.GenerateKernelClientToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel client token generation failed: %v\n", err)
		return 1
	}
	os.Setenv("TABULA_KERNEL_TOKEN", clientAuthToken)

	// Init hub.
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(toolsJSON, maxSpawnDepth, maxChildren, logger.Logger)
	hub.SetClientAuthToken(clientAuthToken)
	hub.SetTenantStore(tenant.NewFSStore(tabulaHome))
	hub.SetSessionStore(kernel.NewDiskSessionStore(tabulaHome))
	if err := hub.ConfigureRuntimeRegistry(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime registry config failed: %v\n", err)
		return 1
	}
	if err := hub.ConfigureTenantInitMeta(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: tenant init meta config failed: %v\n", err)
		return 1
	}
	hub.StartReaper()

	localRuntimeStore := runtimeauth.NewMemoryStore()
	fileRuntimeStore, err := runtimeauth.NewFileStore(runtimeauth.RuntimeTokenStorePath(tabulaHome))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime token store setup failed: %v\n", err)
		return 1
	}
	runtimeStore := runtimeauth.NewChainStore(localRuntimeStore, fileRuntimeStore)
	if _, err := runtimeauth.IssueLocalTokenFile(localRuntimeStore, runtimeauth.RuntimeTokenPath(tabulaHome), runtimeauth.LocalRuntimeID, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime token setup failed: %v\n", err)
		return 1
	}

	runtimeSock, err := managedLocalRuntimeSocketPath(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime listener config failed: %v\n", err)
		return 1
	}
	runtimeListener, err := unixsock.Listen(runtimeSock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen for runtime connections: %v\n", err)
		return 1
	}
	defer runtimeListener.Close()
	runtimeStop := make(chan struct{})
	var managedRuntimePID atomic.Int64
	go func() {
		authenticator := runtimeauth.Authenticator{Store: runtimeStore, KernelID: runtimeauth.DefaultKernelID}
		serveErr := runtimeListener.Serve(func(ctx context.Context, c *runtimecodec.Conn) {
			if serveErr := hub.ServeAuthenticatedRuntime(ctx, c, kernel.RuntimeAttachOptions{
				Auth:   authenticator,
				Logger: logger.Logger,
				RuntimePIDFunc: func(runtimeID string) int {
					if runtimeID != runtimeauth.LocalRuntimeID {
						return 0
					}
					return int(managedRuntimePID.Load())
				},
			}); serveErr != nil {
				slog.Warn("runtime connection closed", "error", serveErr)
			}
		})
		select {
		case <-runtimeStop:
		default:
			if serveErr != nil {
				slog.Error("runtime listener error", "error", serveErr)
			}
		}
	}()

	// Start HTTP/WebSocket server (driver needs WebSocket).
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, listenAddr, build)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err)
			return
		}
		kernel.NewClient(hub, conn)
	})

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen on %s: %v\n", listenAddr, err)
		close(runtimeStop)
		return 1
	}

	server := newKernelHTTPServer(mux, nil)
	defer server.Close()
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	runtimeProc, err := startAttachedLocalRuntime(hub, tabulaHome, os.Stderr, nil, func(pid int) {
		managedRuntimePID.Store(int64(pid))
	})
	if err != nil {
		close(runtimeStop)
		runtimeListener.Close()
		listener.Close()
		fmt.Fprintf(os.Stderr, "error: local runtime startup failed: %v\n", err)
		return 1
	}
	if err := kernel.WriteKernelClientTokenFile(kernel.KernelClientTokenPath(tabulaHome), clientAuthToken); err != nil {
		if shutdownErr := runtimeProc.Shutdown(5 * time.Second); shutdownErr != nil {
			slog.Warn("local runtime shutdown failed", "error", shutdownErr)
		}
		close(runtimeStop)
		runtimeListener.Close()
		listener.Close()
		fmt.Fprintf(os.Stderr, "error: kernel client token setup failed: %v\n", err)
		return 1
	}
	defer func() {
		if err := runtimeProc.Shutdown(5 * time.Second); err != nil {
			slog.Warn("local runtime shutdown failed", "error", err)
		}
		close(runtimeStop)
	}()

	// Wait for any client to join the session.
	clientReady := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			sess, ok := hub.GetSession("main", tenant.DefaultID)
			if ok && sess.ClientCount() >= 1 {
				clientReady <- true
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		clientReady <- false
	}()

	if !<-clientReady {
		fmt.Fprintln(os.Stderr, "error: no client joined session in time")
		return 1
	}

	// Run one-shot exchange.
	result, err := hub.RunOneShot(kernel.OneShotConfig{
		Prompt:  prompt,
		Session: "main",
		Timeout: timeout,
	})

	hub.Shutdown()
	server.Close()

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if asJSON {
		out, _ := json.Marshal(map[string]string{"text": result})
		fmt.Println(string(out))
	} else {
		fmt.Println(result)
	}

	return 0
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func checkWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	allowed := allowedWebSocketOrigins()
	if len(allowed) == 0 {
		return isLocalOrigin(origin, r.Host)
	}
	for _, candidate := range allowed {
		if strings.EqualFold(origin, candidate) {
			return true
		}
	}
	return false
}

func allowedWebSocketOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("TABULA_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLocalOrigin(origin, requestHost string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	if requestHost == "" {
		return false
	}

	reqURL, err := url.Parse("http://" + requestHost)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), reqURL.Hostname())
}

// loadEnvFile loads KEY=VALUE entries from a .env file without overriding shell env.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.TrimSpace(value))
	}
}

type kernelConfigFile struct {
	Kernel     kernelConfigKernel     `toml:"kernel"`
	RuntimeWSS kernelConfigRuntimeWSS `toml:"runtime_wss"`
}

type kernelConfig struct {
	URL        string
	RuntimeWSS *runtimeWSSEndpoint
}

type kernelConfigKernel struct {
	URL string `toml:"url"`
}

type kernelConfigRuntimeWSS struct {
	Enabled    bool     `toml:"enabled"`
	Listen     string   `toml:"listen"`
	Path       string   `toml:"path"`
	Origins    []string `toml:"origins"`
	CertFile   string   `toml:"cert_file"`
	KeyFile    string   `toml:"key_file"`
	ClientCA   string   `toml:"client_ca"`
	ClientAuth string   `toml:"client_auth"`
}

type runtimeWSSEndpoint struct {
	Listen     string
	Path       string
	Origins    []string
	CertFile   string
	KeyFile    string
	ClientCA   string
	ClientAuth string
}

func loadKernelConfigFile(tabulaHome string) (*kernelConfig, error) {
	return loadKernelConfig(filepath.Join(tabulaHome, "config", "kernel.toml"))
}

func loadKernelConfig(path string) (*kernelConfig, error) {
	var cfg kernelConfigFile
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load kernel config %s: %w", path, err)
	}
	url := strings.TrimSpace(os.ExpandEnv(cfg.Kernel.URL))
	if envURL := strings.TrimSpace(os.Getenv("TABULA_URL")); envURL != "" {
		url = envURL
	}
	if url == "" {
		return nil, fmt.Errorf("kernel.url is required in %s", path)
	}
	out := &kernelConfig{URL: url}
	if cfg.RuntimeWSS.Enabled {
		out.RuntimeWSS = &runtimeWSSEndpoint{
			Listen:     strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.Listen)),
			Path:       strings.TrimSpace(cfg.RuntimeWSS.Path),
			Origins:    cfg.RuntimeWSS.Origins,
			CertFile:   strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.CertFile)),
			KeyFile:    strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.KeyFile)),
			ClientCA:   strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.ClientCA)),
			ClientAuth: strings.TrimSpace(cfg.RuntimeWSS.ClientAuth),
		}
	}
	return out, nil
}

func (c *kernelConfig) runtimeWSSEndpoint() (runtimeWSSEndpoint, bool) {
	if c == nil || c.RuntimeWSS == nil {
		return runtimeWSSEndpoint{}, false
	}
	endpoint := *c.RuntimeWSS
	if strings.TrimSpace(endpoint.Path) == "" {
		endpoint.Path = wss.DefaultPath
	}
	return endpoint, true
}

func runtimePeerCertValidator(hub *kernel.Hub) func(*x509.Certificate) error {
	return func(cert *x509.Certificate) error {
		if cert == nil {
			return nil
		}
		cn := strings.TrimSpace(cert.Subject.CommonName)
		if cn == "" {
			return &wss.RejectError{Status: http.StatusUnauthorized, Code: string(wire.ErrorUnknownRuntime), Message: "client certificate common name is required"}
		}
		if hub == nil || !hub.RuntimeConfigured(cn) {
			return &wss.RejectError{Status: http.StatusUnauthorized, Code: string(wire.ErrorUnknownRuntime), Message: fmt.Sprintf("runtime %q is not configured", cn)}
		}
		return nil
	}
}
