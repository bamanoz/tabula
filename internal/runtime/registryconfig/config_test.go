package registryconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefinitionsWithoutConfigReturnsEmpty(t *testing.T) {
	defs, err := LoadDefinitions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("definitions = %#v", defs)
	}
}

func TestLoadDefinitionsRejectsDuplicateRuntimeID(t *testing.T) {
	home := t.TempDir()
	writeConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "local"
backend = "local"

[[runtime]]
id = "local"
backend = "attach"
url = "unix:///tmp/runtime.sock"
`)
	_, err := LoadDefinitions(home)
	if err == nil {
		t.Fatal("expected duplicate runtime id error")
	}
}

func TestLoadDefinitionsRejectsUnsupportedBackend(t *testing.T) {
	home := t.TempDir()
	writeConfigFile(t, filepath.Join(home, "config", "global.toml"), `[[runtime]]
id = "remote"
backend = "ftp"
`)
	_, err := LoadDefinitions(home)
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
}

func TestLoadBindingValidatesUnknownRuntime(t *testing.T) {
	home := t.TempDir()
	writeConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["missing"]
default_runtime = "local"
`)
	_, err := LoadBinding(home, "alpha", map[string]struct{}{"local": {}})
	if err == nil {
		t.Fatal("expected unknown runtime error")
	}
}

func TestLoadBindingAllowsMissingDefaultRuntime(t *testing.T) {
	home := t.TempDir()
	writeConfigFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), `[tenant]
allowed_runtimes = ["local"]
`)
	binding, err := LoadBinding(home, "alpha", map[string]struct{}{"local": {}})
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	if binding.DefaultRuntime != "" || len(binding.AllowedRuntimes) != 1 || binding.AllowedRuntimes[0] != "local" {
		t.Fatalf("binding = %#v", binding)
	}
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
