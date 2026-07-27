package hostcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/layout"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
)

func TestConfiguredRuntimeIDUsesExplicitValue(t *testing.T) {
	got, err := configuredRuntimeID("remote-1", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("configuredRuntimeID: %v", err)
	}
	if got != "remote-1" {
		t.Fatalf("configuredRuntimeID = %q", got)
	}
}

func TestConfiguredRuntimeIDBootstrapsMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	got, err := configuredRuntimeID("", time.Unix(123, 0).UTC())
	if err != nil {
		t.Fatalf("configuredRuntimeID: %v", err)
	}
	meta, err := runtimeinstance.Load(layout.RuntimeInstanceFile(layout.Home()))
	if err != nil {
		t.Fatalf("Load metadata: %v", err)
	}
	if got != meta.RuntimeID {
		t.Fatalf("configuredRuntimeID = %q, metadata runtime id = %q", got, meta.RuntimeID)
	}
	if meta.CreatedAt != time.Unix(123, 0).UTC() {
		t.Fatalf("CreatedAt = %s", meta.CreatedAt)
	}
}

func TestWorkerKernelURLUsesConfiguredURLWhenEnvUnset(t *testing.T) {
	t.Setenv("TABULA_URL", "")
	if got := workerKernelURL("ws://127.0.0.1:8089/ws"); got != "ws://127.0.0.1:8089/ws" {
		t.Fatalf("workerKernelURL = %q", got)
	}
}

func TestWorkerKernelURLPrefersEnvURL(t *testing.T) {
	t.Setenv("TABULA_URL", "ws://127.0.0.1:9999/ws")
	if got := workerKernelURL("ws://127.0.0.1:8089/ws"); got != "ws://127.0.0.1:9999/ws" {
		t.Fatalf("workerKernelURL = %q", got)
	}
}

func TestPrepareRuntimeEnvironmentLoadsEnvWithoutRewritingPath(t *testing.T) {
	home := t.TempDir()
	venv := filepath.Join(home, "venv")
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("TABULA_VENV="+venv+"\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	unsetEnvForTest(t, "TABULA_VENV")
	unsetEnvForTest(t, "TABULA_PATH")
	t.Setenv("PATH", "/usr/bin")

	prepareRuntimeEnvironment(home)

	if got := os.Getenv("PATH"); got != "/usr/bin" {
		t.Fatalf("PATH = %q, want unchanged", got)
	}
	if got := os.Getenv("TABULA_VENV"); got != venv {
		t.Fatalf("TABULA_VENV = %q, want %q", got, venv)
	}
}

func TestPrepareRuntimeEnvironmentRestoresTabulaPath(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("TABULA_VENV=/ignored\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("TABULA_PATH", "/runtime/bin:/usr/bin")
	t.Setenv("PATH", "/usr/bin")

	prepareRuntimeEnvironment(home)

	if got := os.Getenv("PATH"); got != "/runtime/bin:/usr/bin" {
		t.Fatalf("PATH = %q", got)
	}
}

func TestNewManifestStoreLoadsTenantCatalogs(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "tenants", "alpha", "plugins", "fs", "plugin.toml"), `id = "fs"
name = "Filesystem"
version = "0.1.0"
[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "fs_read"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)
	store, err := newManifestStore(runtimeconfig.Config{Tenants: []runtimeconfig.Tenant{{ID: "alpha", PluginDirs: []string{filepath.Join(dir, "tenants", "alpha", "plugins")}}}})
	if err != nil {
		t.Fatalf("newManifestStore: %v", err)
	}
	caps := store.Capabilities()
	if len(caps) != 1 || caps[0].Target.ID != "fs" || caps[0].Tenants[0] != "alpha" {
		t.Fatalf("capabilities = %#v", caps)
	}
	if got := runtimePluginDirs(runtimeconfig.Config{Tenants: []runtimeconfig.Tenant{{ID: "alpha", PluginDirs: []string{"/tmp/alpha"}}}}); len(got) != 1 || got[0] != "/tmp/alpha" {
		t.Fatalf("runtimePluginDirs = %#v", got)
	}
}

func TestNewManifestStoreSurfacesTenantCatalogErrors(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "tenants", "alpha", "plugins", "broken", "plugin.toml"), `id = "broken"
name = "Broken"
version = "0.1.0"
[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = "bad"
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)
	_, err := newManifestStore(runtimeconfig.Config{Tenants: []runtimeconfig.Tenant{{ID: "alpha", PluginDirs: []string{filepath.Join(dir, "tenants", "alpha", "plugins")}}}})
	if err == nil || !strings.Contains(err.Error(), "requires.protocol_version") {
		t.Fatalf("expected manifest load error, got %v", err)
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func writePlugin(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
}
