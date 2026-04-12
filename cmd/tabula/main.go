package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("tabula %s (%s) built %s\n", version, commit, date)
		os.Exit(0)
	}

	// 1. Resolve TABULA_HOME
	tabulaHome := os.Getenv("TABULA_HOME")
	if tabulaHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory")
			os.Exit(1)
		}
		tabulaHome = filepath.Join(home, ".tabula")
	}

	// 2. Setup structured logging
	logger := logging.Setup(logging.Config{
		ConsoleLevel: os.Getenv("TABULA_LOG_LEVEL"),
		FileLevel:    os.Getenv("TABULA_FILE_LOG_LEVEL"),
		FilePath:     os.Getenv("TABULA_LOG_FILE"),
		Compress:     true,
	})
	defer logger.Close()

	// 3. chdir to TABULA_HOME
	if err := os.Chdir(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot chdir to %s: %v\n", tabulaHome, err)
		os.Exit(1)
	}
	slog.Info("working directory", "path", tabulaHome)

	// 4. Prepend venv bin to PATH so "python3" resolves to the venv
	venvBin := venvBinDir(tabulaHome)
	if _, err := os.Stat(venvBin); err == nil {
		os.Setenv("PATH", venvBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	// 5. Read tabula.yaml → boot command
	configPath := filepath.Join(tabulaHome, "tabula.yaml")
	bootCmd, err := readBootCmd(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("running boot", "command", bootCmd)

	// 5. Run boot script → get config
	bootConfig, err := runBoot(bootCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: boot failed: %v\n", err)
		os.Exit(1)
	}
	slog.Info("boot config loaded", "url", bootConfig.URL, "prompt_bytes", len(bootConfig.SystemPrompt), "spawn_count", len(bootConfig.Spawn))

	// 6. Load and merge tools (embedded kernel tools + boot skill tools)
	var kernelTools []json.RawMessage
	if err := json.Unmarshal(embeddedToolsJSON, &kernelTools); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid embedded kernel.tools.json: %v\n", err)
		os.Exit(1)
	}
	allTools := make([]json.RawMessage, len(kernelTools))
	copy(allTools, kernelTools)

	// Merge skill tools from boot config
	skillExec := make(map[string]string)
	if len(bootConfig.Tools) > 0 {
		var bootTools []json.RawMessage
		if err := json.Unmarshal(bootConfig.Tools, &bootTools); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid boot tools: %v\n", err)
			os.Exit(1)
		}
		allTools = append(allTools, bootTools...)

		// Build exec dispatch map
		var parsed []struct {
			Name string `json:"name"`
			Exec string `json:"exec"`
		}
		json.Unmarshal(bootConfig.Tools, &parsed)
		for _, t := range parsed {
			if t.Exec != "" {
				skillExec[t.Name] = t.Exec
			}
		}
		slog.Info("merged skill tools", "count", len(bootTools))
	}

	toolsJSON, _ := json.Marshal(allTools)

	// 7. Parse URL to get listen address
	u, err := url.Parse(bootConfig.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url %q: %v\n", bootConfig.URL, err)
		os.Exit(1)
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}

	// 8. Set environment for all child processes
	os.Setenv("TABULA_URL", bootConfig.URL)
	os.Setenv("TABULA_HOME", tabulaHome)

	// 9. Init kernel hub
	slog.Info("initializing kernel")
	maxSpawnDepth := envInt("TABULA_MAX_SPAWN_DEPTH", 3)
	maxChildren := envInt("TABULA_MAX_CHILDREN_PER_SESSION", 5)
	hub := kernel.NewHub(bootConfig.SystemPrompt, toolsJSON, skillExec, maxSpawnDepth, maxChildren, logger.Logger)
	hub.StartReaper()

	// 10. Start HTTP/WebSocket server
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
		os.Exit(1)
	}
	slog.Info("listening", "addr", listenAddr)

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	// 11. Spawn processes from boot config
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

	// 12. Wait for signal
	waitForShutdownSignal()

	slog.Info("shutting down")
	hub.Shutdown()
	server.Close()
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

// readBootCmd reads the boot command from tabula.yaml.
func readBootCmd(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if strings.HasPrefix(line, "boot:") {
			val := strings.TrimSpace(line[len("boot:"):])
			if val != "" {
				return val, nil
			}
		}
	}
	return "", fmt.Errorf("tabula.yaml must specify 'boot'")
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
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("boot script failed: %v", err)
	}

	var config BootConfig
	if err := json.Unmarshal(out, &config); err != nil {
		return nil, fmt.Errorf("cannot parse boot output: %v", err)
	}
	return &config, nil
}
