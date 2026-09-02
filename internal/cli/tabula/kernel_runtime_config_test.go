package tabula

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestLoadKernelRuntimeRegistryConfigDefaultsManagedTenantsToLocal(t *testing.T) {
	home := t.TempDir()
	store := tenant.NewFSStore(home)
	if err := store.Create(tenant.Tenant{ID: "default", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	definitions, bindings, err := loadKernelRuntimeRegistryConfig(home, store, true)
	if err != nil {
		t.Fatalf("load managed runtime config: %v", err)
	}
	if len(definitions) != 1 || definitions[0].ID != runtimeauth.LocalRuntimeID || definitions[0].Backend != "local" {
		t.Fatalf("managed definitions = %#v", definitions)
	}
	binding := bindings["default"]
	if binding.DefaultRuntime != runtimeauth.LocalRuntimeID || len(binding.AllowedRuntimes) != 1 || binding.AllowedRuntimes[0] != "*" {
		t.Fatalf("managed binding = %#v", binding)
	}
}

func TestLoadKernelRuntimeRegistryConfigRespectsExplicitManagedAllowlist(t *testing.T) {
	home := t.TempDir()
	store := tenant.NewFSStore(home)
	if err := store.Create(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	writeCLIConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "remote"
backend = "attach"
url = "unix:///tmp/remote.sock"
`)
	writeCLIConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["remote"]
`)

	definitions, bindings, err := loadKernelRuntimeRegistryConfig(home, store, true)
	if err != nil {
		t.Fatalf("load managed runtime config: %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("managed definitions = %#v", definitions)
	}
	binding := bindings["alpha"]
	if binding.DefaultRuntime != "" || len(binding.AllowedRuntimes) != 1 || binding.AllowedRuntimes[0] != "remote" {
		t.Fatalf("explicit binding = %#v", binding)
	}
}

func writeCLIConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
