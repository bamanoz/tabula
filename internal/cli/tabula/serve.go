package tabula

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	"github.com/bamanoz/tabula/internal/kernel"
	"github.com/bamanoz/tabula/internal/kernel/clientauth"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimecodec "github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/sessionrecord"
	"github.com/bamanoz/tabula/internal/tenant"
)

// kernelSessionDatabaseRelativePath is kernel-owned durable runtime state under TABULA_HOME.
const kernelSessionDatabaseRelativePath = "state/kernel/sessions.db"

type agentSessionRepositorySetter interface {
	SetAgentSessionRepository(agent.SessionRepository)
}

func kernelSessionDatabasePath(tabulaHome string) string {
	return filepath.Join(tabulaHome, filepath.FromSlash(kernelSessionDatabaseRelativePath))
}

func prepareKernelSessionDatabasePath(tabulaHome string) (string, error) {
	path := kernelSessionDatabasePath(tabulaHome)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create kernel session state directory %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

func openSessionRecordStore(ctx context.Context, tabulaHome string) (*sessionrecord.SQLiteStore, error) {
	path, err := prepareKernelSessionDatabasePath(tabulaHome)
	if err != nil {
		return nil, err
	}
	store, err := sessionrecord.OpenSQLiteStore(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open session record store %s: %w", path, err)
	}
	return store, nil
}

func openAgentSessionRepository(ctx context.Context, tabulaHome string) (*agent.SQLiteRepository, error) {
	path, err := prepareKernelSessionDatabasePath(tabulaHome)
	if err != nil {
		return nil, err
	}
	repository, err := agent.OpenSQLiteRepository(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open agent session repository %s: %w", path, err)
	}
	return repository, nil
}

func configureAgentSessionRepository(ctx context.Context, tabulaHome string, hub agentSessionRepositorySetter) (*agent.SQLiteRepository, error) {
	if hub == nil {
		return nil, fmt.Errorf("configure agent session repository: kernel hub is required")
	}
	repository, err := openAgentSessionRepository(ctx, tabulaHome)
	if err != nil {
		return nil, err
	}
	hub.SetAgentSessionRepository(repository)
	return repository, nil
}

func serveCmd(build BuildInfo, opts serveOptions) int {
	cfg, err := loadKernelServiceConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	tabulaHome := cfg.Home
	kernelConfig := cfg.Kernel
	listenAddr := cfg.ListenAddr
	wsEndpoint := cfg.WSEndpoint

	logger := setupKernelLogger(cfg.Logging)
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
	if cfg.SavedPath != "" {
		os.Setenv("PATH", cfg.SavedPath)
		slog.Info("PATH restored from TABULA_PATH", "path", cfg.SavedPath)
	}

	// Plugin layout is owned by runtime.toml (installer-written). Fail
	// fast if it is missing so the operator gets a clear message rather
	// than an obscure "no plugins" downstream error.
	if err := ensureRuntimeConfigExists(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime config not ready: %v\n", err)
		return 1
	}

	slog.Info("kernel config loaded", "url", kernelConfig.URL)

	// Runtime-owned skills are advertised from runtime capabilities, not boot metadata.
	toolsJSON := json.RawMessage(`[]`)

	// Set environment for all child processes
	os.Setenv("TABULA_URL", kernelConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)
	clientAuthToken, err := clientauth.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel client token generation failed: %v\n", err)
		return 1
	}
	os.Setenv("TABULA_KERNEL_TOKEN", clientAuthToken)

	// Init kernel hub
	slog.Info("initializing kernel")
	hub := kernel.NewHub(toolsJSON, logger.Logger)
	hub.SetClientAuthToken(clientAuthToken)
	tenantStore := tenant.NewFSStore(tabulaHome)
	hub.SetTenantStore(tenantStore)
	hub.SetSessionStore(kernel.NewDiskSessionStore(tabulaHome))
	sessionRecordStore, err := openSessionRecordStore(context.Background(), tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: session record store setup failed: %v\n", err)
		return 1
	}
	hub.SetSessionRecordStore(sessionRecordStore)
	defer func() {
		if err := sessionRecordStore.Close(); err != nil {
			slog.Warn("session record store close failed", "error", err)
		}
	}()
	agentSessionRepository, err := configureAgentSessionRepository(context.Background(), tabulaHome, hub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: agent session repository setup failed: %v\n", err)
		return 1
	}
	defer func() {
		if err := agentSessionRepository.Close(); err != nil {
			slog.Warn("agent session repository close failed", "error", err)
		}
	}()
	if err := configureKernelRuntimeRegistry(tabulaHome, hub, tenantStore, opts.runtimeMode == localRuntimeModeManaged); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime registry config failed: %v\n", err)
		return 1
	}
	if err := configureTenantInitMeta(tabulaHome, hub); err != nil {
		fmt.Fprintf(os.Stderr, "error: tenant init meta config failed: %v\n", err)
		return 1
	}
	runtimeDefinitions, err := runtimeconfig.LoadDefinitions(tabulaHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime registry config failed: %v\n", err)
		return 1
	}

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
	registerKernelHTTPHandlers(mux, hub, listenAddr, build, opts.runtimeMode == localRuntimeModeManaged)
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
			registerKernelHTTPHandlers(runtimeMux, hub, endpoint.Listen, build, opts.runtimeMode == localRuntimeModeManaged)
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
		runtimeProc, err = startAttachedLocalRuntime(hub, tabulaHome, os.Stderr, nil, nil)
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
	if err := clientauth.WriteFile(clientauth.Path(tabulaHome), clientAuthToken); err != nil {
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

	var runtimeSupervisorCancel context.CancelFunc
	var runtimeSupervisorDone chan error
	if runtimeProc != nil {
		runtimeSupervisorCtx, cancel := context.WithCancel(context.Background())
		runtimeSupervisorCancel = cancel
		runtimeSupervisorDone = make(chan error, 1)
		go func(initial *managedLocalRuntime) {
			runtimeSupervisorDone <- superviseManagedLocalRuntime(
				runtimeSupervisorCtx,
				initial,
				func() bool { return hub.RuntimeAttached(runtimeauth.LocalRuntimeID) },
				func() (*managedLocalRuntime, error) {
					return startAttachedLocalRuntime(hub, tabulaHome, os.Stderr, nil, nil)
				},
				func(pid int, err error) {
					if err != nil {
						slog.Warn("managed local runtime exited; restarting", "pid", pid, "error", err)
					} else {
						slog.Warn("managed local runtime exited; restarting", "pid", pid)
					}
				},
				func(pid int) {
					slog.Info("managed local runtime restarted", "pid", pid)
				},
				func(err error) {
					slog.Error("managed local runtime restart failed", "error", err)
				},
			)
		}(runtimeProc)
	}

	// Watch for distro reinstall reload triggers.
	stopReload := make(chan struct{})
	go watchReloadTrigger(tabulaHome, hub, opts.runtimeMode == localRuntimeModeManaged, stopReload)
	stopRuntimeTokenRevokeWatch := make(chan struct{})
	go watchRuntimeTokenRevocations(fileRuntimeStore, hub, stopRuntimeTokenRevokeWatch)
	sshCtx, stopSSH := context.WithCancel(context.Background())
	sshDone := startSSHRuntimeSupervisors(sshCtx, hub, runtimeDefinitions, runtimeauth.Authenticator{Store: runtimeStore, KernelID: runtimeauth.DefaultKernelID}, logger.Logger)
	hub.StartAgentLifecycle(context.Background())

	slog.Info("ready")

	exitCode := 0
	if runtimeSupervisorDone != nil {
		select {
		case <-shutdownSignalChan():
			runtimeSupervisorCancel()
			if err := <-runtimeSupervisorDone; err != nil {
				slog.Warn("managed local runtime shutdown failed", "error", err)
			}
		case err := <-runtimeSupervisorDone:
			if err != nil {
				slog.Error("managed local runtime supervisor stopped", "error", err)
			} else {
				slog.Error("managed local runtime supervisor stopped")
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
	return exitCode
}

// watchReloadTrigger polls TABULA_HOME/run/reload.touch and triggers a runtime
// reload when its mtime changes. Distro install code touches that file after
// switching the active generation so the live kernel picks up new plugin/skill
// code without a manual restart. Polling is intentional: avoids a new
