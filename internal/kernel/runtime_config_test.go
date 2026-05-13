package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestLoadRuntimeDefinitionsSynthesizesLocal(t *testing.T) {
	defs, err := LoadRuntimeDefinitions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRuntimeDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].ID != "local" || defs[0].Backend != "local" {
		t.Fatalf("definitions = %#v", defs)
	}
}

func TestLoadRuntimeDefinitionsRejectsDuplicateRuntimeID(t *testing.T) {
	home := t.TempDir()
	writeKernelConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "local"
backend = "local"

[[runtime]]
id = "local"
backend = "attach"
url = "unix:///tmp/runtime.sock"
`)
	_, err := LoadRuntimeDefinitions(home)
	if err == nil {
		t.Fatal("expected duplicate runtime id error")
	}
}

func TestLoadRuntimeDefinitionsRejectsUnsupportedBackend(t *testing.T) {
	home := t.TempDir()
	writeKernelConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "remote"
backend = "ftp"
`)
	_, err := LoadRuntimeDefinitions(home)
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
}

func TestLoadTenantRuntimeBindingValidatesUnknownRuntime(t *testing.T) {
	home := t.TempDir()
	writeKernelConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["missing"]
default_runtime = "local"
`)
	_, err := LoadTenantRuntimeBinding(home, "alpha", map[string]struct{}{"local": {}})
	if err == nil {
		t.Fatal("expected unknown runtime error")
	}
}

func TestConfigureRuntimeRegistryLoadsTenantBindings(t *testing.T) {
	home := t.TempDir()
	store := tenant.NewFSStore(home)
	if err := store.Create(tenant.Tenant{ID: "alpha"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	writeKernelConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "local"
backend = "local"

[[runtime]]
id = "remote"
backend = "attach"
url = "unix:///tmp/runtime.sock"
`)
	writeKernelConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["remote"]
default_runtime = "remote"
`)
	hub := NewHub(nil, 0, 0, nil)
	hub.SetTenantStore(store)
	if err := hub.ConfigureRuntimeRegistry(home); err != nil {
		t.Fatalf("ConfigureRuntimeRegistry: %v", err)
	}
	binding := hub.runtimes.tenantBindings["alpha"]
	if binding.DefaultRuntime != "remote" || len(binding.AllowedRuntimes) != 1 || binding.AllowedRuntimes[0] != "remote" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestConfigureRuntimeRegistryReloadUpdatesTenantBinding(t *testing.T) {
	home := t.TempDir()
	store := tenant.NewFSStore(home)
	if err := store.Create(tenant.Tenant{ID: "alpha"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	writeKernelConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "local"
backend = "local"

[[runtime]]
id = "remote"
backend = "attach"
url = "unix:///tmp/runtime.sock"
`)
	writeKernelConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["local", "remote"]
default_runtime = "local"
`)
	hub := NewHub(nil, 0, 0, nil)
	hub.SetTenantStore(store)
	local := runtimemock.New()
	remote := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", local, nil, 0); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	if err := hub.ConfigureRuntimeRegistry(home); err != nil {
		t.Fatalf("ConfigureRuntimeRegistry local: %v", err)
	}
	conn, runtimeID, code, err := hub.pickRuntime("alpha")
	if err != nil || code != "" || runtimeID != "local" || conn == nil {
		t.Fatalf("initial pick = conn:%T runtime:%q code:%q err:%v", conn, runtimeID, code, err)
	}

	writeKernelConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["local", "remote"]
default_runtime = "remote"
`)
	if err := hub.ConfigureRuntimeRegistry(home); err != nil {
		t.Fatalf("ConfigureRuntimeRegistry remote: %v", err)
	}
	conn, runtimeID, code, err = hub.pickRuntime("alpha")
	if err != nil || code != "" || runtimeID != "remote" || conn == nil {
		t.Fatalf("reload pick = conn:%T runtime:%q code:%q err:%v", conn, runtimeID, code, err)
	}
	if _, err := conn.Health(context.Background()); err != nil {
		t.Fatalf("remote conn health through picked runtime: %v", err)
	}
}

func writeKernelConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
