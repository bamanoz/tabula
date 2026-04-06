package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bamanoz/tabula/internal/kernel"

	"github.com/gorilla/websocket"
)

//go:embed kernel.tools.json
var embeddedToolsJSON []byte

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	verbose := flag.Bool("v", false, "verbose logging")
	flag.BoolVar(verbose, "verbose", false, "verbose logging")
	flag.Parse()

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

	// 2. Redirect log to file in verbose mode
	if *verbose {
		logPath := filepath.Join(tabulaHome, "kernel.log")
		f, err := os.Create(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot create log file %s: %v\n", logPath, err)
			os.Exit(1)
		}
		defer f.Close()
		log.SetOutput(f)
		log.SetFlags(log.Ltime | log.Lmicroseconds)
	} else {
		log.SetOutput(os.Stderr)
	}

	// 3. chdir to TABULA_HOME
	if err := os.Chdir(tabulaHome); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot chdir to %s: %v\n", tabulaHome, err)
		os.Exit(1)
	}
	logv(*verbose, "[main] working directory: %s", tabulaHome)

	// 4. Read tabula.yaml → boot command
	configPath := filepath.Join(tabulaHome, "tabula.yaml")
	bootCmd, err := readBootCmd(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	logv(*verbose, "[main] running boot: %s", bootCmd)

	// 5. Run boot script → get config
	bootConfig, err := runBoot(bootCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: boot failed: %v\n", err)
		os.Exit(1)
	}
	logv(*verbose, "[main] url: %s, prompt: %d bytes, spawn: %d processes",
		bootConfig.URL, len(bootConfig.SystemPrompt), len(bootConfig.Spawn))

	// 6. Load tools (embedded)
	var compacted json.RawMessage
	if err := json.Unmarshal(embeddedToolsJSON, &compacted); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid embedded kernel.tools.json: %v\n", err)
		os.Exit(1)
	}
	toolsJSON, _ := json.Marshal(compacted)

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
	// Prepend venv bin to PATH so "python3" resolves to the venv
	venvBin := filepath.Join(tabulaHome, ".venv", "bin")
	if _, err := os.Stat(venvBin); err == nil {
		os.Setenv("PATH", venvBin+":"+os.Getenv("PATH"))
	}

	// 9. Init kernel hub
	logv(*verbose, "[main] initializing kernel...")
	hub := kernel.NewHub(bootConfig.SystemPrompt, toolsJSON, *verbose)
	hub.StartReaper()

	// 9. Start HTTP/WebSocket server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[main] upgrade error: %v", err)
			return
		}
		kernel.NewClient(hub, conn)
	})

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot listen on %s: %v\n", listenAddr, err)
		os.Exit(1)
	}
	logv(*verbose, "[main] listening on %s", listenAddr)

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			log.Printf("[main] server error: %v", err)
		}
	}()

	// 11. Spawn processes from boot config
	for _, cmd := range bootConfig.Spawn {
		logv(*verbose, "[main] spawning: %s", cmd)
		c := exec.Command("sh", "-c", cmd)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			logv(*verbose, "[main] error spawning %q: %v", cmd, err)
			continue
		}
		hub.RegisterSpawn(c, cmd, "main")
		logv(*verbose, "[main] spawned PID %d: %s", c.Process.Pid, cmd)
	}

	logv(*verbose, "[main] ready")

	// 11. Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logv(*verbose, "[main] shutting down...")
	hub.Shutdown()
	server.Close()
}

func logv(verbose bool, format string, args ...any) {
	if verbose {
		log.Printf(format, args...)
	}
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
	URL          string   `json:"url"`
	SystemPrompt string   `json:"system_prompt"`
	Spawn        []string `json:"spawn"`
}

// runBoot executes the boot command and parses its JSON output.
func runBoot(cmd string) (*BootConfig, error) {
	c := exec.Command("sh", "-c", cmd)
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
