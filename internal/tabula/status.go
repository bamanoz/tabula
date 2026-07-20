package tabula

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
	"time"

	"github.com/bamanoz/tabula/internal/runtime/paths"
	"github.com/bamanoz/tabula/internal/tenant"
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
	ID                   string              `json:"id"`
	Attached             bool                `json:"attached"`
	PID                  int                 `json:"pid"`
	Capabilities         []string            `json:"capabilities"`
	TenantsServed        []string            `json:"tenants_served"`
	CapabilitiesByTenant map[string][]string `json:"capabilities_by_tenant"`
	Targets              []statusTarget      `json:"targets,omitempty"`
}

type statusTarget struct {
	Kind           string   `json:"kind"`
	ID             string   `json:"id"`
	Tools          []string `json:"tools,omitempty"`
	Hooks          []string `json:"hooks,omitempty"`
	State          string   `json:"state,omitempty"`
	Source         string   `json:"source,omitempty"`
	Revision       int64    `json:"revision,omitempty"`
	PID            int      `json:"pid,omitempty"`
	LifecycleState string   `json:"lifecycle_state,omitempty"`
	Diagnostic     string   `json:"diagnostic,omitempty"`
}

type statusTenant struct {
	ID                  string `json:"id"`
	DisplayName         string `json:"display_name,omitempty"`
	CreatedAt           string `json:"created_at"`
	ActiveSessionsCount int    `json:"active_sessions"`
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
		ID                   string              `json:"id"`
		Attached             bool                `json:"attached"`
		PID                  int                 `json:"pid"`
		Capabilities         []string            `json:"capabilities"`
		TenantsServed        []string            `json:"tenants_served"`
		CapabilitiesByTenant map[string][]string `json:"capabilities_by_tenant"`
		WorkerCount          int                 `json:"worker_count,omitempty"`
		LastError            *string             `json:"last_error,omitempty"`
		ConnectedAt          string              `json:"connected_at,omitempty"`
		Targets              []statusTarget      `json:"targets,omitempty"`
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
	if inferredHome, ok := executableTabulaHome(); ok {
		if preferredDoc, preferred := preferCandidateStatus(context.Background(), doc, tabulaHome, inferredHome, http.DefaultClient); preferred {
			doc = preferredDoc
		}
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
	if strings.TrimSpace(os.Getenv("TABULA_HOME")) != "" {
		return paths.Home(), nil
	}
	if home, ok := executableTabulaHome(); ok {
		return home, nil
	}
	return paths.Home(), nil
}

func executableTabulaHome() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	return tabulaHomeFromExecutable(executable)
}

func preferCandidateStatus(ctx context.Context, current statusDocument, currentHome, candidateHome string, client *http.Client) (statusDocument, bool) {
	if current.Kernel.Running || candidateHome == "" || candidateHome == currentHome {
		return current, false
	}
	candidate, err := buildStatus(ctx, candidateHome, client)
	if err != nil || !candidate.Kernel.Running {
		return current, false
	}
	return candidate, true
}

func tabulaHomeFromExecutable(executable string) (string, bool) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return "", false
	}
	binDir := filepath.Dir(executable)
	if filepath.Base(binDir) != "bin" {
		return "", false
	}
	home := filepath.Clean(filepath.Dir(binDir))
	if !installedHomeLayout(home) {
		return "", false
	}
	return home, true
}

func installedHomeLayout(home string) bool {
	if home == "" || home == "." || home == string(filepath.Separator) {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, "PROTOCOL")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "tabula-runner")); err != nil {
		return false
	}
	return true
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
	activeSessions := map[string]int{}
	if running {
		doc.Kernel.UptimeSeconds = uptimeSeconds(state.StartedAt, time.Now().UTC())
		if counts, err := fetchSessionTenantCounts(ctx, client, state.WSEndpoint); err == nil {
			activeSessions = counts
		}
	}
	tenants, err := readTenants(tabulaHome, activeSessions)
	if err != nil {
		return statusDocument{}, err
	}
	doc.Tenants = tenants
	if running {
		if runtimes, err := fetchRuntimeStatus(ctx, client, state.WSEndpoint, statusTenantIDs(tenants)); err == nil {
			doc.Runtimes = runtimes
		} else {
			// If the pidfile is stale enough that the loopback snapshot cannot be read,
			// treat the kernel as not running. `status` still succeeds and reports
			// tenant state from disk, per M2-06.
			doc.Kernel.Running = false
			doc.Kernel.UptimeSeconds = 0
		}
	}
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
			return kernelStatusFile{PID: pid, RuntimeSocket: localRuntimeSocketPath(tabulaHome)}, nil
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

func fetchRuntimeStatus(ctx context.Context, client *http.Client, wsEndpoint string, knownTenants []string) ([]statusRuntime, error) {
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
		tenantsServed := expandRuntimeTenants(runtime.TenantsServed, knownTenants)
		out = append(out, statusRuntime{ID: runtime.ID, Attached: runtime.Attached, PID: runtime.PID, Capabilities: caps, TenantsServed: tenantsServed, CapabilitiesByTenant: statusCapabilitiesByTenant(tenantsServed, caps), Targets: runtime.Targets})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func statusTenantIDs(tenants []statusTenant) []string {
	out := make([]string, 0, len(tenants))
	for _, item := range tenants {
		out = append(out, item.ID)
	}
	sort.Strings(out)
	return out
}

func fetchSessionTenantCounts(ctx context.Context, client *http.Client, wsEndpoint string) (map[string]int, error) {
	if strings.TrimSpace(wsEndpoint) == "" {
		return nil, fmt.Errorf("kernel websocket endpoint is unknown")
	}
	sessionsURL, err := internalSnapshotURL(wsEndpoint, "/sessions")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sessionsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session snapshot returned %s", resp.Status)
	}
	var sessions map[string]struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, session := range sessions {
		tenantID := strings.TrimSpace(session.TenantID)
		if tenantID == "" {
			tenantID = tenant.DefaultID
		}
		counts[tenantID]++
	}
	return counts, nil
}

func expandRuntimeTenants(raw, known []string) []string {
	if len(raw) == 0 {
		raw = []string{"*"}
	}
	for _, tenantID := range raw {
		if tenantID == "*" {
			if len(known) == 0 {
				return []string{tenant.DefaultID}
			}
			out := append([]string(nil), known...)
			sort.Strings(out)
			return out
		}
	}
	out := append([]string(nil), raw...)
	sort.Strings(out)
	return out
}

func statusCapabilitiesByTenant(tenants, capabilities []string) map[string][]string {
	out := make(map[string][]string, len(tenants))
	for _, tenantID := range tenants {
		out[tenantID] = append([]string(nil), capabilities...)
	}
	return out
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

func readTenants(tabulaHome string, activeSessions map[string]int) ([]statusTenant, error) {
	store := tenant.NewFSStore(tabulaHome)
	items, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("read tenants: %w", err)
	}
	tenants := make([]statusTenant, 0, len(items))
	for _, item := range items {
		tenants = append(tenants, statusTenant{ID: item.ID, DisplayName: item.DisplayName, CreatedAt: formatStatusTime(item.CreatedAt), ActiveSessionsCount: activeSessions[item.ID]})
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
		RuntimeSocket: localRuntimeSocketPath(tabulaHome),
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
	return platformProcessRunning(pid)
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
			fmt.Fprintf(w, "%-10s %s (%s, tenants %s) — %s\n", label, runtime.ID, state, summarizedTenantList(runtime.TenantsServed), caps)
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

func summarizedTenantList(tenants []string) string {
	if len(tenants) == 0 {
		return "none"
	}
	if len(tenants) <= 3 {
		return strings.Join(tenants, ", ")
	}
	return strings.Join(tenants[:3], ", ") + fmt.Sprintf(", +%d more", len(tenants)-3)
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
