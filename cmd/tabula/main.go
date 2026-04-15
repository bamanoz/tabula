package main

import (
	"bufio"
	"bytes"
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
	"github.com/bamanoz/tabula/internal/logging"

	"github.com/gorilla/websocket"
)

//go:embed kernel.tools.json
var embeddedToolsJSON []byte

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: checkWebSocketOrigin,
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
	case "serve":
		// Fall through to server mode.
	default:
		// --version is the only flag-only invocation we support.
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Printf("tabula %s (%s) built %s\n", version, commit, date)
			os.Exit(0)
		}
		// No subcommand — print usage.
		fmt.Fprintf(os.Stderr, "Usage: tabula <command>\n\nCommands:\n  serve   Start the kernel WebSocket server (default)\n  run     One-shot prompt → response\n\nFlags:\n  --version   Show version\n")
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
	savedPath := readEnvKey(filepath.Join(tabulaHome, ".env"), "TABULA_PATH")
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
	slog.Info("boot config loaded", "url", bootConfig.URL, "prompt_bytes", len(bootConfig.SystemPrompt), "spawn_count", len(bootConfig.Spawn))

	// Load and merge tools
	var kernelTools []json.RawMessage
	if err := json.Unmarshal(embeddedToolsJSON, &kernelTools); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid embedded kernel.tools.json: %v\n", err)
		return 1
	}
	allTools := make([]json.RawMessage, len(kernelTools))
	copy(allTools, kernelTools)

	skillExec := make(map[string]string)
	if len(bootConfig.Tools) > 0 {
		var bootTools []json.RawMessage
		if err := json.Unmarshal(bootConfig.Tools, &bootTools); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot tools: %v\n", err)
			return 1
		}
		allTools = append(allTools, bootTools...)

		parsed, err := parseSkillExecMap(bootConfig.Tools)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot tools for exec dispatch: %v\n", err)
			return 1
		}
		for _, t := range parsed {
			if t.Exec != "" {
				skillExec[t.Name] = t.Exec
			}
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

	// Set environment for all child processes
	os.Setenv("TABULA_URL", bootConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)

	// Init kernel hub
	slog.Info("initializing kernel")
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(bootConfig.SystemPrompt, toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger.Logger)
	hub.StartReaper()

	// Start HTTP/WebSocket server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err)
			return
		}
		kernel.NewClient(hub, conn)
	})
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(hub.SnapshotSessions())
	})

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen on %s: %v\n", listenAddr, err)
		return 1
	}
	slog.Info("listening", "addr", listenAddr)

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	// Spawn processes from boot config
	for _, cmd := range bootConfig.Spawn {
		slog.Info("spawning boot process", "command", cmd)
		c := mainShellCommand(cmd)
		c.Stdout = nil
		c.Stderr = nil
		if err := c.Start(); err != nil {
			slog.Error("failed to spawn boot process", "command", cmd, "error", err)
			continue
		}
		hub.RegisterSpawn(c, cmd, "main")
		slog.Info("spawned boot process", "pid", c.Process.Pid, "command", cmd)
	}

	slog.Info("ready")

	// Wait for signal
	waitForShutdownSignal()

	slog.Info("shutting down")
	hub.Shutdown()
	server.Close()
	return 0
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
	savedPath := readEnvKey(filepath.Join(tabulaHome, ".env"), "TABULA_PATH")
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

	// Load tools.
	var kernelTools []json.RawMessage
	if err := json.Unmarshal(embeddedToolsJSON, &kernelTools); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid embedded kernel.tools.json: %v\n", err)
		return 1
	}
	allTools := make([]json.RawMessage, len(kernelTools))
	copy(allTools, kernelTools)

	skillExec := make(map[string]string)
	if len(bootConfig.Tools) > 0 {
		var bootTools []json.RawMessage
		if err := json.Unmarshal(bootConfig.Tools, &bootTools); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot tools: %v\n", err)
			return 1
		}
		allTools = append(allTools, bootTools...)

		parsed, err := parseSkillExecMap(bootConfig.Tools)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot tools for exec dispatch: %v\n", err)
			return 1
		}
		for _, t := range parsed {
			if t.Exec != "" {
				skillExec[t.Name] = t.Exec
			}
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
	hub := kernel.NewHub(bootConfig.SystemPrompt, toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger.Logger)
	hub.StartReaper()

	// Start HTTP/WebSocket server (driver needs WebSocket).
	mux := http.NewServeMux()
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

	// Spawn boot processes (driver, MCP, hooks, etc).
	for _, cmd := range bootConfig.Spawn {
		slog.Info("spawning boot process", "command", cmd)
		c := mainShellCommand(cmd)
		c.Stdout = nil
		c.Stderr = nil
		if err := c.Start(); err != nil {
			slog.Error("failed to spawn boot process", "command", cmd, "error", err)
			continue
		}
		hub.RegisterSpawn(c, cmd, "main")
	}

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

// readEnvKey reads a single KEY=VALUE from a .env file.
func readEnvKey(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	prefix := key + "="
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			return line[len(prefix):]
		}
	}
	return ""
}

// BootConfig holds the parsed output of the boot script.
type BootConfig struct {
	URL          string          `json:"url"`
	SystemPrompt string          `json:"system_prompt"`
	Spawn        []string        `json:"spawn"`
	Tools        json.RawMessage `json:"tools"`
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
