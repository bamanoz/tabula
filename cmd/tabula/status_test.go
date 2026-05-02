package main

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

func TestBuildStatusKernelRunningFetchesRuntimeSnapshot(t *testing.T) {
	tabulaHome := t.TempDir()
	writeTenantDir(t, tabulaHome, "default")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/snapshot/runtimes" {
			t.Fatalf("unexpected snapshot path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runtimes":[{"id":"local","attached":true,"pid":12346,"capabilities":["write","read"]}]}`))
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

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal status json: %v", err)
	}
	for _, want := range []string{`"kernel"`, `"running":true`, `"runtimes"`, `"capabilities":["read","write"]`, `"tenants"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("json status missing %q in %s", want, string(data))
		}
	}
}

func TestBuildStatusKernelRunningWithNoRuntimeAttached(t *testing.T) {
	tabulaHome := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	stateDir := filepath.Join(tabulaHome, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "tenants"), []byte("not a dir"), 0o644); err != nil {
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
	if err := os.MkdirAll(filepath.Join(tabulaHome, "state", "tenants", id), 0o755); err != nil {
		t.Fatalf("mkdir tenant %s: %v", id, err)
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
