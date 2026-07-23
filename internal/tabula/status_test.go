package tabula

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

func TestBuildStatusKernelDownStillListsTenants(t *testing.T) {
	tabulaHome := t.TempDir()
	writeTenantDir(t, tabulaHome, "default")
	writeTenantDir(t, tabulaHome, "project-a")

	doc, err := buildStatus(t.Context(), tabulaHome, http.DefaultClient)
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	if doc.Kernel.Running {
		t.Fatalf("expected stopped kernel, got %+v", doc.Kernel)
	}
	if len(doc.Runtimes) != 0 {
		t.Fatalf("expected no runtimes when kernel is down, got %+v", doc.Runtimes)
	}
	if got := tenantIDs(doc.Tenants); strings.Join(got, ",") != "default,project-a" {
		t.Fatalf("unexpected tenants: %+v", doc.Tenants)
	}

	var out bytes.Buffer
	printHumanStatus(&out, doc)
	got := out.String()
	for _, want := range []string{"kernel:    stopped", "runtime:   none", "tenants:   default, project-a (2)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human status missing %q in:\n%s", want, got)
		}
	}
}

func TestProcessRunningRecognizesCurrentProcess(t *testing.T) {
	if !processRunning(os.Getpid()) {
		t.Fatalf("current process %d should be running", os.Getpid())
	}
	if processRunning(0) {
		t.Fatal("zero PID must not be running")
	}
}

func TestTabulaHomeFromExecutableDetectsInstalledLayout(t *testing.T) {
	tabulaHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tabulaHome, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tabulaHome, "PROTOCOL"), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write protocol: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tabulaHome, "bin", "tabula"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	got, ok := tabulaHomeFromExecutable(filepath.Join(tabulaHome, "bin", "tabula"))
	if !ok || got != tabulaHome {
		t.Fatalf("tabulaHomeFromExecutable = %q, %v; want %q, true", got, ok, tabulaHome)
	}
}

func TestTabulaHomeFromExecutableRejectsSourceTreeBin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("0.10.0\n"), 0o644); err != nil {
		t.Fatalf("write version: %v", err)
	}

	got, ok := tabulaHomeFromExecutable(filepath.Join(root, "bin", "tabula"))
	if ok || got != "" {
		t.Fatalf("tabulaHomeFromExecutable = %q, %v; want empty, false", got, ok)
	}
}

func TestPreferCandidateStatusUsesLiveExecutableHome(t *testing.T) {
	currentHome := t.TempDir()
	candidateHome := t.TempDir()
	writeTenantDir(t, currentHome, "old-home")
	writeTenantDir(t, candidateHome, "live-home")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions":
			_, _ = w.Write([]byte(`{}`))
		case "/internal/snapshot/runtimes":
			_, _ = w.Write([]byte(`{"runtimes":[]}`))
		default:
			t.Fatalf("unexpected status path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	writeKernelStatusFixture(t, candidateHome, kernelStatusFile{PID: os.Getpid(), WSEndpoint: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", StartedAt: formatStatusTime(time.Now())})

	current, err := buildStatus(t.Context(), currentHome, srv.Client())
	if err != nil {
		t.Fatalf("build current status: %v", err)
	}
	preferred, ok := preferCandidateStatus(t.Context(), current, currentHome, candidateHome, srv.Client())
	if !ok || !preferred.Kernel.Running {
		t.Fatalf("expected live candidate status, ok=%v doc=%+v", ok, preferred.Kernel)
	}
	if got := tenantIDs(preferred.Tenants); strings.Join(got, ",") != "live-home" {
		t.Fatalf("expected candidate tenants, got %+v", preferred.Tenants)
	}
}

func TestBuildStatusKernelRunningFetchesRuntimeSnapshot(t *testing.T) {
	tabulaHome := t.TempDir()
	writeTenantDir(t, tabulaHome, "default")
	writeTenantDir(t, tabulaHome, "project-a")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session-1":{"tenant_id":"project-a"},"session-2":{"tenant_id":"project-a"}}`))
			return
		}
		if r.URL.Path != "/internal/snapshot/runtimes" {
			t.Fatalf("unexpected snapshot path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runtimes":[{"id":"local","attached":true,"pid":12346,"capabilities":["write","read"],"tenants_served":["*"]}]}`))
	}))
	defer srv.Close()
	writeKernelStatusFixture(t, tabulaHome, kernelStatusFile{
		PID:           os.Getpid(),
		RuntimeSocket: filepath.Join(tabulaHome, "run", "runtime.sock"),
		WSEndpoint:    "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
		StartedAt:     formatStatusTime(time.Now().Add(-2 * time.Minute)),
	})

	doc, err := buildStatus(t.Context(), tabulaHome, srv.Client())
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	if !doc.Kernel.Running || doc.Kernel.PID != os.Getpid() || doc.Kernel.UptimeSeconds <= 0 {
		t.Fatalf("unexpected kernel status: %+v", doc.Kernel)
	}
	if len(doc.Runtimes) != 1 {
		t.Fatalf("expected one runtime, got %+v", doc.Runtimes)
	}
	runtime := doc.Runtimes[0]
	if runtime.ID != "local" || !runtime.Attached || runtime.PID != 12346 || strings.Join(runtime.Capabilities, ",") != "read,write" {
		t.Fatalf("unexpected runtime status: %+v", runtime)
	}
	if strings.Join(runtime.TenantsServed, ",") != "default,project-a" {
		t.Fatalf("unexpected runtime tenants: %+v", runtime.TenantsServed)
	}
	if strings.Join(runtime.CapabilitiesByTenant["project-a"], ",") != "read,write" {
		t.Fatalf("unexpected capabilities_by_tenant: %+v", runtime.CapabilitiesByTenant)
	}
	if len(doc.Tenants) != 2 || doc.Tenants[1].ID != "project-a" || doc.Tenants[1].ActiveSessionsCount != 2 {
		t.Fatalf("unexpected tenant active sessions: %+v", doc.Tenants)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal status json: %v", err)
	}
	for _, want := range []string{`"kernel"`, `"running":true`, `"runtimes"`, `"capabilities":["read","write"]`, `"tenants_served":["default","project-a"]`, `"capabilities_by_tenant"`, `"active_sessions":2`, `"tenants"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("json status missing %q in %s", want, string(data))
		}
	}
}

func TestStatusJSONPreservesM206ShapeSubset(t *testing.T) {
	doc := statusDocument{
		Kernel:   statusKernel{Running: true, PID: 123, Socket: "/tmp/runtime.sock", WSEndpoint: "ws://127.0.0.1:8089/ws", UptimeSeconds: 5},
		Runtimes: []statusRuntime{{ID: "local", Attached: true, PID: 456, Capabilities: []string{"read"}, TenantsServed: []string{"default"}, CapabilitiesByTenant: map[string][]string{"default": {"read"}}}},
		Tenants:  []statusTenant{{ID: "default", CreatedAt: "2026-05-05T00:00:00Z", ActiveSessionsCount: 0}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, top := range []string{"kernel", "runtimes", "tenants"} {
		if _, ok := got[top]; !ok {
			t.Fatalf("missing top-level M2 field %q in %s", top, data)
		}
	}
	runtimes, ok := got["runtimes"].([]any)
	if !ok || len(runtimes) != 1 {
		t.Fatalf("bad runtimes field: %#v", got["runtimes"])
	}
	runtime, ok := runtimes[0].(map[string]any)
	if !ok {
		t.Fatalf("bad runtime entry: %#v", runtimes[0])
	}
	for _, field := range []string{"id", "attached", "pid", "capabilities"} {
		if _, ok := runtime[field]; !ok {
			t.Fatalf("missing M2 runtime field %q in %s", field, data)
		}
	}
}

func TestBuildStatusKernelRunningWithNoRuntimeAttached(t *testing.T) {
	tabulaHome := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"runtimes":[]}`))
	}))
	defer srv.Close()
	writeKernelStatusFixture(t, tabulaHome, kernelStatusFile{PID: os.Getpid(), WSEndpoint: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", StartedAt: formatStatusTime(time.Now())})

	doc, err := buildStatus(t.Context(), tabulaHome, srv.Client())
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	if !doc.Kernel.Running {
		t.Fatalf("expected running kernel, got %+v", doc.Kernel)
	}
	if len(doc.Runtimes) != 0 {
		t.Fatalf("expected no runtimes, got %+v", doc.Runtimes)
	}
}

func TestBuildStatusReturnsReadErrorForInvalidTenantsState(t *testing.T) {
	tabulaHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(tabulaHome, "tenants"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write tenants file: %v", err)
	}
	if _, err := buildStatus(t.Context(), tabulaHome, http.DefaultClient); err == nil {
		t.Fatal("expected tenants read error")
	}
}

func TestInternalSnapshotURLConvertsWebsocketEndpoint(t *testing.T) {
	got, err := internalSnapshotURL("ws://127.0.0.1:7777/ws", "/internal/snapshot/runtimes")
	if err != nil {
		t.Fatalf("internalSnapshotURL: %v", err)
	}
	if got != "http://127.0.0.1:7777/internal/snapshot/runtimes" {
		t.Fatalf("unexpected snapshot URL: %s", got)
	}
}

func TestInternalSnapshotURLUsesLoopbackForWildcardBind(t *testing.T) {
	got, err := internalSnapshotURL("ws://0.0.0.0:7777/ws", "/internal/snapshot/runtimes")
	if err != nil {
		t.Fatalf("internalSnapshotURL: %v", err)
	}
	if got != "http://127.0.0.1:7777/internal/snapshot/runtimes" {
		t.Fatalf("unexpected snapshot URL: %s", got)
	}
}

func writeTenantDir(t *testing.T, tabulaHome, id string) {
	t.Helper()
	store := tenant.NewFSStore(tabulaHome)
	if err := store.Create(tenant.Tenant{ID: id}); err != nil {
		t.Fatalf("create tenant %s: %v", id, err)
	}
}

func tenantIDs(tenants []statusTenant) []string {
	out := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, tenant.ID)
	}
	return out
}

func writeKernelStatusFixture(t *testing.T, tabulaHome string, state kernelStatusFile) {
	t.Helper()
	runDir := filepath.Join(tabulaHome, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, statusFilename), data, 0o644); err != nil {
		t.Fatalf("write status fixture: %v", err)
	}
}
