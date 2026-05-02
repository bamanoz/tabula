package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
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
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	"github.com/bamanoz/tabula/internal/kernel/plugin"
	"github.com/bamanoz/tabula/internal/logging"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimecodec "github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/transport/unixsock"

	"github.com/gorilla/websocket"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkWebSocketOrigin,
}

func registerKernelHTTPHandlers(mux *http.ServeMux, hub *kernel.Hub, listenerHost string) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                      "ok",
			"version":                     version,
			"kernel_version":              version,
			"commit":                      commit,
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
		w.Write(hub.SnapshotSessions())
	})

	mux.HandleFunc("/internal/snapshot/plugins", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(hub.SnapshotPlugins())
	}))

	mux.HandleFunc("/internal/snapshot/runtimes", internalDiagnosticsGuard(listenerHost, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(hub.SnapshotRuntimes())
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

func main() {
	// Parse subcommand early.
	subcommand := ""
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		subcommand = os.Args[1]
	}

	switch subcommand {
	case "run":
		os.Exit(runCmd(os.Args[2:]))
		return
	case "status":
		os.Exit(statusCmd(os.Args[2:]))
		return
	case "serve":
		// Fall through to server mode.
	default:
		// --version is the only flag-only invocation we support.
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Printf("tabula %s (%s) built %s\n", version, commit, date)
			os.Exit(0)
		}
		// --protocol prints the kernel's plugin protocol range as JSON. Used
		// by install scripts to write $TABULA_HOME/PROTOCOL so the distro
		// installer can enforce `requires.protocol_version` offline.
		if len(os.Args) == 2 && os.Args[1] == "--protocol" {
			fmt.Printf("{\"plugin_protocol_min\": %d, \"plugin_protocol_max\": %d}\n",
				kernel.MinPluginProtocolVersion, kernel.MaxPluginProtocolVersion)
			os.Exit(0)
		}
		// No subcommand — print usage.
		fmt.Fprintf(os.Stderr, "Usage: tabula <command>\n\nCommands:\n  serve   Start the kernel WebSocket server (default)\n  run     One-shot prompt → response\n  status  Show kernel/runtime/tenant status\n\nFlags:\n  --version    Show version\n  --protocol   Show kernel plugin protocol range (JSON)\n")
		os.Exit(1)
	}

	os.Exit(serveCmd())
}

// serveCmd handles "tabula serve" — persistent WebSocket server.
func serveCmd() int {
	// Resolve TABULA_HOME
	tabulaHome := os.Getenv("TABULA_HOME")
	if tabulaHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory")
			return 1
		}
		tabulaHome = filepath.Join(home, ".tabula")
	}

	// Load .env before reading any other configuration.
	loadEnvFile(filepath.Join(tabulaHome, ".env"))

	// Setup structured logging
	logFile := os.Getenv("TABULA_LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(tabulaHome, "logs", "kernel.log")
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

	// Restore full PATH from install-time snapshot
	savedPath := os.Getenv("TABULA_PATH")
	if savedPath != "" {
		os.Setenv("PATH", savedPath)
		slog.Info("PATH restored from TABULA_PATH", "path", savedPath)
	}

	// Resolve boot command
	bootCmd := os.Getenv("TABULA_BOOT")
	if bootCmd == "" {
		fmt.Fprintln(os.Stderr, "error: no boot command specified (set TABULA_BOOT env var)")
		return 1
	}
	slog.Info("running boot", "command", bootCmd)

	// Run boot script → get config
	bootConfig, err := runBoot(bootCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: boot failed: %v\n", err)
		return 1
	}
	slog.Info("boot config loaded", "url", bootConfig.URL)

	// Load static skill tools from the boot contract.
	allTools := make([]json.RawMessage, 0)
	skillExec := make(map[string]string)
	if len(bootConfig.Skills) > 0 && !isJSONEmpty(bootConfig.Skills) {
		bootTools, parsed, err := validateBootSkillTools(bootConfig.Skills)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot skills for exec dispatch: %v\n", err)
			return 1
		}
		allTools = append(allTools, bootTools...)
		for _, t := range parsed {
			skillExec[t.Name] = t.Exec
		}
		slog.Info("merged skill tools", "count", len(bootTools))
	}

	toolsJSON, _ := json.Marshal(allTools)

	// Parse URL to get listen address
	u, err := url.Parse(bootConfig.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url %q: %v\n", bootConfig.URL, err)
		return 1
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}
	wsEndpoint := bootConfig.URL

	// Set environment for all child processes
	os.Setenv("TABULA_URL", bootConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)

	// Init kernel hub
	slog.Info("initializing kernel")
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger.Logger)
	hub.SetInitMeta(bootConfig.Meta)
	hub.ProjectRoot = os.Getenv("TABULA_PROJECT_ROOT")
	hub.StartReaper()

	runtimeStore := runtimeauth.NewMemoryStore()
	if _, err := runtimeauth.IssueLocalTokenFile(runtimeStore, runtimeauth.RuntimeTokenPath(tabulaHome), runtimeauth.LocalRuntimeID, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "error: runtime token setup failed: %v\n", err)
		return 1
	}
	slog.Info("runtime token issued", "runtime_id", runtimeauth.LocalRuntimeID)
	if err := writeKernelStatusFiles(tabulaHome, wsEndpoint, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "error: kernel status setup failed: %v\n", err)
		return 1
	}
	defer removeKernelStatusFiles(tabulaHome)

	// Start HTTP/WebSocket server
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, listenAddr)
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
		return 1
	}
	slog.Info("listening", "addr", listenAddr)

	runtimeListener, err := unixsock.Listen(filepath.Join(tabulaHome, "run", "runtime.sock"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen for runtime connections: %v\n", err)
		listener.Close()
		return 1
	}
	slog.Info("runtime listener ready", "path", runtimeListener.Path())
	runtimeStop := make(chan struct{})
	go func() {
		authenticator := runtimeauth.Authenticator{Store: runtimeStore, KernelID: runtimeauth.DefaultKernelID}
		serveErr := runtimeListener.Serve(func(ctx context.Context, c *runtimecodec.Conn) {
			if serveErr := hub.ServeAuthenticatedRuntime(ctx, c, kernel.RuntimeAttachOptions{Auth: authenticator, Logger: logger.Logger}); serveErr != nil {
				slog.Warn("runtime connection closed", "error", serveErr)
			}
		})
		select {
		case <-runtimeStop:
			// Expected shutdown path.
		default:
			if serveErr != nil {
				slog.Error("runtime listener error", "error", serveErr)
			}
		}
	}()

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	go func() {
		if err := hub.LoadPlugins(bootConfig.Plugins); err != nil {
			slog.Warn("one or more plugins failed to load", "error", err)
		}
	}()

	// Watch for distro reinstall reload triggers.
	stopReload := make(chan struct{})
	go watchReloadTrigger(tabulaHome, bootCmd, hub, stopReload)

	slog.Info("ready")

	// Wait for signal
	waitForShutdownSignal()

	slog.Info("shutting down")
	close(stopReload)
	hub.Shutdown()
	close(runtimeStop)
	runtimeListener.Close()
	server.Close()
	return 0
}

// watchReloadTrigger polls TABULA_HOME/run/reload.touch and triggers a plugin
// reload when its mtime changes. Distro install code touches that file after
// switching the active generation so the live kernel picks up new plugin/skill
// code without a manual restart. Polling is intentional: avoids a new
// fsnotify dependency, and 2s granularity is fine for a manual operator action.
func watchReloadTrigger(tabulaHome, bootCmd string, hub *kernel.Hub, stop <-chan struct{}) {
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
		slog.Info("reload trigger fired; re-running boot", "path", triggerPath)
		bootConfig, err := runBoot(bootCmd)
		if err != nil {
			slog.Error("reload boot failed", "error", err)
			continue
		}
		if err := hub.ReloadPlugins(bootConfig.Plugins); err != nil {
			slog.Warn("one or more plugins failed to reload", "error", err)
		} else {
			slog.Info("plugin reload complete", "count", len(bootConfig.Plugins))
		}
	}
}

func triggerMTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// runCmd handles "tabula run" — one-shot prompt → response.
func runCmd(args []string) int {
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
	tabulaHome := os.Getenv("TABULA_HOME")
	if tabulaHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory")
			return 1
		}
		tabulaHome = filepath.Join(home, ".tabula")
	}

	loadEnvFile(filepath.Join(tabulaHome, ".env"))

	// Setup logging.
	logFile := os.Getenv("TABULA_LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(tabulaHome, "logs", "kernel.log")
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

	// Restore PATH.
	savedPath := os.Getenv("TABULA_PATH")
	if savedPath != "" {
		os.Setenv("PATH", savedPath)
	}

	// Resolve boot command.
	bootCmd := os.Getenv("TABULA_BOOT")
	if bootCmd == "" {
		fmt.Fprintln(os.Stderr, "error: no boot command specified (set TABULA_BOOT env var)")
		return 1
	}
	slog.Info("running boot", "command", bootCmd)

	bootConfig, err := runBoot(bootCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: boot failed: %v\n", err)
		return 1
	}

	// Load static skill tools from the boot contract.
	allTools := make([]json.RawMessage, 0)
	skillExec := make(map[string]string)
	if len(bootConfig.Skills) > 0 && !isJSONEmpty(bootConfig.Skills) {
		bootTools, parsed, err := validateBootSkillTools(bootConfig.Skills)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot skills for exec dispatch: %v\n", err)
			return 1
		}
		allTools = append(allTools, bootTools...)
		for _, t := range parsed {
			skillExec[t.Name] = t.Exec
		}
	}

	toolsJSON, _ := json.Marshal(allTools)

	// Parse URL.
	u, err := url.Parse(bootConfig.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url %q: %v\n", bootConfig.URL, err)
		return 1
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}

	os.Setenv("TABULA_URL", bootConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)
	os.Setenv("TABULA_SKIP_MCP", "1")

	// Init hub.
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger.Logger)
	hub.SetInitMeta(bootConfig.Meta)
	hub.ProjectRoot = os.Getenv("TABULA_PROJECT_ROOT")
	if err := hub.LoadPlugins(bootConfig.Plugins); err != nil {
		slog.Warn("one or more plugins failed to load", "error", err)
	}
	hub.StartReaper()

	// Start HTTP/WebSocket server (driver needs WebSocket).
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, listenAddr)
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
		return 1
	}

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	// Wait for any client to join the session.
	clientReady := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			sess, ok := hub.GetSession("main")
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

type skillToolExec struct {
	Name string `json:"name"`
	Exec string `json:"exec"`
}

func parseSkillExecMap(raw json.RawMessage) ([]skillToolExec, error) {
	var parsed []skillToolExec
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validateBootSkillTools(raw json.RawMessage) ([]json.RawMessage, []skillToolExec, error) {
	var bootTools []json.RawMessage
	if err := json.Unmarshal(raw, &bootTools); err != nil {
		return nil, nil, err
	}
	parsed, err := parseSkillExecMap(raw)
	if err != nil {
		return nil, nil, err
	}
	if len(parsed) != len(bootTools) {
		return nil, nil, fmt.Errorf("skills metadata count mismatch")
	}
	seen := make(map[string]int, len(parsed))
	for i, t := range parsed {
		name := strings.TrimSpace(t.Name)
		exec := strings.TrimSpace(t.Exec)
		if name == "" {
			return nil, nil, fmt.Errorf("skills[%d].name is required", i)
		}
		if exec == "" {
			return nil, nil, fmt.Errorf("skills[%d].exec is required", i)
		}
		if first, ok := seen[name]; ok {
			return nil, nil, fmt.Errorf("skills[%d].name duplicates skills[%d].name %q", i, first, name)
		}
		seen[name] = i
		parsed[i].Name = name
		parsed[i].Exec = exec
	}
	return bootTools, parsed, nil
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

// BootConfig holds the parsed output of the boot script.
type BootConfig struct {
	URL     string             `json:"url"`
	Skills  json.RawMessage    `json:"skills"`
	Plugins []plugin.BootEntry `json:"plugins"`
	Meta    json.RawMessage    `json:"meta"`
}

// isJSONEmpty reports whether the raw JSON value is null, [], or {}.
func isJSONEmpty(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "[]" || s == "{}"
}

// runBoot executes the boot command and parses its JSON output.
func runBoot(cmd string) (*BootConfig, error) {
	c := mainShellCommand(cmd)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		if stderr.Len() > 0 {
			slog.Error("boot script stderr", "output", stderr.String())
		}
		return nil, fmt.Errorf("boot script failed: %v", err)
	}

	if stderr.Len() > 0 {
		slog.Warn("boot script warnings", "output", stderr.String())
	}

	var config BootConfig
	if err := json.Unmarshal(out, &config); err != nil {
		return nil, fmt.Errorf("cannot parse boot output: %v", err)
	}
	return &config, nil
}
