package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	statusExitOK         = 0
	statusExitCannotRead = 2
	statusFilename       = "kernel-status.json"
	pidFilename          = "kernel.pid"
)

type statusKernel struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	Socket        string `json:"socket,omitempty"`
	WSEndpoint    string `json:"ws_endpoint,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
}

type statusRuntime struct {
	ID           string   `json:"id"`
	Attached     bool     `json:"attached"`
	PID          int      `json:"pid"`
	Capabilities []string `json:"capabilities"`
}

type statusTenant struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

type statusDocument struct {
	Kernel   statusKernel    `json:"kernel"`
	Runtimes []statusRuntime `json:"runtimes"`
	Tenants  []statusTenant  `json:"tenants"`
}

type kernelStatusFile struct {
	PID           int    `json:"pid"`
	RuntimeSocket string `json:"runtime_socket"`
	WSEndpoint    string `json:"ws_endpoint"`
	StartedAt     string `json:"started_at"`
}

type runtimeSnapshotFile struct {
	Runtimes []struct {
		ID           string   `json:"id"`
		Attached     bool     `json:"attached"`
		PID          int      `json:"pid"`
		Capabilities []string `json:"capabilities"`
		WorkerCount  int      `json:"worker_count,omitempty"`
		LastError    *string  `json:"last_error,omitempty"`
		ConnectedAt  string   `json:"connected_at,omitempty"`
	} `json:"runtimes"`
}

func statusCmd(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return statusExitCannotRead
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", fs.Arg(0))
		return statusExitCannotRead
	}

	tabulaHome, err := resolveTabulaHome()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return statusExitCannotRead
	}
	doc, err := buildStatus(context.Background(), tabulaHome, http.DefaultClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return statusExitCannotRead
	}
	if *asJSON {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return statusExitCannotRead
		}
		fmt.Println(string(data))
		return statusExitOK
	}
	printHumanStatus(os.Stdout, doc)
	return statusExitOK
}

func resolveTabulaHome() (string, error) {
	if tabulaHome := strings.TrimSpace(os.Getenv("TABULA_HOME")); tabulaHome != "" {
		return tabulaHome, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory")
	}
	return filepath.Join(home, ".tabula"), nil
}

func buildStatus(ctx context.Context, tabulaHome string, client *http.Client) (statusDocument, error) {
	if client == nil {
		client = http.DefaultClient
	}
	state, err := readKernelStatusFile(tabulaHome)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return statusDocument{}, err
	}
	running := state.PID > 0 && processRunning(state.PID)
	doc := statusDocument{
		Kernel: statusKernel{
			Running:    running,
			PID:        state.PID,
			Socket:     state.RuntimeSocket,
			WSEndpoint: state.WSEndpoint,
		},
		Runtimes: []statusRuntime{},
	}
	if running {
		doc.Kernel.UptimeSeconds = uptimeSeconds(state.StartedAt, time.Now().UTC())
		if runtimes, err := fetchRuntimeStatus(ctx, client, state.WSEndpoint); err == nil {
			doc.Runtimes = runtimes
		} else {
			// If the pidfile is stale enough that the loopback snapshot cannot be read,
			// treat the kernel as not running. `status` still succeeds and reports
			// tenant state from disk, per M2-06.
			doc.Kernel.Running = false
			doc.Kernel.UptimeSeconds = 0
		}
	}
	tenants, err := readTenants(tabulaHome)
	if err != nil {
		return statusDocument{}, err
	}
	doc.Tenants = tenants
	return doc, nil
}

func readKernelStatusFile(tabulaHome string) (kernelStatusFile, error) {
	path := filepath.Join(tabulaHome, "run", statusFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		// M2-06 also writes a pidfile; tolerate status files created by older test
		// fixtures by falling back to the pidfile when the richer state is absent.
		if errors.Is(err, os.ErrNotExist) {
			pid, pidErr := readPIDFile(filepath.Join(tabulaHome, "run", pidFilename))
			if pidErr != nil {
				return kernelStatusFile{}, err
			}
			return kernelStatusFile{PID: pid, RuntimeSocket: filepath.Join(tabulaHome, "run", "runtime.sock")}, nil
		}
		return kernelStatusFile{}, err
	}
	var state kernelStatusFile
	if err := json.Unmarshal(data, &state); err != nil {
		return kernelStatusFile{}, fmt.Errorf("read kernel status: %w", err)
	}
	return state, nil
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("read kernel pid: %w", err)
	}
	return pid, nil
}

func fetchRuntimeStatus(ctx context.Context, client *http.Client, wsEndpoint string) ([]statusRuntime, error) {
	if strings.TrimSpace(wsEndpoint) == "" {
		return nil, fmt.Errorf("kernel websocket endpoint is unknown")
	}
	snapshotURL, err := internalSnapshotURL(wsEndpoint, "/internal/snapshot/runtimes")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime snapshot returned %s", resp.Status)
	}
	var snapshot runtimeSnapshotFile
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	out := make([]statusRuntime, 0, len(snapshot.Runtimes))
	for _, runtime := range snapshot.Runtimes {
		caps := append([]string(nil), runtime.Capabilities...)
		sort.Strings(caps)
		out = append(out, statusRuntime{ID: runtime.ID, Attached: runtime.Attached, PID: runtime.PID, Capabilities: caps})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func internalSnapshotURL(wsEndpoint, path string) (string, error) {
	u, err := url.Parse(wsEndpoint)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported kernel endpoint scheme %q", u.Scheme)
	}
	port := u.Port()
	if replacement := localStatusHost(u.Hostname()); replacement != "" {
		u.Host = replacement
		if port != "" {
			u.Host = net.JoinHostPort(replacement, port)
		}
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func localStatusHost(host string) string {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	default:
		ip := net.ParseIP(host)
		if ip != nil && ip.IsUnspecified() {
			if ip.To4() != nil {
				return "127.0.0.1"
			}
			return "::1"
		}
		return ""
	}
}

func readTenants(tabulaHome string) ([]statusTenant, error) {
	dir := filepath.Join(tabulaHome, "state", "tenants")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []statusTenant{}, nil
		}
		return nil, fmt.Errorf("read tenants: %w", err)
	}
	tenants := make([]statusTenant, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read tenant %s: %w", entry.Name(), err)
		}
		tenants = append(tenants, statusTenant{ID: entry.Name(), CreatedAt: formatStatusTime(info.ModTime())})
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].ID < tenants[j].ID })
	return tenants, nil
}

func writeKernelStatusFiles(tabulaHome, wsEndpoint string, startedAt time.Time) error {
	runDir := filepath.Join(tabulaHome, "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	if err := os.Chmod(runDir, 0o700); err != nil {
		return fmt.Errorf("chmod run dir: %w", err)
	}
	state := kernelStatusFile{
		PID:           os.Getpid(),
		RuntimeSocket: filepath.Join(runDir, "runtime.sock"),
		WSEndpoint:    wsEndpoint,
		StartedAt:     formatStatusTime(startedAt),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, statusFilename), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write kernel status: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, pidFilename), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return fmt.Errorf("write kernel pid: %w", err)
	}
	return nil
}

func removeKernelStatusFiles(tabulaHome string) {
	runDir := filepath.Join(tabulaHome, "run")
	_ = os.Remove(filepath.Join(runDir, statusFilename))
	_ = os.Remove(filepath.Join(runDir, pidFilename))
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func uptimeSeconds(startedAt string, now time.Time) int64 {
	started, err := time.Parse(time.RFC3339, strings.TrimSpace(startedAt))
	if err != nil || now.Before(started) {
		return 0
	}
	return int64(now.Sub(started).Seconds())
}

func formatStatusTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func printHumanStatus(w io.Writer, doc statusDocument) {
	if doc.Kernel.Running {
		fmt.Fprintf(w, "kernel:    running (pid %d, uptime %s)\n", doc.Kernel.PID, humanDuration(time.Duration(doc.Kernel.UptimeSeconds)*time.Second))
	} else {
		fmt.Fprintln(w, "kernel:    stopped")
	}
	if len(doc.Runtimes) == 0 {
		fmt.Fprintln(w, "runtime:   none")
	} else {
		for i, runtime := range doc.Runtimes {
			label := "runtime:"
			if i > 0 {
				label = "runtime:"
			}
			state := "detached"
			if runtime.Attached {
				state = "attached"
			}
			caps := strings.Join(runtime.Capabilities, ", ")
			if caps == "" {
				caps = "no capabilities"
			}
			fmt.Fprintf(w, "%-10s %s (%s) — %s\n", label, runtime.ID, state, caps)
		}
	}
	tenantIDs := make([]string, 0, len(doc.Tenants))
	for _, tenant := range doc.Tenants {
		tenantIDs = append(tenantIDs, tenant.ID)
	}
	if len(tenantIDs) == 0 {
		fmt.Fprintln(w, "tenants:   none (0)")
		return
	}
	fmt.Fprintf(w, "tenants:   %s (%d)\n", strings.Join(tenantIDs, ", "), len(tenantIDs))
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
