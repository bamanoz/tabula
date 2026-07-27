package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestRuntimeForTenantAllowsAttachedRuntimeWithoutConfiguredDefinition(t *testing.T) {
	hub := NewHub(nil, nil)
	if err := hub.runtimes.Configure(nil, map[string]runtimeconfig.Binding{"alpha": {AllowedRuntimes: []string{"local"}}}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	conn := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", conn, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	got, code, err := hub.runtimes.RuntimeForTenant("alpha", "local")
	if err != nil || code != "" || got == nil {
		t.Fatalf("RuntimeForTenant = conn:%T code:%q err:%v", got, code, err)
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
	hub := NewHub(nil, nil)
	hub.SetTenantStore(store)
	if err := configureRuntimeRegistryFromFiles(t, hub, home, store); err != nil {
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
	hub := NewHub(nil, nil)
	hub.SetTenantStore(store)
	local := runtimemock.New()
	remote := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", local, nil, 0); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	if err := configureRuntimeRegistryFromFiles(t, hub, home, store); err != nil {
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
	if err := configureRuntimeRegistryFromFiles(t, hub, home, store); err != nil {
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

func TestPickRuntimeDoesNotAutoSelectSingleConfiguredRuntime(t *testing.T) {
	hub := NewHub(nil, nil)
	if err := hub.runtimes.Configure(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}},
		map[string]runtimeconfig.Binding{},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	conn := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", conn, nil, 0); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	gotConn, runtimeID, code, err := hub.pickRuntime("alpha")
	if err == nil || err.Error() != `default runtime is not configured for tenant "alpha"` {
		t.Fatalf("unexpected error: conn=%T runtime=%q code=%q err=%v", gotConn, runtimeID, code, err)
	}
	if gotConn != nil || runtimeID != "" || code != wire.ErrorRuntimeUnavailable {
		t.Fatalf("unexpected pick result: conn=%T runtime=%q code=%q err=%v", gotConn, runtimeID, code, err)
	}
}

func configureRuntimeRegistryFromFiles(t *testing.T, hub *Hub, home string, store tenant.Store) error {
	t.Helper()
	defs, err := runtimeconfig.LoadDefinitions(home)
	if err != nil {
		return err
	}
	runtimeIDs := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		runtimeIDs[def.ID] = struct{}{}
	}
	bindings := map[string]runtimeconfig.Binding{}
	if store != nil {
		items, err := store.List()
		if err != nil {
			return err
		}
		for _, item := range items {
			binding, err := runtimeconfig.LoadBinding(home, item.ID, runtimeIDs)
			if err != nil {
				return err
			}
			bindings[item.ID] = binding
		}
	}
	return hub.ConfigureRuntimeRegistry(defs, bindings)
}

func writeKernelConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
