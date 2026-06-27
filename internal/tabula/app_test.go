package tabula

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestWebSocketOriginCheck_DefaultAllowsLocalhost(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "http://localhost:3000")

	if !checkWebSocketOrigin(req) {
		t.Fatal("expected localhost origin to be allowed by default")
	}
}

func TestWebSocketOriginCheck_DefaultRejectsRemoteOrigin(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "https://evil.example")

	if checkWebSocketOrigin(req) {
		t.Fatal("expected remote origin to be rejected by default")
	}
}

func TestWebSocketOriginCheck_AllowsConfiguredOrigin(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "https://app.example, https://admin.example")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "https://admin.example")

	if !checkWebSocketOrigin(req) {
		t.Fatal("expected configured origin to be allowed")
	}
}

func TestLoadEnvFileLoadsMissingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TABULA_PROVIDER=openai\nOPENAI_MODEL=gpt-5.4\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("TABULA_PROVIDER", "")
	os.Unsetenv("TABULA_PROVIDER")
	t.Setenv("OPENAI_MODEL", "")
	os.Unsetenv("OPENAI_MODEL")

	loadEnvFile(path)

	if got := os.Getenv("TABULA_PROVIDER"); got != "openai" {
		t.Fatalf("expected TABULA_PROVIDER=openai, got %q", got)
	}
	if got := os.Getenv("OPENAI_MODEL"); got != "gpt-5.4" {
		t.Fatalf("expected OPENAI_MODEL=gpt-5.4, got %q", got)
	}
}

func TestLoadEnvFileDoesNotOverrideExistingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TABULA_PROVIDER=anthropic\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("TABULA_PROVIDER", "openai")
	loadEnvFile(path)

	if got := os.Getenv("TABULA_PROVIDER"); got != "openai" {
		t.Fatalf("expected existing TABULA_PROVIDER to win, got %q", got)
	}
}

func TestLoadKernelConfigUsesKernelOwnedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kernel.toml")
	if err := os.WriteFile(path, []byte(`
[kernel]
url = "ws://127.0.0.1:9090/ws"

[runtime_wss]
enabled = true
listen = "127.0.0.1:9443"
path = "/runtime/ws"
origins = ["https://runtime.example"]
cert_file = "$TABULA_TEST_CERT"
key_file = "$TABULA_TEST_KEY"
client_ca = "$TABULA_TEST_CA"
client_auth = "require"
`), 0o644); err != nil {
		t.Fatalf("write kernel config: %v", err)
	}
	t.Setenv("TABULA_TEST_CERT", "/cert.pem")
	t.Setenv("TABULA_TEST_KEY", "/key.pem")
	t.Setenv("TABULA_TEST_CA", "/ca.pem")

	cfg, err := loadKernelConfig(path)
	if err != nil {
		t.Fatalf("loadKernelConfig: %v", err)
	}
	if cfg.URL != "ws://127.0.0.1:9090/ws" {
		t.Fatalf("url = %q", cfg.URL)
	}
	endpoint, ok := cfg.runtimeWSSEndpoint()
	if !ok {
		t.Fatal("expected runtime wss endpoint")
	}
	if endpoint.Listen != "127.0.0.1:9443" || endpoint.Path != "/runtime/ws" || endpoint.CertFile != "/cert.pem" || endpoint.KeyFile != "/key.pem" || endpoint.ClientCA != "/ca.pem" || endpoint.ClientAuth != "require" {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
}

func TestLoadKernelConfigFileReadsInstalledConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "kernel.toml"), []byte("[kernel]\nurl = \"ws://127.0.0.1:9090/ws\"\n"), 0o644); err != nil {
		t.Fatalf("write kernel config: %v", err)
	}

	cfg, err := loadKernelConfigFile(home)
	if err != nil {
		t.Fatalf("loadKernelConfigFile: %v", err)
	}
	if cfg.URL != "ws://127.0.0.1:9090/ws" {
		t.Fatalf("url = %q", cfg.URL)
	}
}

func TestReloadLocalRuntimeUsesAttachedRuntime(t *testing.T) {
	fake := &fakeRuntimeReloader{attempted: true}
	if err := reloadLocalRuntime(t.Context(), fake, "alpha"); err != nil {
		t.Fatalf("reloadLocalRuntime: %v", err)
	}
	if fake.calls != 1 || fake.runtimeID != runtimeauth.LocalRuntimeID || len(fake.tenants) != 1 || fake.tenants[0] != "alpha" {
		t.Fatalf("fake reloader = %+v", fake)
	}
}

func TestReloadTriggerTenantReadsTenantPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reload.touch")
	if err := os.WriteFile(path, []byte("time=1\ntenant=alpha\n"), 0o644); err != nil {
		t.Fatalf("write trigger: %v", err)
	}
	if got := reloadTriggerTenant(path); got != "alpha" {
		t.Fatalf("tenant = %q", got)
	}
}

func TestTenantScopedReloadTriggerStillReloadsAllLocalTenants(t *testing.T) {
	dir := t.TempDir()
	tabulaHome := filepath.Join(dir, "home")
	path := filepath.Join(tabulaHome, "run", "reload.touch")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir trigger: %v", err)
	}
	if err := os.WriteFile(path, []byte("time=1\ntenant=alpha\n"), 0o644); err != nil {
		t.Fatalf("write trigger: %v", err)
	}
	if err := runtimeconfig.Save(filepath.Join(tabulaHome, "config", "runtime.toml"), runtimeconfig.Config{
		Kernels: []runtimeconfig.Kernel{{ID: runtimeauth.LocalRuntimeID, URL: "unix:///tmp/runtime.sock", TokenFile: "/tmp/runtime-token", Tenants: []string{"alpha", "beta"}}},
	}); err != nil {
		t.Fatalf("save runtime config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tabulaHome, "config", "global.toml"), []byte("[[runtime]]\nid = \"local\"\nbackend = \"local\"\n"), 0o644); err != nil {
		t.Fatalf("write global runtime config: %v", err)
	}
	store := tenant.NewFSStore(tabulaHome)
	for _, tenantID := range []string{"alpha", "beta"} {
		if err := store.Create(tenant.Tenant{ID: tenantID, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("create tenant: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tabulaHome, "tenants", tenantID, "config", "tenant.toml"), []byte("[tenant]\ndefault_runtime = \"local\"\n"), 0o644); err != nil {
			t.Fatalf("write tenant runtime config: %v", err)
		}
	}
	got := reloadTriggerTenants(path, tabulaHome)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("tenants = %#v", got)
	}
}

func TestReloadLocalRuntimeFailsWhenNotAttached(t *testing.T) {
	if err := reloadLocalRuntime(t.Context(), &fakeRuntimeReloader{}); err == nil || err.Error() != "local runtime is not attached" {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeRuntimeReloader struct {
	attempted bool
	err       error
	calls     int
	runtimeID string
	tenants   []string
}

func (f *fakeRuntimeReloader) ReloadAttachedRuntime(_ context.Context, runtimeID string, _ *wire.Target, tenants ...string) (bool, error) {
	f.calls++
	f.runtimeID = runtimeID
	f.tenants = append([]string(nil), tenants...)
	return f.attempted, f.err
}

func TestHealthEndpoint(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var body struct {
		Status                   string `json:"status"`
		Version                  string `json:"version"`
		KernelVersion            string `json:"kernel_version"`
		Commit                   string `json:"commit"`
		ProtocolVersion          int    `json:"protocol_version"`
		MinPluginProtocolVersion int    `json:"min_plugin_protocol_version"`
		MaxPluginProtocolVersion int    `json:"max_plugin_protocol_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal health response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Version == "" || body.Commit == "" {
		t.Fatalf("expected version and commit in response, got %+v", body)
	}
	if body.KernelVersion != body.Version {
		t.Fatalf("expected kernel_version to mirror version, got %+v", body)
	}
	if body.ProtocolVersion != kernel.ProtocolVersion {
		t.Fatalf("expected protocol version %d, got %d", kernel.ProtocolVersion, body.ProtocolVersion)
	}
	if body.MinPluginProtocolVersion != 1 || body.MaxPluginProtocolVersion != 1 {
		t.Fatalf("expected plugin protocol range 1..1, got %+v", body)
	}
}

func TestHealthEndpointRejectsNonGET(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodPost, "http://tabula.local/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("expected Allow: GET, got %q", got)
	}
}

func TestRuntimeSnapshotEndpointAllowsLoopbackRequest(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/runtimes", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
}

func TestRuntimeSnapshotEndpointAllowsIPv6LoopbackRequest(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "[::1]:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://[::1]:8089/internal/snapshot/runtimes", nil)
	req.Host = "[::1]:8089"
	req.RemoteAddr = "[::1]:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSnapshotEndpointRejectsRemoteAddress(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "0.0.0.0:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/runtimes", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "203.0.113.10:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSnapshotEndpointRejectsRemoteHost(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "0.0.0.0:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/internal/snapshot/runtimes", nil)
	req.Host = "example.com"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSnapshotEndpointRejectsMalformedRemoteAddr(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/runtimes", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "not-a-host-port"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSnapshotEndpointRejectsForwardedLocalityHeaders(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/runtimes", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "203.0.113.10:34567"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Forwarded-Host", "localhost:8089")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSnapshotEndpointRejectsNonGETBeforeSnapshot(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089", BuildInfo{})

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8089/internal/snapshot/runtimes", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("expected Allow: GET, got %q", got)
	}
}
